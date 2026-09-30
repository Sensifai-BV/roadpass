package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/ledger"
	"github.com/sensifai/roadpass/internal/store"
)

// The public explorer: no sign-in, read-only. It shows every block and transaction
// of the ledger, but what a transaction reveals depends on its organisation:
//   - private (the default): hashes only: transaction id, block, Merkle root. No
//     organisation, no seal id (it carries the organisation's id), no record counts.
//   - public (the admin switched "public ledger" on): the organisation, its seal,
//     the records in it and its supply-chain details.

func (s *Server) explorerGate(w http.ResponseWriter, r *http.Request) bool {
	if ok, _, _ := s.Cache.Allow(r.Context(), "explorer:"+clientIP(r), 240, time.Minute); !ok {
		writeProblem(w, r, http.StatusTooManyRequests, "Rate limit", "")
		return false
	}
	w.Header().Set("Cache-Control", "no-cache")
	return true
}

type explorerSummary struct {
	Online  bool              `json:"online"`
	Channel string            `json:"channel"`
	Chain   *ledger.ChainInfo `json:"chain,omitempty"`
	store.ExplorerTotals
}

func (s *Server) explorerSummary(w http.ResponseWriter, r *http.Request) {
	if !s.explorerGate(w, r) {
		return
	}
	t, err := s.St.ExplorerTotals(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	out := explorerSummary{Channel: s.Cfg.FabricChannel, ExplorerTotals: t}
	if info, err := s.Ledger.ChainInfo(); err == nil {
		out.Chain, out.Online = info, true
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) explorerOrgs(w http.ResponseWriter, r *http.Request) {
	if !s.explorerGate(w, r) {
		return
	}
	out, err := s.St.PublicOrgs(r.Context())
	answer(w, r, nonNil(out), err)
}

func (s *Server) explorerOrg(w http.ResponseWriter, r *http.Request) {
	if !s.explorerGate(w, r) {
		return
	}
	d, err := s.St.PublicOrgDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	seals, err := s.St.PublicSealsOf(r.Context(), d.Org.ID, 100)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"detail": d, "seals": nonNil(seals)})
}

// exTx is a transaction as the public sees it. Public carries the organisation's
// details; a private transaction has only hashes.
type exTx struct {
	TxID       string    `json:"txId"`
	Timestamp  time.Time `json:"timestamp"`
	Type       string    `json:"type"`
	Function   string    `json:"function,omitempty"`
	CreatorMSP string    `json:"creatorMsp"`
	Valid      bool      `json:"valid"`
	Validation string    `json:"validation"`
	Public     bool      `json:"public"`
	MerkleRoot string    `json:"merkleRoot,omitempty"` // seal transactions
	SealID     string    `json:"sealId,omitempty"`     // public only
	OrgID      string    `json:"orgId,omitempty"`      // public only
	OrgName    string    `json:"orgName,omitempty"`    // public only
	Records    int       `json:"records,omitempty"`    // public only
	Provenance string    `json:"provenance,omitempty"` // public only
	KeyID      string    `json:"keyId,omitempty"`      // public only (RegisterSigner)
}

type exBlock struct {
	Number       uint64 `json:"number"`
	DataHash     string `json:"dataHash"`
	PreviousHash string `json:"previousHash"`
	SizeBytes    int    `json:"sizeBytes"`
	Txs          []exTx `json:"txs"`
}

func (s *Server) explorerBuild(r *http.Request, bs []*ledger.Block) []exBlock {
	nums := make([]int64, 0, len(bs))
	var keyIDs []string
	for _, b := range bs {
		nums = append(nums, int64(b.Number))
		for _, t := range b.Txs {
			keyIDs = append(keyIDs, t.Subject)
		}
	}
	seals, _ := s.St.ChainSeals(r.Context(), nums)
	byTx := map[string]store.ChainSeal{}
	orgIDs := map[string]bool{}
	for _, x := range seals {
		byTx[x.TxID] = x
		if x.Public {
			orgIDs[x.OrgID] = true
		}
	}
	keyOrg, _ := s.St.PublicKeyOrgs(r.Context(), keyIDs)
	for _, o := range keyOrg {
		orgIDs[o] = true
	}
	names := map[string]string{}
	if len(orgIDs) > 0 {
		if list, err := s.St.PublicOrgs(r.Context()); err == nil {
			for _, o := range list {
				names[o.ID] = o.Name
			}
		}
	}
	out := make([]exBlock, 0, len(bs))
	for _, b := range bs {
		eb := exBlock{Number: b.Number, DataHash: b.DataHash, PreviousHash: b.PreviousHash, SizeBytes: b.SizeBytes, Txs: []exTx{}}
		for _, t := range b.Txs {
			x := exTx{TxID: t.TxID, Timestamp: t.Timestamp, Type: t.Type, Function: t.Function, CreatorMSP: t.CreatorMSP, Valid: t.Valid, Validation: t.Validation}
			if cs, ok := byTx[t.TxID]; ok {
				x.MerkleRoot = cs.MerkleRoot
				if cs.Public {
					x.Public, x.SealID, x.OrgID, x.OrgName = true, cs.ID, cs.OrgID, names[cs.OrgID]
					x.Records, x.Provenance = cs.LeafCount, cs.Provenance
				}
			} else if org, ok := keyOrg[t.Subject]; ok {
				x.Public, x.OrgID, x.OrgName, x.KeyID = true, org, names[org], t.Subject
			}
			eb.Txs = append(eb.Txs, x)
		}
		out = append(out, eb)
	}
	return out
}

