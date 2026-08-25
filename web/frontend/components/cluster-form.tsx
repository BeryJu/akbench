"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { WorkloadClusterSpec } from "@/lib/types";
import { ErrorBanner } from "@/components/ui";

// KUBERNETES_VERSIONS is the latest patch of each of the last 5 minor
// releases, newest first. Static rather than fetched live: a cluster-
// creation form that depends on reaching an external version index at
// request time is a worse failure mode than an occasionally-stale list, and
// every other version pin in this repo (K6Image, chart versions) is
// deliberately static for the same reason. Sourced from dl.k8s.io/release/
// stable[-<minor>].txt - refresh these (and emptySpec's default below) when
// they drift too far behind.
const KUBERNETES_VERSIONS = ["v1.36.4", "v1.35.8", "v1.34.11", "v1.33.13", "v1.32.13"];

// INSTANCE_FLAVORS are cluster-api-provider-incus's own "cX-mY" flavor
// format (X vCPUs, Y GiB RAM - see LXCMachineSpec.Flavor's doc comment
// upstream), not a made-up scheme of this project's - so the dropdown's
// labels can be computed directly from the value instead of hand-maintained
// alongside it.
const INSTANCE_FLAVORS = ["c1-m2", "c2-m4", "c4-m8", "c8-m16", "c16-m32"];

function flavorLabel(flavor: string): string {
  const match = /^c(\d+)-m(\d+)$/.exec(flavor);
  if (!match) {
    return flavor;
  }
  const [, cpu, memGiB] = match;
  return `${flavor} - ${cpu} vCPU, ${memGiB} GiB RAM`;
}

/**
 * emptySpec mirrors WorkloadClusterSpec's own CRD defaults, the same way
 * topology-form's emptySpec does - what you see is what gets created.
 */
function emptySpec(): WorkloadClusterSpec {
  return {
    kubernetesVersion: KUBERNETES_VERSIONS[0],
    provider: "Incus",
    controlPlane: { replicas: 1 },
    workers: { replicas: 2 },
    incus: {
      lxcSecretRef: { name: "lxc-secret" },
      image: "ubuntu:24.04",
      profiles: ["default"],
      controlPlane: { flavor: "c2-m4" },
      workers: { flavor: "c2-m4" },
    },
  };
}

/**
 * ClusterForm registers a new WorkloadCluster - there is no edit path: once
 * provisioned, WorkloadClusterReconciler never updates the Cluster API
 * objects it created (see applyCreateOnly), so resizing or reconfiguring
 * means deprovisioning and registering a replacement.
 */
export function ClusterForm() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [spec, setSpec] = useState<WorkloadClusterSpec>(emptySpec());
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const patch = (changes: Partial<WorkloadClusterSpec>) => setSpec({ ...spec, ...changes });
  const patchIncus = (changes: Partial<NonNullable<WorkloadClusterSpec["incus"]>>) =>
    setSpec({ ...spec, incus: { ...spec.incus!, ...changes } });

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      await api.registerCluster({ metadata: { name }, spec });
      router.push("/clusters");
      router.refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
      setSaving(false);
    }
  }

  return (
    <form onSubmit={submit}>
      <ErrorBanner error={error} />

      <fieldset>
        <legend>Identity</legend>
        <div className="grid2">
          <label>
            <span>Name</span>
            <input
              name="name"
              value={name}
              required
              pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
              title="A Kubernetes object name: lowercase letters, digits and dashes"
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label>
            <span>Kubernetes version</span>
            <select
              name="kubernetesVersion"
              value={spec.kubernetesVersion}
              onChange={(e) => patch({ kubernetesVersion: e.target.value })}
            >
              {KUBERNETES_VERSIONS.map((version, i) => (
                <option key={version} value={version}>
                  {version}
                  {i === 0 ? " (latest)" : ""}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>Infrastructure provider</span>
            <select
              name="provider"
              value={spec.provider}
              onChange={(e) => patch({ provider: e.target.value as WorkloadClusterSpec["provider"] })}
            >
              <option value="Incus">Incus / LXC</option>
            </select>
          </label>
        </div>
      </fieldset>

      <fieldset>
        <legend>Machines</legend>
        <div className="grid2">
          <label>
            <span>Control plane replicas</span>
            <input
              name="controlPlaneReplicas"
              type="number"
              min={1}
              value={spec.controlPlane.replicas}
              onChange={(e) =>
                patch({ controlPlane: { replicas: Number(e.target.value) } })
              }
            />
          </label>
          <label>
            <span>Worker replicas</span>
            <input
              name="workerReplicas"
              type="number"
              min={1}
              value={spec.workers.replicas}
              onChange={(e) => patch({ workers: { replicas: Number(e.target.value) } })}
            />
          </label>
        </div>
      </fieldset>

      {spec.provider === "Incus" && (
        <fieldset>
          <legend>Incus / LXC</legend>
          <div className="grid2">
            <label>
              <span>LXC daemon connection secret</span>
              <input
                name="lxcSecretRef"
                required
                value={spec.incus?.lxcSecretRef.name ?? ""}
                placeholder="lxc-secret"
                onChange={(e) => patchIncus({ lxcSecretRef: { name: e.target.value } })}
              />
            </label>
            <label>
              <span>Image</span>
              <input
                name="image"
                value={spec.incus?.image ?? ""}
                placeholder="ubuntu:24.04"
                onChange={(e) => patchIncus({ image: e.target.value })}
              />
            </label>
            <label>
              <span>Profiles (comma-separated)</span>
              <input
                name="profiles"
                value={(spec.incus?.profiles ?? []).join(",")}
                placeholder="default"
                onChange={(e) =>
                  patchIncus({
                    profiles: e.target.value
                      .split(",")
                      .map((p) => p.trim())
                      .filter(Boolean),
                  })
                }
              />
            </label>
          </div>
          <div className="grid2">
            <label>
              <span>Control plane flavor</span>
              <select
                name="controlPlaneFlavor"
                value={spec.incus?.controlPlane?.flavor ?? INSTANCE_FLAVORS[1]}
                onChange={(e) =>
                  patchIncus({ controlPlane: { flavor: e.target.value } })
                }
              >
                {INSTANCE_FLAVORS.map((flavor) => (
                  <option key={flavor} value={flavor}>
                    {flavorLabel(flavor)}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>Worker flavor</span>
              <select
                name="workerFlavor"
                value={spec.incus?.workers?.flavor ?? INSTANCE_FLAVORS[1]}
                onChange={(e) => patchIncus({ workers: { flavor: e.target.value } })}
              >
                {INSTANCE_FLAVORS.map((flavor) => (
                  <option key={flavor} value={flavor}>
                    {flavorLabel(flavor)}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </fieldset>
      )}

      <div className="row">
        <button type="submit" className="primary" disabled={saving}>
          {saving ? "Provisioning..." : "Register cluster"}
        </button>
        <button type="button" onClick={() => router.push("/clusters")}>
          Cancel
        </button>
      </div>
    </form>
  );
}
