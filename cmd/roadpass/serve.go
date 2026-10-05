package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sensifai/roadpass/internal/alerts"
	"github.com/sensifai/roadpass/internal/analytics"
	"github.com/sensifai/roadpass/internal/api"
	"github.com/sensifai/roadpass/internal/cache"
	"github.com/sensifai/roadpass/internal/compliance"
	"github.com/sensifai/roadpass/internal/config"
	"github.com/sensifai/roadpass/internal/demo"
	"github.com/sensifai/roadpass/internal/ingest"
	"github.com/sensifai/roadpass/internal/keyring"
	"github.com/sensifai/roadpass/internal/lca"
	"github.com/sensifai/roadpass/internal/ledger"
	"github.com/sensifai/roadpass/internal/passport"
	"github.com/sensifai/roadpass/internal/secrets"
	"github.com/sensifai/roadpass/internal/signing"
	"github.com/sensifai/roadpass/internal/store"
	"github.com/sensifai/roadpass/internal/vault"
	"github.com/sensifai/roadpass/internal/worker"
)

type deps struct {
	st     *store.Store
	ch     *analytics.DB
	cache  *cache.Cache
	led    ledger.Client
	keys   *keyring.Keyring
	notify *alerts.Notifier
}

// newNotifier wires notifications: secrets in the vault (or sealed locally),
// the operator's mail relay and SMS account, and browser push.
func newNotifier(cfg config.Config, st *store.Store, c *cache.Cache, keys *keyring.Keyring) (*alerts.Notifier, error) {
	master, err := signing.MasterKey(cfg.SigningKey)
	if err != nil {
		return nil, err
	}
	var kv *vault.KV
	if keys.Transit != nil {
		kv = keys.Transit.KV(cfg.VaultKVMount)
	}
	sec, err := secrets.New(kv, st.DB, master)
	if err != nil {
		return nil, err
	}
	db := &alerts.DB{P: st.DB}
	return &alerts.Notifier{DB: db, Secrets: sec, Cache: c, PublicURL: cfg.PublicURL,
		Push: &alerts.Push{DB: db, Secrets: sec, Contact: cfg.PushContact},
		Senders: &alerts.Senders{AllowPrivate: cfg.AlertsAllowPrivate, TelegramBase: cfg.TelegramBase, Load: alerts.PlatformMail(st.DB, sec),
			SMTP:   alerts.SMTPConfig{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUser, Password: cfg.SMTPPassword, From: cfg.SMTPFrom, TLS: cfg.SMTPTLS},
			Twilio: alerts.TwilioConfig{Base: cfg.TwilioBase, AccountSID: cfg.TwilioSID, AuthToken: cfg.TwilioToken, From: cfg.TwilioFrom}}}, nil
}

// newKeyring opens the signing keys: OpenBao Transit when configured, plus the
// local sealing key for keys created before (or without) a vault.
func newKeyring(cfg config.Config, st *store.Store) (*keyring.Keyring, error) {
	local, err := signing.NewVault(cfg.SigningKey)
	if err != nil {
		return nil, err
	}
	kr := &keyring.Keyring{St: st, Local: local}
	if cfg.VaultAddr != "" {
		if kr.Transit, err = vault.New(vault.Config{Addr: cfg.VaultAddr, Token: cfg.VaultToken, TokenFile: cfg.VaultTokenFile, Mount: cfg.VaultMount}); err != nil {
			return nil, err
		}
	}
	return kr, nil
}

