package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/domain"
	"github.com/sensifai/roadpass/internal/ledger"
	"github.com/sensifai/roadpass/internal/proof"
	"github.com/sensifai/roadpass/internal/store"
)

type ledgerSummary struct {
	Enabled  bool              `json:"enabled"`
	Online   bool              `json:"online"`
	Channel  string            `json:"channel"`
	Chain    *ledger.ChainInfo `json:"chain,omitempty"`
	Platform store.SealStats   `json:"platform"`
	Mine     *store.SealStats  `json:"mine,omitempty"`
	Error    string            `json:"error,omitempty"`
}

func (s *Server) ledgerSummary(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	v, err := cache.Remember(r.Context(), s.Cache, "ledger:summary:"+p.Org.ID, 10*time.Second, func(ctx context.Context) (ledgerSummary, error) {
		out := ledgerSummary{Enabled: s.Cfg.LedgerEnabled(), Channel: s.Cfg.FabricChannel}
		st, err := s.St.SealStatistics(ctx)
		if err != nil {
			return out, err
		}
		out.Platform = st
		if !p.Viewer.Oversight {
			mine, err := s.St.OrgSealStats(ctx, p.Org.ID)
			if err != nil {
				return out, err
			}
			out.Mine = &mine
		}
		if info, err := s.Ledger.ChainInfo(); err == nil {
			out.Chain, out.Online = info, true
		} else {
			out.Error = err.Error()
		}
		return out, nil
	})
	answer(w, r, v, err)
}

func (s *Server) ledgerSeals(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	org := p.Org.ID
	if p.Viewer.Oversight {
		org = r.URL.Query().Get("org")
	}
	seals, err := s.St.SealsFor(r.Context(), org, limit)
	answer(w, r, nonNil(seals), err)
}

// ledgerOrgs shows the ledger split by organisation: every seal is dedicated to one
// organisation, so each one owns its seal transactions and the blocks holding them.
// Oversight viewers see every organisation; others only their own.
func (s *Server) ledgerOrgs(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	org := p.Org.ID
	if p.Viewer.Oversight {
		org = ""
	}
	out, err := s.St.LedgerByOrg(r.Context(), org)
	answer(w, r, nonNil(out), err)
}

