// cmd/appliance: the bootstrap side of the single-container appliance
// (Dockerfile.appliance). The container's entrypoint starts `bootstrap` in
// the background and then execs k3s as PID 1; bootstrap waits for k3s's API
// and installs Flux, the Cluster API providers and akbench on top. `token`
// and `incus-cert` are for `docker exec`.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	case "incus-cert":
		fmt.Print(st.IncusClientCert)
		return nil
	case "bootstrap":
	default:
		return fmt.Errorf("unknown command %q (want bootstrap, token or incus-cert)", cmd)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	b := &appliance.Bootstrapper{Config: cfg, State: st, Run: appliance.ExecRunner(cfg.Kubeconfig), Log: os.Stdout}
	b.PrintIncusInstructions()
	if err := b.RunUntilDone(ctx); err != nil {
		return err
	}
	b.PrintBanner()
	// Stay alive rather than exit: k3s is PID 1 and this process's parent,
	// and isn't guaranteed to reap an exited child.
	<-ctx.Done()
	return nil
}