func (s *Server) explorerBlocks(w http.ResponseWriter, r *http.Request) {
	if !s.explorerGate(w, r) {
		return
	}
	info, err := s.Ledger.ChainInfo()
	if err != nil {
		fail(w, r, err)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 || n > 50 {
		n = 20
	}
	from := info.Height
	if b, err := strconv.ParseUint(r.URL.Query().Get("before"), 10, 64); err == nil && b < from {
		from = b
	}
	var bs []*ledger.Block
	for i := 0; i < n && from > 0; i++ {
		from--
		b, err := s.Ledger.Block(from)
		if err != nil {
			fail(w, r, err)
			return
		}
		bs = append(bs, b)
	}
	writeJSON(w, http.StatusOK, map[string]any{"height": info.Height, "blocks": s.explorerBuild(r, bs)})
}

func (s *Server) explorerBlock(w http.ResponseWriter, r *http.Request) {
	if !s.explorerGate(w, r) {
		return
	}
	n, err := strconv.ParseUint(r.PathValue("n"), 10, 64)
	if err != nil {
		fail(w, r, store.ErrNotFound)
		return
	}
	b, err := s.Ledger.Block(n)
	if err != nil {
		fail(w, r, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, s.explorerBuild(r, []*ledger.Block{b})[0])
}

func (s *Server) explorerSeal(w http.ResponseWriter, r *http.Request) {
	if !s.explorerGate(w, r) {
		return
	}
	seal, err := s.St.PublicSeal(r.Context(), r.PathValue("id")) // a private seal looks like nothing
	if err != nil {
		fail(w, r, err)
		return
	}
	org, _ := s.St.Org(r.Context(), seal.OrgID)
	recs, err := s.St.PublicRecords(r.Context(), seal.ID, 500)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := map[string]any{"seal": seal, "orgName": org.Name, "orgKind": org.Kind, "records": nonNil(recs)}
	if oc, err := s.Ledger.GetSeal(seal.ID); err == nil {
		out["onChain"] = oc
	} else {
		out["ledgerError"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// explorerSearch takes a block number, seal id, transaction id, Merkle root or the
// fingerprint of a record in a public seal, and says where to go.
func (s *Server) explorerSearch(w http.ResponseWriter, r *http.Request) {
	if !s.explorerGate(w, r) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 200 {
		fail(w, r, store.ErrNotFound)
		return
	}
	if n, err := strconv.ParseUint(q, 10, 64); err == nil {
		if _, err := s.Ledger.Block(n); err == nil {
			writeJSON(w, http.StatusOK, map[string]string{"kind": "block", "id": q})
			return
		}
	}
	kind, id, err := s.St.ExplorerSearch(r.Context(), q)
	if err != nil {
		fail(w, r, err)
		return
	}
	if kind == "" {
		fail(w, r, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": kind, "id": id})
}

// setPublicLedger lets an organisation admin list or unlist its ledger data.
func (s *Server) setPublicLedger(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	var in struct {
		Public bool `json:"public"`
	}
	if !decode(w, r, &in, 1024) {
		return
	}
	if err := s.St.SetPublicLedger(r.Context(), p.Org.ID, in.Public); err != nil {
		fail(w, r, err)
		return
	}
	_ = s.St.Audit(r.Context(), p.Org.ID, p.Actor(), "org.public_ledger", p.Org.ID, map[string]any{"public": in.Public})
	p.Org.PublicLedger = in.Public
	writeJSON(w, http.StatusOK, p.Org)
}
