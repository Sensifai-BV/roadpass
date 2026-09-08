// Package ledger connects to the local ROADPASS Fabric network through the
// Fabric Gateway (peer >= 2.4) using the cryptogen identities in fabric/crypto.
package ledger

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hyperledger/fabric-gateway/pkg/client"
	"github.com/hyperledger/fabric-gateway/pkg/hash"
	"github.com/hyperledger/fabric-gateway/pkg/identity"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	gwproto "github.com/hyperledger/fabric-protos-go-apiv2/gateway"
	"github.com/sensifai/roadpass/internal/audit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Org describes one peer organisation of the demo network.
type Org struct {
	Name, MSP, Peer, Host string
}

// Orgs of the demo network (see fabric/crypto-config.yaml).
var Orgs = map[string]Org{
	"sensifai": {"sensifai", "SensifaiMSP", "127.0.0.1:7051", "peer0.sensifai.roadpass.local"},
	"plasmix":  {"plasmix", "PlasmixMSP", "127.0.0.1:9051", "peer0.plasmix.roadpass.local"},
}

func init() {
	if s := os.Getenv("FABRIC_SENSIFAI_PEER"); s != "" {
		o := Orgs["sensifai"]
		o.Peer = s
		Orgs["sensifai"] = o
	}
	if p := os.Getenv("FABRIC_PLASMIX_PEER"); p != "" {
		o := Orgs["plasmix"]
		o.Peer = p
		Orgs["plasmix"] = o
	}
}

// OrgFor returns a demo-network org, or the client-only actor org that
// `network.sh onboard <name>` creates (MSP <Name>MSP, no peer of its own).
func OrgFor(name string) Org {
	if o, ok := Orgs[name]; ok {
		return o
	}
	if name == "" {
		return Org{}
	}
	return Org{Name: name, MSP: strings.ToUpper(name[:1]) + name[1:] + "MSP"}
}

// Session is a gateway connection for one client identity.
type Session struct {
	Org   Org
	Audit *audit.Log // every submitted transaction is appended here (nil: not audited)
	conn  *grpc.ClientConn
	gw    *client.Gateway
}

// Connect opens a gateway session as User1@<org>, optionally through another org's peer
// (required for onboarded client-only orgs).
func Connect(fabricDir, org, viaPeerOrg string) (*Session, error) {
	o := OrgFor(org)
	if o.Name == "" {
		return nil, fmt.Errorf("empty org name")
	}
	via := o
	if viaPeerOrg != "" {
		via = OrgFor(viaPeerOrg)
	}
	if via.Peer == "" {
		return nil, fmt.Errorf("org %q has no peer of its own; connect through a member peer", via.Name)
	}
	base := filepath.Join(fabricDir, "crypto", "peerOrganizations", o.Name+".roadpass.local")
	user := filepath.Join(base, "users", "User1@"+o.Name+".roadpass.local", "msp")
	certPEM, err := firstFile(filepath.Join(user, "signcerts"))
	if err != nil {
		return nil, fmt.Errorf("identity not found (is the network up?): %w", err)
	}
	cert, err := identity.CertificateFromPEM(certPEM)
	if err != nil {
		return nil, err
	}
	id, err := identity.NewX509Identity(o.MSP, cert)
	if err != nil {
		return nil, err
	}
	keyPEM, err := firstFile(filepath.Join(user, "keystore"))
	if err != nil {
		return nil, err
	}
	key, err := identity.PrivateKeyFromPEM(keyPEM)
	if err != nil {
		return nil, err
	}
	sign, err := identity.NewPrivateKeySign(key)
	if err != nil {
		return nil, err
	}

	tlsCAPEM, err := os.ReadFile(filepath.Join(fabricDir, "crypto", "peerOrganizations", via.Name+".roadpass.local",
		"peers", via.Host, "tls", "ca.crt"))
	if err != nil {
		return nil, err
	}
	tlsCA, err := identity.CertificateFromPEM(tlsCAPEM)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(tlsCA)
	conn, err := grpc.NewClient(via.Peer, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(pool, via.Host)))
	if err != nil {
		return nil, err
	}
	gw, err := client.Connect(id, client.WithSign(sign), client.WithHash(hash.SHA256), client.WithClientConnection(conn),
		client.WithEvaluateTimeout(15*time.Second), client.WithEndorseTimeout(30*time.Second),
		client.WithSubmitTimeout(30*time.Second), client.WithCommitStatusTimeout(2*time.Minute))
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &Session{Org: o, conn: conn, gw: gw}, nil
}

