// Package worker runs the background loops: sealing records on the ledger,
// registering signing keys, projecting into ClickHouse, and draining the telemetry
// stream. Loops are safe to run in several replicas (row locks, consumer groups,
// idempotent ledger writes).
package worker

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/compliance"
	"github.com/sensifai/roadpass/internal/ledger"
	"github.com/sensifai/roadpass/internal/proof"
	"github.com/sensifai/roadpass/internal/signing"
	"github.com/sensifai/roadpass/internal/store"
)

// Worker holds the loops' dependencies.
type Worker struct {
	St        *store.Store
	CH        *analytics.DB
	Cache     *cache.Cache
	Ledger    ledger.Client
	Projector *analytics.Projector
	Name      string

	SealEvery time.Duration
	SealMax   int
	// Alerts evaluates rules every AlertsEvery (nil: off). One replica at a time.
	Alerts      *alerts.Engine
	AlertsEvery time.Duration
	// Reports e-mails scheduled compliance reports (nil: off). One replica at a time.
	Reports *compliance.Mailer
	// Heartbeat, when set, is a file touched after every loop step so a container
	// health check can tell a live worker from a wedged one (see HeartbeatMaxAge).
	Heartbeat string

	seals atomic.Int64
}

// HeartbeatMaxAge is how stale the heartbeat file may get before the worker is
// considered unhealthy; it covers the longest step backoff.
const HeartbeatMaxAge = 6 * time.Minute

func (w *Worker) beat() {
	if w.Heartbeat == "" {
		return
	}
	now := time.Now()
	if err := os.Chtimes(w.Heartbeat, now, now); err != nil {
		if f, err := os.Create(w.Heartbeat); err == nil {
			_ = f.Close()
		}
	}
}

// Run starts every loop and blocks until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	n := 3
	done := make(chan struct{}, 8)
	go func() { w.loop(ctx, "projector", time.Second, w.project); done <- struct{}{} }()
	// One sealer at a time across replicas: it submits one transaction and waits for
	// its commit before the next, so every block holds one organisation's seal.
	go func() { w.loop(ctx, "sealer", w.SealEvery, w.exclusive(7349100, w.seal)); done <- struct{}{} }()
	go func() { w.telemetry(ctx); done <- struct{}{} }()
	n++
	go func() {
		w.loop(ctx, "ingest-sweep", 5*time.Minute, w.exclusive(7349105, func(c context.Context) error { return w.St.FailStaleIngests(c, 5*time.Minute) }))
		done <- struct{}{}
	}()
	if w.Alerts != nil {
		every := w.AlertsEvery
		if every <= 0 {
			every = 30 * time.Second
		}
		n += 3
		go func() { w.loop(ctx, "alerts", every, w.exclusive(7349101, w.Alerts.Tick)); done <- struct{}{} }()
		go func() {
			w.loop(ctx, "digest", 10*time.Minute, w.exclusive(7349102, w.Alerts.Digest))
			done <- struct{}{}
		}()
		go func() {
			w.loop(ctx, "prune", 6*time.Hour, w.exclusive(7349103, w.Alerts.DB.PruneNotifications))
			done <- struct{}{}
		}()
	}
	if w.Reports != nil {
		n++
		go func() {
			w.loop(ctx, "reports", 10*time.Minute, w.exclusive(7349104, w.Reports.Tick))
			done <- struct{}{}
		}()
	}
	for range n {
		<-done
	}
}

// exclusive runs step only while holding a Postgres advisory lock, so with
// several worker replicas only one evaluates alerts (no duplicate messages).
func (w *Worker) exclusive(lock int64, step func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		conn, err := w.St.DB.Acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		var got bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, lock).Scan(&got); err != nil || !got {
			return err
		}
		defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, lock) //nolint:errcheck // released with the session anyway
		return step(ctx)
	}
}

