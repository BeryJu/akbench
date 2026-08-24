"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { PlacementMode, PostgresMode, Topology, TopologySpec } from "@/lib/types";
import { ErrorBanner, useResource } from "@/components/ui";

/**
 * emptySpec mirrors the CRD's own defaults. Restating them here is what makes
 * the form show what will actually be created rather than a set of blanks the
 * API server fills in invisibly.
 */
function emptySpec(): TopologySpec {
  return {
    namespace: "",
    server: { replicas: 1 },
    worker: { replicas: 1 },
    placement: { mode: "Default", topologyKey: "kubernetes.io/hostname" },
    postgres: { mode: "Embedded", embedded: {} },
    chart: { repo: "https://charts.goauthentik.io" },
    authentik: { image: "ghcr.io/goauthentik/server" },
  };
}

export function TopologyForm({ existing }: { existing?: Topology }) {
  const router = useRouter();
  const clusters = useResource(() => api.clusters(), [], 0);
  const [name, setName] = useState(existing?.metadata.name ?? "");
  const [spec, setSpec] = useState<TopologySpec>(existing?.spec ?? emptySpec());
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const patch = (changes: Partial<TopologySpec>) => setSpec({ ...spec, ...changes });

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      if (existing) {
        await api.updateTopology(existing.metadata.name, { spec });
      } else {
        await api.createTopology({ metadata: { name }, spec });
      }
      router.push("/topologies");
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
              disabled={Boolean(existing)}
              pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
              title="A Kubernetes object name: lowercase letters, digits and dashes"
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label>
            <span>Target name (defaults to the name)</span>
            <input
              name="targetName"
              value={spec.targetName ?? ""}
              placeholder={name}
              onChange={(e) => patch({ targetName: e.target.value || undefined })}
            />
          </label>
          <label>
            <span>Namespace on the target cluster</span>
            <input
              name="namespace"
              value={spec.namespace}
              required
              onChange={(e) => patch({ namespace: e.target.value })}
            />
          </label>
          <label>
            <span>Cluster</span>
            <select
              name="cluster"
              value={spec.clusterRef?.name ?? ""}
              onChange={(e) =>
                patch({ clusterRef: e.target.value ? { name: e.target.value } : undefined })
              }
            >
              <option value="">Management cluster</option>
              {(clusters.data ?? [])
                .filter((cluster) => !cluster.management)
                .map((cluster) => (
                  <option key={cluster.name} value={cluster.name}>
                    {cluster.name}
                  </option>
                ))}
            </select>
          </label>
        </div>
      </fieldset>

      <fieldset>
        <legend>Workloads</legend>
        <div className="grid2">
          <label>
            <span>Server replicas</span>
            <input
              name="serverReplicas"
              type="number"
              min={1}
              value={spec.server.replicas}
              onChange={(e) => patch({ server: { ...spec.server, replicas: Number(e.target.value) } })}
            />
          </label>
          <label>
            <span>Worker replicas</span>
            <input
              name="workerReplicas"
              type="number"
              min={1}
              value={spec.worker.replicas}
              onChange={(e) => patch({ worker: { ...spec.worker, replicas: Number(e.target.value) } })}
            />
          </label>
          <label>
            <span>authentik image tag (defaults to the chart&apos;s)</span>
            <input
              name="tag"
              value={spec.authentik?.tag ?? ""}
              placeholder="2026.6.0"
              onChange={(e) =>
                patch({ authentik: { ...spec.authentik, tag: e.target.value || undefined } })
              }
            />
          </label>
          <label>
            <span>Chart version (defaults to latest)</span>
            <input
              name="chartVersion"
              value={spec.chart?.version ?? ""}
              onChange={(e) =>
                patch({ chart: { ...spec.chart, version: e.target.value || undefined } })
              }
            />
          </label>
        </div>
      </fieldset>

      <fieldset>
        <legend>Placement</legend>
        <label>
          <span>Mode</span>
          <select
            name="placementMode"
            value={spec.placement?.mode ?? "Default"}
            onChange={(e) =>
              patch({ placement: { ...spec.placement, mode: e.target.value as PlacementMode } })
            }
          >
            <option value="Default">Default - pods may share nodes</option>
            <option value="Dedicated">Dedicated - one pod per node</option>
          </select>
        </label>
        {spec.placement?.mode === "Dedicated" && (
          <label>
            <span>Anti-affinity topology key</span>
            <input
              name="topologyKey"
              value={spec.placement?.topologyKey ?? ""}
              placeholder="kubernetes.io/hostname"
              onChange={(e) => patch({ placement: { ...spec.placement, topologyKey: e.target.value } })}
            />
          </label>
        )}
      </fieldset>

      <fieldset>
        <legend>Postgres</legend>
        <label>
          <span>Mode</span>
          <select
            name="postgresMode"
            value={spec.postgres.mode}
            onChange={(e) => {
              const mode = e.target.value as PostgresMode;
              patch({
                postgres: {
                  mode,
                  embedded: mode === "Embedded" ? (spec.postgres.embedded ?? {}) : undefined,
                  cnpg: mode === "CNPG" ? (spec.postgres.cnpg ?? { instances: 3 }) : undefined,
                  external:
                    mode === "External"
                      ? (spec.postgres.external ?? { secretRef: { name: "" }, sslMode: "require" })
                      : undefined,
                },
              });
            }}
          >
            <option value="Embedded">Embedded - the chart&apos;s bundled subchart</option>
            <option value="CNPG">CloudNativePG - HA cluster</option>
            <option value="External">External - an instance you manage</option>
          </select>
        </label>

        {spec.postgres.mode === "Embedded" && (
          <label>
            <span>Storage size</span>
            <input
              name="embeddedStorage"
              value={spec.postgres.embedded?.storageSize ?? ""}
              placeholder="8Gi"
              onChange={(e) =>
                patch({
                  postgres: {
                    ...spec.postgres,
                    embedded: { ...spec.postgres.embedded, storageSize: e.target.value || undefined },
                  },
                })
              }
            />
          </label>
        )}

        {spec.postgres.mode === "CNPG" && (
          <div className="grid2">
            <label>
              <span>Instances</span>
              <input
                name="cnpgInstances"
                type="number"
                min={1}
                value={spec.postgres.cnpg?.instances ?? 3}
                onChange={(e) =>
                  patch({
                    postgres: {
                      ...spec.postgres,
                      cnpg: { ...spec.postgres.cnpg, instances: Number(e.target.value) },
                    },
                  })
                }
              />
            </label>
            <label>
              <span>Storage size</span>
              <input
                name="cnpgStorage"
                value={spec.postgres.cnpg?.storageSize ?? ""}
                placeholder="10Gi"
                onChange={(e) =>
                  patch({
                    postgres: {
                      ...spec.postgres,
                      cnpg: { ...spec.postgres.cnpg, storageSize: e.target.value || undefined },
                    },
                  })
                }
              />
            </label>
          </div>
        )}

        {spec.postgres.mode === "External" && (
          <div className="grid2">
            <label>
              <span>Connection secret name</span>
              <input
                name="externalSecret"
                required
                value={spec.postgres.external?.secretRef.name ?? ""}
                onChange={(e) =>
                  patch({
                    postgres: {
                      ...spec.postgres,
                      external: {
                        ...spec.postgres.external,
                        secretRef: { name: e.target.value },
                      },
                    },
                  })
                }
              />
            </label>
            <label>
              <span>SSL mode</span>
              <select
                name="externalSslMode"
                value={spec.postgres.external?.sslMode ?? "require"}
                onChange={(e) =>
                  patch({
                    postgres: {
                      ...spec.postgres,
                      external: {
                        secretRef: spec.postgres.external?.secretRef ?? { name: "" },
                        sslMode: e.target.value,
                      },
                    },
                  })
                }
              >
                {["disable", "require", "verify-ca", "verify-full"].map((mode) => (
                  <option key={mode} value={mode}>
                    {mode}
                  </option>
                ))}
              </select>
            </label>
          </div>
        )}
      </fieldset>

      <div className="row">
        <button type="submit" className="primary" disabled={saving}>
          {saving ? "Saving..." : existing ? "Save changes" : "Create topology"}
        </button>
        <button type="button" onClick={() => router.push("/topologies")}>
          Cancel
        </button>
      </div>
    </form>
  );
}
