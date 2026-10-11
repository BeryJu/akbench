/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package appliance

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const fieldManager = "akbench-appliance"

// Runner executes an external command (kubectl/clusterctl), feeding it stdin
// when non-nil, and returns its combined output. Swapped out in tests.
type Runner func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)

// ExecRunner runs commands for real, with KUBECONFIG pointed at k3s's admin
// kubeconfig and clusterctl's GitHub version check disabled (every provider
// it installs comes from the image's local repository).
func ExecRunner(kubeconfig string) Runner {
	return func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig, "CLUSTERCTL_DISABLE_VERSIONCHECK=true")
		if stdin != nil {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return out, nil
	}
}

// Bootstrapper installs everything on top of k3s. Every step is idempotent,
// so the whole sequence runs again on each container start - which is also
// how an image upgrade rolls out (newer pinned providers, newer chart).
type Bootstrapper struct {
	Config *Config
	State  *State
	Run    Runner
	Log    io.Writer
}

func (b *Bootstrapper) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(b.Log, "[akbench] "+format+"\n", args...)
}

// RunUntilDone retries the bootstrap sequence until it succeeds or ctx ends.
func (b *Bootstrapper) RunUntilDone(ctx context.Context) error {
	for attempt := 1; ; attempt++ {
		err := b.RunOnce(ctx)
		if err == nil {
			return nil
		}
		b.logf("Bootstrap attempt %d failed, retrying in 15s: %v", attempt, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
}

// RunOnce performs one pass of the bootstrap sequence.
func (b *Bootstrapper) RunOnce(ctx context.Context) error {
	if err := b.waitForAPI(ctx); err != nil {
		return err
	}

	b.logf("Applying Flux")
	if _, err := b.Run(ctx, nil, "kubectl", "apply", "--server-side", "--force-conflicts",
		"--field-manager="+fieldManager, "-f", filepath.Join(b.Config.AssetsDir, "flux", "install.yaml")); err != nil {
		return err
	}
	if _, err := b.Run(ctx, nil, "kubectl", "wait", "--for=condition=Established", "--timeout=120s",
		"crd/helmreleases.helm.toolkit.fluxcd.io", "crd/ocirepositories.source.toolkit.fluxcd.io"); err != nil {
		return err
	}

	if err := b.ensureClusterAPI(ctx); err != nil {
		return err
	}

	b.logf("Applying akbench %s", b.Config.Version)
	stack, err := RenderStack(b.Config, b.State)
	if err != nil {
		return fmt.Errorf("render stack: %w", err)
	}
	if _, err := b.Run(ctx, stack, "kubectl", "apply", "--server-side", "--force-conflicts",
		"--field-manager="+fieldManager, "-f", "-"); err != nil {
		return err
	}

	b.logf("Waiting for the web UI to roll out")
	if _, err := b.Run(ctx, nil, "kubectl", "-n", "authentik-bench", "rollout", "status",
		"deployment/apiserver", "--timeout=10m"); err != nil {
		return err
	}
	return nil
}

func (b *Bootstrapper) waitForAPI(ctx context.Context) error {
	b.logf("Waiting for the k3s API server")
	for {
		if _, err := os.Stat(b.Config.Kubeconfig); err == nil {
			if _, err := b.Run(ctx, nil, "kubectl", "get", "--raw=/readyz"); err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (b *Bootstrapper) ensureClusterAPI(ctx context.Context) error {
	var installed []Provider
	crd, err := b.Run(ctx, nil, "kubectl", "get", "crd", "providers.clusterctl.cluster.x-k8s.io", "--ignore-not-found", "-o", "name")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(crd)) != "" {
		raw, err := b.Run(ctx, nil, "kubectl", "get", "providers.clusterctl.cluster.x-k8s.io", "-A", "-o", "json")
		if err != nil {
			return err
		}
		if installed, err = ParseInventory(raw); err != nil {
			return fmt.Errorf("parse clusterctl inventory: %w", err)
		}
	}
	cmds, skipped, err := PlanClusterctl(installed, PinnedProviders(b.Config))
	if err != nil {
		return err
	}
	for _, s := range skipped {
		b.logf("Not downgrading Cluster API provider: %s", s)
	}
	if len(cmds) == 0 {
		b.logf("Cluster API providers are up to date")
		return nil
	}
	global := []string{"--config", filepath.Join(b.Config.AssetsDir, "clusterctl", "clusterctl.yaml"), "--kubeconfig", b.Config.Kubeconfig}
	for _, args := range cmds {
		b.logf("Running clusterctl %s", strings.Join(args, " "))
		if _, err := b.Run(ctx, nil, "clusterctl", append(args, global...)...); err != nil {
			return err
		}
	}
	return nil
}

// PrintIncusInstructions tells the user how to trust a generated client
// certificate on their Incus server - the one manual step the appliance
// can't do for them.
func (b *Bootstrapper) PrintIncusInstructions() {
	if !b.State.IncusCertGenerated {
		return
	}
	fp, _ := CertFingerprint(b.State.IncusClientCert)
	b.logf("Incus client certificate (fingerprint %s) - trust it on your Incus server with:", fp)
	b.logf("  docker exec <container> akbench-appliance incus-cert > akbench.crt")
	b.logf("  incus config trust add-certificate akbench.crt --name akbench")
	if b.Config.Incus.Server == "" {
		b.logf("INCUS_SERVER is not set: the %s/%s Secret is not managed by the appliance", b.Config.Namespace, b.Config.Incus.SecretName)
	}
}

// PrintBanner reports where the UI is and how to log in.
func (b *Bootstrapper) PrintBanner() {
	b.logf("akbench %s is ready", b.Config.Version)
	b.logf("  Web UI:    http://<host>:%s/", b.Config.UIPort)
	b.logf("  API token: docker exec <container> akbench-appliance token")
	b.logf("  kubectl:   docker exec -it <container> kubectl get workloadclusters -A")
}