// Contract is the roadpass-proof contract on one channel. Evaluations pass straight
// through; SubmitTransaction is audited.
type Contract struct {
	*client.Contract
	s       *Session
	channel string
}

// Contract returns the roadpass-proof contract on a channel.
func (s *Session) Contract(channel string) *Contract {
	return &Contract{s.gw.GetNetwork(channel).GetContract("roadpass-proof"), s, channel}
}

// SubmitTransaction endorses, submits and waits for the commit of one transaction, and
// appends the attempt — committed or refused, with its transaction id — to the audit log.
func (c *Contract) SubmitTransaction(name string, args ...string) ([]byte, error) {
	start := time.Now()
	var txID string
	res, err := func() ([]byte, error) {
		p, err := c.NewProposal(name, client.WithArguments(args...))
		if err != nil {
			return nil, err
		}
		txID = p.TransactionID()
		tx, err := p.Endorse()
		if err != nil {
			return nil, err
		}
		commit, err := tx.Submit()
		if err != nil {
			return nil, err
		}
		st, err := commit.Status()
		if err != nil {
			return nil, err
		}
		if !st.Successful {
			return nil, fmt.Errorf("transaction %s failed to commit with status code %d (%s)", txID, int32(st.Code), st.Code)
		}
		return tx.Result(), nil
	}()
	e := audit.Entry{Kind: audit.LedgerSubmit, Actor: c.s.Org.MSP, Target: c.channel + "/" + c.ChaincodeName(),
		Op: name, Subject: subject(args), TxID: txID, Outcome: "committed",
		Seconds: time.Since(start).Seconds()}
	if payload, jerr := json.Marshal(args); jerr == nil {
		e.PayloadSHA256 = audit.SHA256(payload)
	}
	if err != nil {
		e.Outcome, e.Detail = "rejected", ErrText(err)
	}
	if aerr := c.s.Audit.Append(e); aerr != nil {
		return res, errors.Join(err, fmt.Errorf("transaction %s: %w", txID, aerr))
	}
	return res, err
}

// subject names what a transaction writes: its first argument, or the id inside a JSON argument.
func subject(args []string) string {
	if len(args) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(args[0]), &m) == nil {
		for _, k := range []string{"recordId", "packageId"} {
			if v, ok := m[k].(string); ok {
				return v
			}
		}
		return ""
	}
	if len(args[0]) > 200 {
		return args[0][:200]
	}
	return args[0]
}

// ChainHeight returns the block height of a channel, read through qscc with this
// session's identity (so it is subject to the channel's Readers policy).
func (s *Session) ChainHeight(channel string) (uint64, error) {
	out, err := s.gw.GetNetwork(channel).GetContract("qscc").EvaluateTransaction("GetChainInfo", channel)
	if err != nil {
		return 0, err
	}
	var info common.BlockchainInfo
	if err := proto.Unmarshal(out, &info); err != nil {
		return 0, err
	}
	return info.GetHeight(), nil
}

// Close releases the connection.
func (s *Session) Close() {
	s.gw.Close()
	s.conn.Close()
}

// ErrText flattens a gateway error including chaincode/peer details.
func ErrText(err error) string {
	if err == nil {
		return ""
	}
	parts := []string{err.Error()}
	var st interface{ GRPCStatus() *status.Status }
	if !errors.As(err, &st) {
		if s, ok := status.FromError(err); ok {
			st = statusWrap{s}
		}
	}
	if st != nil {
		for _, d := range st.GRPCStatus().Details() {
			if ed, ok := d.(*gwproto.ErrorDetail); ok {
				parts = append(parts, fmt.Sprintf("%s: %s", ed.GetAddress(), ed.GetMessage()))
			}
		}
	}
	return strings.Join(parts, " | ")
}

func firstFile(dir string) ([]byte, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		if !e.IsDir() {
			return os.ReadFile(filepath.Join(dir, e.Name()))
		}
	}
	return nil, fmt.Errorf("no file in %s", dir)
}

type statusWrap struct{ s *status.Status }

func (w statusWrap) GRPCStatus() *status.Status { return w.s }
