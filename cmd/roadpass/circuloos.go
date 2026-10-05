package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/audit"
	"github.com/sensifai/roadpass/internal/circuloos"
	"github.com/sensifai/roadpass/internal/config"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/ledger"
	"github.com/sensifai/roadpass/internal/ngsild"
	"github.com/sensifai/roadpass/internal/proof"
	"github.com/sensifai/roadpass/internal/store"
)

// cmdCirculoos exports sealed passports to the CIRCULOOS Data Platform as NGSI-LD
// entities, then reads every one back, recomputes its fingerprint from the
// platform's copy and checks it against the seal root on the Fabric ledger:
// the platform copy is provably the sealed copy.
func cmdCirculoos(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("circuloos", flag.ExitOnError)
	envFile := fs.String("env", "", "env file with PARTNER_USERNAME, PARTNER_PASSWORD, ORION_PEP_SECRET")
	tenant := fs.String("tenant", "circuloos_roadpass", "NGSILD-Tenant to write to")
	types := fs.String("types", "road_section,asphalt_lot,additive_batch", "record types to export")
	limit := fs.Int("limit", 200, "at most this many records (newest first)")
	chunk := fs.Int("chunk", 25, "entities per upsert request")
	dry := fs.Bool("dry-run", false, "build the entities and verify locally; send nothing")
	report := fs.String("report", ".run/circuloos_report.json", "where to write the report")
	_ = fs.Parse(args)

	if strings.HasPrefix(*tenant, "circuloos_plasmix") {
		return fmt.Errorf("refusing to write to %s: that is PLASMIX ROAD's operational tenant", *tenant)
	}
	st, err := store.Open(ctx, cfg.DatabaseURL, 4)
	if err != nil {
		return err
	}
	defer st.Close()
	var led ledger.Client = ledger.Offline{}
	if cfg.LedgerEnabled() {
		led = ledger.NewFabric(cfg.FabricDir, cfg.FabricOrg, cfg.FabricPeer, cfg.FabricChannel)
	}

	var ts []domain.RecordType
	for _, t := range strings.Split(*types, ",") {
		if domain.RecordType(t).Valid() {
			ts = append(ts, domain.RecordType(t))
		}
	}
	// Only records sealed on the ledger leave ROADPASS.
	var recs []domain.Record
	f := store.Filter{Types: ts, Limit: 500}
	for len(recs) < *limit {
		page, next, err := st.Records(ctx, store.SystemViewer, f)
		if err != nil {
			return err
		}
		for _, r := range page {
			if r.Sealed && len(recs) < *limit {
				recs = append(recs, r)
			}
		}
		if next == nil {
			break
		}
		f.Before = next
	}
	resolve := func(id string) (domain.RecordType, bool) {
		for _, t := range domain.RecordTypes {
			if strings.HasPrefix(id, domain.Types[t].Prefix+"-") {
				return t, true
			}
		}
		return "", false
	}
	entities := make([]map[string]any, len(recs))
	for i := range recs {
		r := &recs[i]
		li := r.LeafIndex
		pr := &ngsild.Proof{ContentHash: r.ContentHash, Algorithm: "sha256/jcs-rfc8785/roadpass-record-v1", Status: string(r.Status),
			SealID: r.SealID, LeafIndex: &li}
		if r.Signature != nil {
			pr.KeyID, pr.Signature = r.Signature.KeyID, base64.StdEncoding.EncodeToString(r.Signature.Value)
		}
		entities[i] = ngsild.FromRecord(r, nil, pr, resolve)
	}
	fmt.Printf("%d sealed records → tenant %s\n", len(entities), *tenant)

	out := map[string]any{"tenant": *tenant, "at": time.Now().UTC().Format(time.RFC3339), "sent": len(entities)}
	var verify func(id string) (map[string]any, error)
	if *dry {
		verify = func(id string) (map[string]any, error) {
			for _, e := range entities {
				if e["id"] == id {
					return roundTrip(e)
				}
			}
			return nil, errors.New("not built")
		}
	} else {
		if *envFile == "" {
			return errors.New("-env is required (see reference/circuloos-data-platform/explore/explore.env.template)")
		}
		env, err := circuloos.Env(*envFile)
		if err != nil {
			return err
		}
		cl, err := circuloos.Login(env, *tenant)
		if err != nil {
			return err
		}
		_ = os.MkdirAll(filepath.Dir(*report), 0o755)
		cl.Audit = audit.Open(filepath.Join(filepath.Dir(*report), "circuloos_audit.jsonl"))
		out["user"] = cl.User
		accepted := 0
		for i := 0; i < len(entities); i += *chunk {
			j := min(i+*chunk, len(entities))
			code, body, err := cl.Upsert(entities[i:j])
			if err == nil && (code == 201 || code == 204) {
				accepted += j - i
			}
			slog.Info("upsert", "from", i+1, "to", j, "http", code, "body", trimTo(string(body), 160))
		}
		out["upsertAccepted"] = accepted
		verify = func(id string) (map[string]any, error) { e, _, err := cl.Get(id); return e, err }
	}

	counts := map[string]int{}
	var problems []map[string]any
	roots := map[string]string{}
	for i := range recs {
		r := &recs[i]
		id := ngsild.EntityID(r.Type, r.ID)
		remote, err := verify(id)
		if err != nil {
			counts["read_failed"]++
			problems = append(problems, map[string]any{"id": id, "error": err.Error()})
			continue
		}
		back, err := ngsild.ToRecord(remote, r.OrgID)
		if err != nil {
			counts["unreadable"]++
			problems = append(problems, map[string]any{"id": id, "error": err.Error()})
			continue
		}
		back.ID, back.Version, back.PrevHash = r.ID, r.Version, r.PrevHash
		back.Counterparty = r.Counterparty
		h, err := back.ComputeHash()
		if err != nil || h != r.ContentHash {
			counts["mismatch"]++
			problems = append(problems, map[string]any{"id": id, "remoteHash": h, "sealedHash": r.ContentHash})
			continue
		}
		seal, err := st.GetSeal(ctx, r.SealID)
		if err != nil {
			return err
		}
		leaves, err := st.SealLeaves(ctx, r.SealID)
		if err != nil {
			return err
		}
		_, path, err := proof.InclusionProof(leaves, r.LeafIndex)
		if err != nil || !proof.VerifyInclusion(h, path, seal.MerkleRoot) {
			counts["not_in_seal"]++
			continue
		}
		if _, ok := roots[seal.ID]; !ok {
			if oc, err := led.GetSeal(seal.ID); err == nil {
				roots[seal.ID] = oc.MerkleRoot
			} else {
				roots[seal.ID] = "unavailable: " + err.Error()
			}
		}
		if roots[seal.ID] == seal.MerkleRoot {
			counts["verified_on_ledger"]++
		} else {
			counts["sealed_locally_ledger_unchecked"]++
		}
	}
	out["readBack"], out["problems"] = counts, problems
	fmt.Printf("read back: %v\n", counts)
	b, _ := json.MarshalIndent(out, "", "  ")
	_ = os.MkdirAll(filepath.Dir(*report), 0o755)
	if err := os.WriteFile(*report, b, 0o644); err != nil {
		return err
	}
	if counts["mismatch"] > 0 || counts["not_in_seal"] > 0 {
		return fmt.Errorf("%d records do not match their seal", counts["mismatch"]+counts["not_in_seal"])
	}
	return nil
}

func roundTrip(e map[string]any) (map[string]any, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(b, &out)
}

func trimTo(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
