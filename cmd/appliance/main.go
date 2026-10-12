// cmd/appliance: the bootstrap side of the single-container appliance
// (Dockerfile.appliance). The container's entrypoint starts `bootstrap` in
// the background and then execs k3s as PID 1; bootstrap waits for k3s's API,
// installs Flux, Cluster API and akbench on top, and then keeps installing
// whichever infrastructure providers get enabled in the web UI. `token` is
// for `docker exec`.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/goauthentik/authentik-bench/internal/appliance"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "akbench-appliance:", err)
		os.Exit(1)
	}
}

func run() error {
	cmd := "bootstrap"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	cfg, err := appliance.LoadConfig(os.Getenv)
	if err != nil {
		return err
	}
	st, err := appliance.EnsureState(cfg)
	if err != nil {
		return err
	}

	switch cmd {
	case "token":
		fmt.Println(st.APIToken)
		return nil
	case "bootstrap":
	default:
		return fmt.Errorf("unknown command %q (want bootstrap or token)", cmd)
	}

	shipped, err := appliance.DiscoverShipped(filepath.Join(cfg.AssetsDir, "clusterctl", "repo"))
	if err != nil {
		return fmt.Errorf("read the providers this image ships: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	b := &appliance.Bootstrapper{
		Config: cfg, State: st, Shipped: shipped,
		Run: appliance.ExecRunner(cfg.Kubeconfig), Log: os.Stdout,
		SyncInterval: 15 * time.Second,
	}
	// Serve never returns before ctx ends - which also keeps this process
	// alive, as it should be: k3s is PID 1 and its parent, and isn't
	// guaranteed to reap an exited child.
	if err := b.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