func open(ctx context.Context, cfg config.Config) (*deps, func(), error) {
	st, ch, err := openAll(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := st.Migrate(ctx); err != nil {
		return nil, nil, err
	}
	if err := ch.Migrate(ctx); err != nil {
		return nil, nil, err
	}
	c, err := cache.Open(ctx, cfg.RedisURL)
	if err != nil {
		return nil, nil, err
	}
	keys, err := newKeyring(cfg, st)
	if err != nil {
		return nil, nil, err
	}
	if err := keys.Health(ctx); err != nil {
		slog.Warn("vault not ready; signing will fail until it is", "err", err)
	}
	var led ledger.Client = ledger.Offline{}
	if cfg.LedgerEnabled() {
		led = ledger.NewFabric(cfg.FabricDir, cfg.FabricOrg, cfg.FabricPeer, cfg.FabricChannel)
	}
	notify, err := newNotifier(cfg, st, c, keys)
	if err != nil {
		return nil, nil, err
	}
	return &deps{st, ch, c, led, keys, notify}, func() { st.Close(); _ = ch.Close(); _ = c.Close() }, nil
}

func cmdServe(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	withWorker := fs.Bool("worker", false, "also run the background loops in this process")
	_ = fs.Parse(args)
	d, closeAll, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	srv := &api.Server{Cfg: cfg, St: d.st, CH: d.ch, Cache: d.cache, Ledger: d.led, Keyring: d.keys, Notify: d.notify, Version: version, Started: time.Now(),
		Ingest: &ingest.Service{St: d.st, Cache: d.cache, Keys: d.keys}, Passport: &passport.Builder{St: d.st}}
	srv.AnnounceSetup(ctx)
	handler := srv.Handler()
	if cfg.DemoTenant {
		dm := &demo.Manager{Cfg: cfg, St: d.st, CH: d.ch, Cache: d.cache, Keys: d.keys, Alerts: d.notify.DB, Handler: handler}
		go dm.Run(ctx)
	}
	if *withWorker {
		go newWorker(cfg, d).Run(ctx)
	}
	hs := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = hs.Shutdown(shut)
	}()
	slog.Info("listening", "addr", cfg.HTTPAddr, "public", cfg.PublicURL, "ledger", cfg.LedgerEnabled(), "demo", cfg.DemoMode)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func newWorker(cfg config.Config, d *deps) *worker.Worker {
	return &worker.Worker{St: d.st, CH: d.ch, Cache: d.cache, Ledger: d.led, Name: cfg.WorkerName,
		Projector: analytics.NewProjector(d.st, d.ch, passport.NewResolver(d.st, lca.DefaultScenario)),
		SealEvery: cfg.SealInterval, SealMax: cfg.SealMax, AlertsEvery: cfg.AlertsInterval,
		Alerts:  &alerts.Engine{DB: d.notify.DB, N: d.notify, Src: alerts.Sources{St: d.st, CH: d.ch}},
		Reports: &compliance.Mailer{Deps: compliance.Deps{St: d.st, CH: d.ch}, Senders: d.notify.Senders, PublicURL: cfg.PublicURL}}
}

func cmdWorker(ctx context.Context, cfg config.Config, _ []string) error {
	d, closeAll, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	slog.Info("worker started", "name", cfg.WorkerName, "ledger", cfg.LedgerEnabled())
	w := newWorker(cfg, d)
	w.Heartbeat = workerHeartbeat
	w.Run(ctx)
	return nil
}

// cmdSetupLink prints a fresh first-run setup link (when the server's start-up
// log is gone). It does nothing once a platform manager exists.
func cmdSetupLink(ctx context.Context, cfg config.Config, _ []string) error {
	d, closeAll, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	srv := &api.Server{Cfg: cfg, St: d.st, Cache: d.cache, Keyring: d.keys}
	needed, err := d.st.SetupNeeded(ctx)
	if err != nil {
		return err
	}
	if !needed {
		return errors.New("this installation already has a platform manager; sign in instead")
	}
	link, err := srv.NewSetupLink(ctx)
	if err != nil {
		return err
	}
	fmt.Println(link)
	return nil
}

// cmdLedger registers keys and seals everything sealable now, then exits.
func cmdLedger(ctx context.Context, cfg config.Config, _ []string) error {
	if !cfg.LedgerEnabled() {
		return errors.New("FABRIC_DIR is not set")
	}
	d, closeAll, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	start := time.Now()
	if err := newWorker(cfg, d).SealAll(ctx); err != nil {
		return err
	}
	st, err := d.st.SealStatistics(ctx)
	if err != nil {
		return err
	}
	slog.Info("ledger up to date", "sealed", st.Sealed, "seals", st.Seals, "waiting", st.Pending, "blocked", st.Blocked, "took", time.Since(start).Round(time.Second))
	return nil
}
