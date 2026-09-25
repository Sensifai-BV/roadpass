// Package circuloos talks to the CIRCULOOS Data Platform (Orion-LD behind Kong,
// Keycloak auth) exactly like the Phase-1 Postman collection does.
package circuloos

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/audit"
)

const (
	Host       = "https://circuloos-platform.eurodyn.com"
	ContextURL = "http://circuloos-ld-context/circuloos-context.jsonld"
)

// Client holds a bearer token for one tenant.
type Client struct {
	Tenant string
	User   string     // partner account (not secret), recorded in the audit log
	Audit  *audit.Log // every upsert is appended here (nil: not audited)
	token  string
	http   *http.Client
}

// Env reads KEY=VALUE lines (PARTNER_USERNAME, PARTNER_PASSWORD, ORION_PEP_SECRET, …).
func Env(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") || !strings.Contains(l, "=") {
			continue
		}
		kv := strings.SplitN(l, "=", 2)
		m[strings.TrimSpace(kv[0])] = strings.Trim(strings.TrimSpace(kv[1]), `"'`)
	}
	return m, sc.Err()
}

// Login gets an orion-pep token for the partner account.
func Login(env map[string]string, tenant string) (*Client, error) {
	for _, k := range []string{"PARTNER_USERNAME", "PARTNER_PASSWORD", "ORION_PEP_SECRET"} {
		if env[k] == "" {
			return nil, fmt.Errorf("%s missing in env file", k)
		}
	}
	form := url.Values{"username": {env["PARTNER_USERNAME"]}, "password": {env["PARTNER_PASSWORD"]},
		"grant_type": {"password"}, "client_id": {"orion-pep"}, "client_secret": {env["ORION_PEP_SECRET"]}}
	hc := &http.Client{Timeout: 60 * time.Second}
	resp, err := hc.PostForm(Host+"/idm/realms/fiware-server/protocol/openid-connect/token", form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		return nil, fmt.Errorf("auth failed (HTTP %d): %s %v", resp.StatusCode, tok.Error, err)
	}
	return &Client{Tenant: tenant, User: env["PARTNER_USERNAME"], token: tok.AccessToken, http: hc}, nil
}

func (c *Client) req(method, path string, body []byte) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, Host+"/kong/keycloak-orion/ngsi-ld/v1"+path, r)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("NGSILD-Tenant", c.Tenant)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Link", `<`+ContextURL+`>; rel="http://www.w3.org/ns/json-ld#context"; type="application/ld+json"`)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(2+attempt*3) * time.Second)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 500 || (resp.StatusCode == 404 && bytes.Contains(b, []byte("Hibernate"))) {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(b))
			time.Sleep(time.Duration(2+attempt*3) * time.Second)
			continue
		}
		return resp.StatusCode, b, nil
	}
	return 0, nil, lastErr
}

// Upsert sends entities to /entityOperations/upsert (update mode: replace attrs given)
// and appends the request and its outcome to the audit log.
func (c *Client) Upsert(entities []map[string]any) (int, []byte, error) {
	body, err := json.Marshal(entities)
	if err != nil {
		return 0, nil, err
	}
	start := time.Now()
	code, b, err := c.req(http.MethodPost, "/entityOperations/upsert", body)
	e := audit.Entry{Kind: audit.PlatformUpsert, Actor: c.User, Target: c.Tenant,
		Op: "POST /ngsi-ld/v1/entityOperations/upsert", PayloadSHA256: audit.SHA256(body), HTTP: code,
		Outcome: "accepted", Seconds: time.Since(start).Seconds()}
	for _, ent := range entities {
		if id, ok := ent["id"].(string); ok {
			e.IDs = append(e.IDs, id)
		}
	}
	if err != nil || (code != http.StatusCreated && code != http.StatusNoContent) {
		e.Outcome, e.Detail = "failed", strings.TrimSpace(fmt.Sprint(truncate(b), " ", errText(err)))
	}
	if aerr := c.Audit.Append(e); aerr != nil {
		return code, b, errors.Join(err, aerr)
	}
	return code, b, err
}

// Delete removes entities via /entityOperations/delete (204: all deleted, 207: partial)
// and appends the request and its outcome to the audit log.
func (c *Client) Delete(ids []string) (int, []byte, error) {
	body, err := json.Marshal(ids)
	if err != nil {
		return 0, nil, err
	}
	start := time.Now()
	code, b, err := c.req(http.MethodPost, "/entityOperations/delete", body)
	e := audit.Entry{Kind: audit.PlatformDelete, Actor: c.User, Target: c.Tenant,
		Op: "POST /ngsi-ld/v1/entityOperations/delete", PayloadSHA256: audit.SHA256(body), HTTP: code,
		Outcome: "accepted", Seconds: time.Since(start).Seconds(), IDs: ids}
	if err != nil || code != http.StatusNoContent {
		e.Outcome, e.Detail = "failed", strings.TrimSpace(fmt.Sprint(truncate(b), " ", errText(err)))
	}
	if aerr := c.Audit.Append(e); aerr != nil {
		return code, b, errors.Join(err, aerr)
	}
	return code, b, err
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Get reads one entity back (normalized representation).
func (c *Client) Get(id string) (map[string]any, int, error) {
	code, b, err := c.req(http.MethodGet, "/entities/"+url.PathEscape(id), nil)
	if err != nil || code != http.StatusOK {
		return nil, code, fmt.Errorf("GET %s: HTTP %d %s %v", id, code, truncate(b), err)
	}
	var e map[string]any
	return e, code, json.Unmarshal(b, &e)
}

func truncate(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}
