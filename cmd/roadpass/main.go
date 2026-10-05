// Command roadpass is the ROADPASS service and its operational tools.
//
//	roadpass serve      HTTP: dashboard, /api/v1, /ngsi-ld/v1
//	roadpass worker     background loops: ingest, checks, projector, sealer
//	roadpass migrate    apply Postgres and ClickHouse schemas
//	roadpass seed       load the simulated Spanish dataset (development, demos)
//	roadpass project    project Postgres records into ClickHouse (--rebuild)
//	roadpass ledger     register signing keys and seal records on Fabric once
//	roadpass circuloos  export sealed passports to the CIRCULOOS Data Platform (NGSI-LD) and verify the read-back
//	roadpass setup-link print a first-run setup link (fresh installation only)
//	roadpass health     exit 0 when the local server answers /healthz (container health check)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata" // IANA zones for CSV times, also on distroless

	"github.com/sensifai/roadpass/internal/config"
)

// version is set with -ldflags "-X main.version=…"; SOURCE_COMMIT is the fallback.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	setupLog(cfg)
	if v := os.Getenv("SOURCE_COMMIT"); version == "dev" && v != "" {
		version = v
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmds := map[string]func(context.Context, config.Config, []string) error{
		"serve":      cmdServe,
		"worker":     cmdWorker,
		"migrate":    cmdMigrate,
		"seed":       cmdSeed,
		"project":    cmdProject,
		"ledger":     cmdLedger,
		"health":     cmdHealth,
		"setup-link": cmdSetupLink,
		"circuloos":  cmdCirculoos,
	}
	fn, ok := cmds[os.Args[1]]
	if !ok {
		usage()
		os.Exit(2)
	}
	if err := fn(ctx, cfg, os.Args[2:]); err != nil {
		slog.Error(os.Args[1]+" failed", "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: roadpass <serve|worker|migrate|seed|project|ledger|circuloos|setup-link|health> [flags]`)
}

func setupLog(cfg config.Config) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var h slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if cfg.LogFormat == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h).With("service", "roadpass", "version", version))
}
