package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sensifai/roadpass/internal/config"
	"github.com/sensifai/roadpass/internal/worker"
)

// workerHeartbeat is the file `roadpass worker` touches after every loop step.
var workerHeartbeat = filepath.Join(os.TempDir(), "roadpass-worker.alive")

// cmdHealth probes the local server, for images without a shell or curl. With
// -worker it checks the worker's heartbeat file instead (the worker serves no HTTP).
func cmdHealth(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	asWorker := fs.Bool("worker", false, "check the worker heartbeat instead of /healthz")
	_ = fs.Parse(args)
	if *asWorker {
		fi, err := os.Stat(workerHeartbeat)
		if err != nil {
			return err
		}
		if age := time.Since(fi.ModTime()); age > worker.HeartbeatMaxAge {
			return fmt.Errorf("worker heartbeat is %s old", age.Round(time.Second))
		}
		return nil
	}
	addr := cfg.HTTPAddr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: %s", res.Status)
	}
	return nil
}