func (w *Worker) loop(ctx context.Context, name string, every time.Duration, step func(context.Context) error) {
	t := time.NewTicker(every)
	defer t.Stop()
	backoff := every
	for {
		if err := step(ctx); err != nil && !errors.Is(err, context.Canceled) {
			if errors.Is(err, ledger.ErrOffline) {
				slog.Debug(name+": ledger offline", "err", err)
			} else {
				slog.Warn(name+" step failed", "err", err)
			}
			backoff = min(backoff*2, 5*time.Minute)
		} else {
			backoff = every
		}
		w.beat()
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (w *Worker) project(ctx context.Context) error {
	for {
		n, err := w.Projector.Step(ctx, 2000)
		if err != nil || n == 0 {
			return err
		}
	}
}

// ---------------------------------------------------------------- sealing

func (w *Worker) seal(ctx context.Context) error {
	// Never claim leaves for a ledger we cannot reach: a claimed record would sit in
	// a pending seal until the network returns.
	if _, err := w.Ledger.ChainInfo(); err != nil {
		return err
	}
	if err := w.registerKeys(ctx); err != nil {
		return err
	}
	pending, err := w.St.PendingSeals(ctx, 20)
	if err != nil {
		return err
	}
	for _, s := range pending {
		if err := w.submitSeal(ctx, s); err != nil {
			return err
		}
	}
	for range 50 { // at most 50 seals of up to 4096 records each per tick
		newID := func(org string) string { return store.NextSealID(org, time.Now(), w.seals.Add(1)) }
		seal, _, err := w.St.ClaimLeaves(ctx, newID, w.SealMax, proof.MerkleRoot)
		if err != nil || seal == nil {
			return err
		}
		if err := w.submitSeal(ctx, *seal); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) submitSeal(ctx context.Context, s store.Seal) error {
	leaves, err := w.St.SealLeaves(ctx, s.ID)
	if err != nil {
		return err
	}
	in := ledger.SealInput{SealID: s.ID, MerkleRoot: s.MerkleRoot, Leaves: leaves, FirstSeq: s.FirstSeq, LastSeq: s.LastSeq, Provenance: s.Provenance}
	_, txID, err := w.Ledger.SealBatch(in)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		// A previous attempt committed but we never saw the answer: adopt it.
		got, gerr := w.Ledger.GetSeal(s.ID)
		if gerr != nil {
			return gerr
		}
		if got.MerkleRoot != s.MerkleRoot {
			return w.St.MarkSealFailed(ctx, s.ID, "ledger holds a different root for this seal id")
		}
		txID, err = got.TxID, nil
	}
	if err != nil {
		if errors.Is(err, ledger.ErrOffline) {
			return err
		}
		_ = w.St.MarkSealFailed(ctx, s.ID, err.Error())
		return err
	}
	var block int64
	if b, err := w.Ledger.BlockByTx(txID); err == nil {
		block = int64(b.Number)
	}
	if err := w.St.MarkSealed(ctx, s.ID, txID, block, time.Now().UTC()); err != nil {
		return err
	}
	slog.Info("sealed", "seal", s.ID, "records", s.LeafCount, "tx", txID, "block", block)
	if w.Cache != nil {
		w.Cache.Publish(ctx, cache.Event{Kind: "seal", Count: s.LeafCount, Ref: s.ID})
	}
	return nil
}

// registerKeys puts the active keys of verified organisations on the ledger.
func (w *Worker) registerKeys(ctx context.Context) error {
	rows, err := w.St.DB.Query(ctx, `SELECT k.id, k.org_id, o.name, k.public_key, COALESCE(k.user_id,'') FROM signing_keys k JOIN orgs o ON o.id = k.org_id
		WHERE k.ledger_tx_id IS NULL AND k.status = 'active' AND o.status = 'verified' ORDER BY k.created_at LIMIT 50`)
	if err != nil {
		return err
	}
	type key struct {
		id, org, name, holder string
		pub                   []byte
	}
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.org, &k.name, &k.pub, &k.holder); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	rows.Close()
	for _, k := range keys {
		in := ledger.SignerInput{KeyID: k.id, OrgID: k.org, OrgName: k.name, Algorithm: signing.Algorithm,
			PublicKey: base64.StdEncoding.EncodeToString(k.pub), HolderID: k.holder}
		_, txID, err := w.Ledger.RegisterSigner(in)
		if err != nil && strings.Contains(err.Error(), "already registered") {
			got, gerr := w.Ledger.GetSigner(k.id)
			if gerr != nil {
				return gerr
			}
			if got.PublicKey != in.PublicKey {
				slog.Error("ledger holds a different public key", "key", k.id)
				continue
			}
			txID, err = got.TxID, nil
		}
		if err != nil {
			return err
		}
		if err := w.St.MarkKeyRegistered(ctx, k.id, txID); err != nil {
			return err
		}
		slog.Info("signer registered", "key", k.id, "tx", txID)
	}
	return w.revokeKeys(ctx)
}

// revokeKeys records on the ledger that a registered key was retired (rotated)
// or revoked (member left, possible compromise). Signatures made before stay valid.
func (w *Worker) revokeKeys(ctx context.Context) error {
	rows, err := w.St.DB.Query(ctx, `SELECT id, status, COALESCE(revoke_reason,'') FROM signing_keys
		WHERE status <> 'active' AND ledger_tx_id IS NOT NULL AND ledger_revoke_tx_id IS NULL ORDER BY revoked_at LIMIT 50`)
	if err != nil {
		return err
	}
	type end struct{ id, status, reason string }
	var ends []end
	for rows.Next() {
		var e end
		if err := rows.Scan(&e.id, &e.status, &e.reason); err != nil {
			rows.Close()
			return err
		}
		ends = append(ends, e)
	}
	rows.Close()
	for _, e := range ends {
		reason := e.reason
		if reason == "" {
			reason = e.status
		}
		_, txID, err := w.Ledger.RevokeSigner(e.id, reason)
		if err != nil && strings.Contains(err.Error(), "already revoked") {
			got, gerr := w.Ledger.GetSigner(e.id)
			if gerr != nil {
				return gerr
			}
			txID, err = got.TxID, nil
		}
		if err != nil {
			return err
		}
		if err := w.St.MarkKeyRevokedOnLedger(ctx, e.id, txID); err != nil {
			return err
		}
		slog.Info("signer revoked", "key", e.id, "reason", reason, "tx", txID)
	}
	return nil
}

// SealAll seals everything sealable now (CLI: roadpass ledger).
func (w *Worker) SealAll(ctx context.Context) error {
	if w.SealMax == 0 {
		w.SealMax = 4096
	}
	for {
		before, err := w.St.SealStatistics(ctx)
		if err != nil {
			return err
		}
		if err := w.seal(ctx); err != nil {
			return err
		}
		after, err := w.St.SealStatistics(ctx)
		if err != nil {
			return err
		}
		if after.Sealed == before.Sealed {
			return nil
		}
	}
}

// ---------------------------------------------------------------- telemetry

func (w *Worker) telemetry(ctx context.Context) {
	if w.Cache == nil || w.CH == nil {
		return
	}
	if err := w.Cache.EnsureTelemetryGroup(ctx); err != nil {
		slog.Warn("telemetry stream", "err", err)
	}
	for ctx.Err() == nil {
		batch, err := w.Cache.NextTelemetry(ctx, w.Name, 2*time.Second, time.Minute, 500)
		if err != nil {
			slog.Warn("telemetry read", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if len(batch.MsgIDs) == 0 {
			continue
		}
		if len(batch.Readings) > 0 {
			if err := w.CH.InsertReadings(ctx, batch.Readings); err != nil {
				slog.Warn("telemetry insert", "err", err)
				continue // not acknowledged: redelivered after the stale period
			}
		}
		if len(batch.Cycles) > 0 {
			if err := w.CH.InsertCycles(ctx, batch.Cycles); err != nil {
				slog.Warn("telemetry insert", "err", err)
				continue
			}
		}
		_ = w.Cache.AckTelemetry(ctx, batch.MsgIDs...)
	}
}