// ledgerOrgSeals lists the seal transactions dedicated to one organisation
// ("shared": the mixed seals made before seals were dedicated).
func (s *Server) ledgerOrgSeals(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	org := r.PathValue("id")
	if org == "shared" {
		org = ""
	}
	if !p.Viewer.Oversight && org != p.Org.ID {
		fail(w, r, store.ErrNotFound)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	seals, err := s.St.DedicatedSeals(r.Context(), org, limit)
	answer(w, r, nonNil(seals), err)
}

type sealDetail struct {
	Seal    store.Seal          `json:"seal"`
	OnChain *ledger.SealOnChain `json:"onChain,omitempty"`
	Block   *ledger.Block       `json:"block,omitempty"`
	Records []store.SealMember  `json:"records"`
	Mine    int                 `json:"mine"`
	Error   string              `json:"ledgerError,omitempty"`
}

func (s *Server) ledgerSeal(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	seal, err := s.St.GetSeal(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	members, mine, err := s.St.SealMembers(r.Context(), seal.ID, p.Viewer, 500)
	if err != nil {
		fail(w, r, err)
		return
	}
	if mine == 0 && !p.Viewer.Oversight {
		fail(w, r, store.ErrNotFound)
		return
	}
	out := sealDetail{Seal: seal, Records: nonNil(members), Mine: mine}
	if seal.Status == "sealed" {
		if oc, err := s.Ledger.GetSeal(seal.ID); err == nil {
			out.OnChain = oc
		} else {
			out.Error = err.Error()
		}
		if b, err := s.blockFor(seal.TxID); err == nil {
			out.Block = s.maskBlock(r.Context(), b, p)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) blockFor(txID string) (*ledger.Block, error) {
	if txID == "" {
		return nil, store.ErrNotFound
	}
	return s.Ledger.BlockByTx(txID)
}

// maskBlock hides other organisations' transactions from a non-oversight viewer:
// it sees that a transaction exists, not what it sealed.
func (s *Server) maskBlock(ctx context.Context, b *ledger.Block, p *Principal) *ledger.Block {
	if p.Viewer.Oversight {
		return b
	}
	mySeals, _ := s.St.OrgSealIDs(ctx, p.Org.ID)
	c := *b
	c.Txs = make([]ledger.Tx, len(b.Txs))
	for i, tx := range b.Txs {
		mine := mySeals[tx.Subject] || strings.HasPrefix(tx.Subject, p.Org.ID+":")
		if !mine {
			tx.Subject, tx.Function = "", "other organisation"
		}
		c.Txs[i] = tx
	}
	return &c
}

func (s *Server) ledgerBlocks(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	if !p.Viewer.Oversight {
		writeProblem(w, r, http.StatusForbidden, "Operators and auditors only", "Open a seal to see the block that holds it.")
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
	var out []*ledger.Block
	for i := 0; i < n && from > 0; i++ {
		from--
		b, err := s.Ledger.Block(from)
		if err != nil {
			fail(w, r, err)
			return
		}
		out = append(out, b)
	}
	writeJSON(w, http.StatusOK, map[string]any{"chain": info, "blocks": nonNil(out)})
}

func (s *Server) ledgerBlock(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	n, err := strconv.ParseUint(r.PathValue("n"), 10, 64)
	if err != nil {
		fail(w, r, store.ErrNotFound)
		return
	}
	b, err := s.Ledger.Block(n)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.maskBlock(r.Context(), b, p))
}

func (s *Server) ledgerSigners(w http.ResponseWriter, r *http.Request) {
	p := principal(r.Context())
	keys, err := s.St.SigningKeys(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	type view struct {
		Key     string                `json:"keyId"`
		OrgID   string                `json:"orgId"`
		OnChain *ledger.SignerOnChain `json:"onChain,omitempty"`
		Status  string                `json:"status"`
	}
	out := []view{}
	for _, k := range keys {
		if k.OrgID != p.Org.ID && !p.Viewer.Oversight {
			continue
		}
		v := view{Key: k.ID, OrgID: k.OrgID, Status: k.Status}
		if k.LedgerTxID != "" && len(out) < 60 {
			if oc, err := s.Ledger.GetSigner(k.ID); err == nil {
				v.OnChain = oc
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// verifyAnything checks a fingerprint or a record document against the ledger.
// Public: anyone holding a record can check it without an account.
func (s *Server) verifyAnything(w http.ResponseWriter, r *http.Request) {
	if ok, _, _ := s.Cache.Allow(r.Context(), "verify:"+clientIP(r), 60, time.Minute); !ok {
		writeProblem(w, r, http.StatusTooManyRequests, "Rate limit", "")
		return
	}
	var in struct {
		Hash   string          `json:"hash"`
		Record json.RawMessage `json:"record"`
	}
	if !decode(w, r, &in, 1<<20) {
		return
	}
	hash := strings.ToLower(strings.TrimSpace(in.Hash))
	if len(in.Record) > 0 {
		var doc domain.HashDoc
		if err := json.Unmarshal(in.Record, &doc); err != nil {
			writeProblem(w, r, http.StatusUnprocessableEntity, "Not a ROADPASS record", err.Error())
			return
		}
		h, err := proof.HashJSON(doc)
		if err != nil {
			fail(w, r, err)
			return
		}
		hash = h
	}
	if len(hash) != 64 {
		writeProblem(w, r, http.StatusUnprocessableEntity, "Give a 64-character fingerprint or a record document", "")
		return
	}
	res, err := s.St.VerifyHash(r.Context(), hash)
	if err != nil {
		fail(w, r, err)
		return
	}
	if res.SealID != "" && res.SealStatus == "sealed" {
		if leaves, err := s.St.SealLeaves(r.Context(), res.SealID); err == nil && res.LeafIndex < len(leaves) {
			if root, path, err := proof.InclusionProof(leaves, res.LeafIndex); err == nil {
				res.Path, res.ComputedRoot = path, root
				res.Included = proof.VerifyInclusion(hash, path, res.MerkleRoot)
			}
		}
		if oc, err := s.Ledger.GetSeal(res.SealID); err == nil {
			res.LedgerRoot = oc.MerkleRoot
			res.Anchored = oc.MerkleRoot == res.MerkleRoot
		} else {
			res.LedgerError = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, res)
}
