"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import {
  ActionGroup,
  Button,
  Content,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  Grid,
  GridItem,
  TextArea,
  TextInput,
  Title,
} from "@patternfly/react-core";
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

/** slugify turns a topology name into a Kubernetes-namespace-safe default:
 * lowercase, non-alphanumeric runs collapsed to a single dash, no leading/
 * trailing dash. */
function slugify(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export function TopologyForm({ existing }: { existing?: Topology }) {
  const router = useRouter();
  const clusters = useResource(() => api.clusters(), [], 0);
  const [name, setName] = useState(existing?.metadata.name ?? "");
  const [spec, setSpec] = useState<TopologySpec>(existing?.spec ?? emptySpec());
  const [extraValuesYAML, setExtraValuesYAML] = useState("");
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  // Once the user has touched the namespace field directly, typing in the
  // name field should stop clobbering their choice.
  const [namespaceEdited, setNamespaceEdited] = useState(Boolean(existing));

  const patch = (changes: Partial<TopologySpec>) => setSpec({ ...spec, ...changes });

  // Extra values live in a separate ConfigMap (see /topologies/{name}/extra-
  // values), so a new topology has nothing to load and an existing one is
  // fetched once on mount.
  useEffect(() => {
    if (!existing) {
      return;
    }
    let cancelled = false;
    api
      .getExtraValues(existing.metadata.name)
      .then((res) => {
        if (!cancelled) {
          setExtraValuesYAML(res.valuesYAML);
        }
      })
      .catch(() => {
        // Not fatal to the form - the field just starts blank, same as a new
        // topology, and the error surfaces properly if the user tries to save.
      });
    return () => {
      cancelled = true;
    };
  }, [existing]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      const savedName = existing?.metadata.name ?? name;
      if (existing) {
        await api.updateTopology(existing.metadata.name, { spec });
      } else {
        await api.createTopology({ metadata: { name }, spec });
      }
      await api.setExtraValues(savedName, extraValuesYAML);
      router.push("/topologies");
      router.refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
      setSaving(false);
    }
  }

  return (
    <Form onSubmit={submit}>
      <ErrorBanner error={error} />

      <Title headingLevel="h2">Identity</Title>
      <Grid hasGutter md={6}>
        <GridItem span={12} md={6}>
          <FormGroup label="Name" isRequired fieldId="name">
            <TextInput
              id="name"
              name="name"
              value={name}
              isRequired
              isDisabled={Boolean(existing)}
              pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
              title="A Kubernetes object name: lowercase letters, digits and dashes"
              onChange={(_e, value) => {
                setName(value);
                if (!namespaceEdited) {
                  patch({ namespace: slugify(value) });
                }
              }}
            />
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={6}>
          <FormGroup label="Target name (defaults to the name)" fieldId="targetName">
            <TextInput
              id="targetName"
              name="targetName"
              value={spec.targetName ?? ""}
              placeholder={name}
              onChange={(_e, value) => patch({ targetName: value || undefined })}
            />
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={6}>
          <FormGroup label="Namespace on the target cluster" isRequired fieldId="namespace">
            <TextInput
              id="namespace"
              name="namespace"
              value={spec.namespace}
              isRequired
              onChange={(_e, value) => {
                setNamespaceEdited(true);
                patch({ namespace: value });
              }}
            />
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={6}>
          <FormGroup label="Cluster" fieldId="cluster">
            <FormSelect
              id="cluster"
              name="cluster"
              value={spec.clusterRef?.name ?? ""}
              onChange={(_e, value) => patch({ clusterRef: value ? { name: value } : undefined })}
            >
              <FormSelectOption value="" label="Management cluster" />
              {(clusters.data ?? [])
                .filter((cluster) => !cluster.management)
                .map((cluster) => (
                  <FormSelectOption key={cluster.name} value={cluster.name} label={cluster.name} />
                ))}
            </FormSelect>
          </FormGroup>
        </GridItem>
      </Grid>

      <Title headingLevel="h2">Workloads</Title>
      <Grid hasGutter md={6}>
        <GridItem span={12} md={6}>
          <FormGroup label="Server replicas" fieldId="serverReplicas">
            <TextInput
              id="serverReplicas"
              name="serverReplicas"
              type="number"
              min={1}
              value={spec.server.replicas}
              onChange={(_e, value) => patch({ server: { ...spec.server, replicas: Number(value) } })}
            />
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={6}>
          <FormGroup label="Worker replicas" fieldId="workerReplicas">
            <TextInput
              id="workerReplicas"
              name="workerReplicas"
              type="number"
              min={1}
              value={spec.worker.replicas}
              onChange={(_e, value) => patch({ worker: { ...spec.worker, replicas: Number(value) } })}
            />
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={6}>
          <FormGroup label="authentik image" fieldId="image">
            <TextInput
              id="image"
              name="image"
              value={spec.authentik?.image ?? ""}
              placeholder="ghcr.io/goauthentik/server"
              onChange={(_e, value) => patch({ authentik: { ...spec.authentik, image: value || undefined } })}
            />
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={6}>
          <FormGroup label="authentik image tag (defaults to the chart's)" fieldId="tag">
            <TextInput
              id="tag"
              name="tag"
              value={spec.authentik?.tag ?? ""}
              placeholder="2026.6.0"
              onChange={(_e, value) => patch({ authentik: { ...spec.authentik, tag: value || undefined } })}
            />
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={6}>
          <FormGroup label="Chart version (defaults to latest)" fieldId="chartVersion">
            <TextInput
              id="chartVersion"
              name="chartVersion"
              value={spec.chart?.version ?? ""}
              onChange={(_e, value) => patch({ chart: { ...spec.chart, version: value || undefined } })}
            />
          </FormGroup>
        </GridItem>
      </Grid>

      <Title headingLevel="h2">Placement</Title>
      <FormGroup label="Mode" fieldId="placementMode">
        <FormSelect
          id="placementMode"
          name="placementMode"
          value={spec.placement?.mode ?? "Default"}
          onChange={(_e, value) => patch({ placement: { ...spec.placement, mode: value as PlacementMode } })}
        >
          <FormSelectOption value="Default" label="Default - pods may share nodes" />
          <FormSelectOption value="Dedicated" label="Dedicated - one pod per node" />
          <FormSelectOption value="Packed" label="Packed - all pods on the same node" />
        </FormSelect>
      </FormGroup>
      {spec.placement?.mode === "Dedicated" && (
        <FormGroup label="Anti-affinity topology key" fieldId="topologyKey">
          <TextInput
            id="topologyKey"
            name="topologyKey"
            value={spec.placement?.topologyKey ?? ""}
            placeholder="kubernetes.io/hostname"
            onChange={(_e, value) => patch({ placement: { ...spec.placement, topologyKey: value } })}
          />
        </FormGroup>
      )}

      <Title headingLevel="h2">Postgres</Title>
      <FormGroup label="Mode" fieldId="postgresMode">
        <FormSelect
          id="postgresMode"
          name="postgresMode"
          value={spec.postgres.mode}
          onChange={(_e, value) => {
            const mode = value as PostgresMode;
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
          <FormSelectOption value="Embedded" label="Embedded - the chart's bundled subchart" />
          <FormSelectOption value="CNPG" label="CloudNativePG - HA cluster" />
          <FormSelectOption value="External" label="External - an instance you manage" />
        </FormSelect>
      </FormGroup>

      {spec.postgres.mode === "Embedded" && (
        <FormGroup label="Storage size" fieldId="embeddedStorage">
          <TextInput
            id="embeddedStorage"
            name="embeddedStorage"
            value={spec.postgres.embedded?.storageSize ?? ""}
            placeholder="8Gi"
            onChange={(_e, value) =>
              patch({
                postgres: { ...spec.postgres, embedded: { ...spec.postgres.embedded, storageSize: value || undefined } },
              })
            }
          />
        </FormGroup>
      )}

      {spec.postgres.mode === "CNPG" && (
        <Grid hasGutter md={6}>
          <GridItem span={12} md={6}>
            <FormGroup label="Instances" fieldId="cnpgInstances">
              <TextInput
                id="cnpgInstances"
                name="cnpgInstances"
                type="number"
                min={1}
                value={spec.postgres.cnpg?.instances ?? 3}
                onChange={(_e, value) =>
                  patch({ postgres: { ...spec.postgres, cnpg: { ...spec.postgres.cnpg, instances: Number(value) } } })
                }
              />
            </FormGroup>
          </GridItem>
          <GridItem span={12} md={6}>
            <FormGroup label="Storage size" fieldId="cnpgStorage">
              <TextInput
                id="cnpgStorage"
                name="cnpgStorage"
                value={spec.postgres.cnpg?.storageSize ?? ""}
                placeholder="10Gi"
                onChange={(_e, value) =>
                  patch({
                    postgres: { ...spec.postgres, cnpg: { ...spec.postgres.cnpg, storageSize: value || undefined } },
                  })
                }
              />
            </FormGroup>
          </GridItem>
        </Grid>
      )}

      {spec.postgres.mode === "External" && (
        <Grid hasGutter md={6}>
          <GridItem span={12} md={6}>
            <FormGroup label="Connection secret name" isRequired fieldId="externalSecret">
              <TextInput
                id="externalSecret"
                name="externalSecret"
                isRequired
                value={spec.postgres.external?.secretRef.name ?? ""}
                onChange={(_e, value) =>
                  patch({
                    postgres: { ...spec.postgres, external: { ...spec.postgres.external, secretRef: { name: value } } },
                  })
                }
              />
            </FormGroup>
          </GridItem>
          <GridItem span={12} md={6}>
            <FormGroup label="SSL mode" fieldId="externalSslMode">
              <FormSelect
                id="externalSslMode"
                name="externalSslMode"
                value={spec.postgres.external?.sslMode ?? "require"}
                onChange={(_e, value) =>
                  patch({
                    postgres: {
                      ...spec.postgres,
                      external: { secretRef: spec.postgres.external?.secretRef ?? { name: "" }, sslMode: value },
                    },
                  })
                }
              >
                {["disable", "require", "verify-ca", "verify-full"].map((mode) => (
                  <FormSelectOption key={mode} value={mode} label={mode} />
                ))}
              </FormSelect>
            </FormGroup>
          </GridItem>
        </Grid>
      )}

      <Title headingLevel="h2">Advanced: extra Helm values</Title>
      <Content component="small">
        Raw <code>values.yaml</code> for the authentik chart, merged over everything above
        last - the escape hatch for settings this form doesn&apos;t expose a field for.
        Deep-merged: a key you don&apos;t set here is left alone.
      </Content>
      <FormGroup label="Extra Helm values (YAML)" fieldId="extraValuesYAML">
        <TextArea
          id="extraValuesYAML"
          name="extraValuesYAML"
          resizeOrientation="vertical"
          rows={10}
          spellCheck={false}
          placeholder={"authentik:\n  geoip:\n    enabled: true\n"}
          value={extraValuesYAML}
          onChange={(_e, value) => setExtraValuesYAML(value)}
          style={{ fontFamily: "monospace" }}
        />
      </FormGroup>

      <ActionGroup>
        <Button type="submit" isDisabled={saving}>
          {saving ? "Saving..." : existing ? "Save changes" : "Create topology"}
        </Button>
        <Button variant="link" onClick={() => router.push("/topologies")}>
          Cancel
        </Button>
      </ActionGroup>
    </Form>
  );
}
