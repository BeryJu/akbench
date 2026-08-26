"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
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
  TextInput,
  Title,
} from "@patternfly/react-core";
import { TrashIcon } from "@patternfly/react-icons";
import { api, ApiError } from "@/lib/api";
import type { WorkerPoolSpec, WorkloadCluster, WorkloadClusterSpec } from "@/lib/types";
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

function emptyPool(name: string): WorkerPoolSpec {
  return { name, replicas: 2, incus: { flavor: "c2-m4" } };
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
    workerPools: [emptyPool("workers")],
    incus: {
      lxcSecretRef: { name: "lxc-secret" },
      image: "ubuntu:24.04",
      profiles: ["default"],
      controlPlane: { flavor: "c2-m4" },
    },
  };
}

/**
 * ClusterForm registers a new WorkloadCluster, or - given `existing` - edits
 * one already provisioned. Editing is deliberately narrow: only worker pool
 * replicas/flavor (and adding/removing whole pools) actually do anything on
 * an existing cluster, since WorkloadClusterReconciler only supports day-2
 * changes to worker pools (see WorkerPoolSpec's doc comment) - the control
 * plane and every Incus/LXC-wide setting (image, profiles, secret) is
 * provisioned once and left alone after that, so those fields are read-only
 * once a cluster exists rather than silently accepting an edit that would
 * never take effect.
 */
export function ClusterForm({ existing }: { existing?: WorkloadCluster }) {
  const router = useRouter();
  const [name, setName] = useState(existing?.metadata.name ?? "");
  const [spec, setSpec] = useState<WorkloadClusterSpec>(existing?.spec ?? emptySpec());
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const patch = (changes: Partial<WorkloadClusterSpec>) => setSpec({ ...spec, ...changes });
  const patchIncus = (changes: Partial<NonNullable<WorkloadClusterSpec["incus"]>>) =>
    setSpec({ ...spec, incus: { ...spec.incus!, ...changes } });

  function patchPool(index: number, changes: Partial<WorkerPoolSpec>) {
    const pools = spec.workerPools.slice();
    pools[index] = { ...pools[index], ...changes };
    patch({ workerPools: pools });
  }
  function addPool() {
    const name = `pool-${spec.workerPools.length + 1}`;
    patch({ workerPools: [...spec.workerPools, emptyPool(name)] });
  }
  function removePool(index: number) {
    patch({ workerPools: spec.workerPools.filter((_, i) => i !== index) });
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      if (existing) {
        await api.updateWorkloadCluster(existing.metadata.name, { spec });
      } else {
        await api.registerCluster({ metadata: { name }, spec });
      }
      router.push("/clusters");
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
      <Grid hasGutter md={4}>
        <GridItem span={12} md={4}>
          <FormGroup label="Name" isRequired fieldId="name">
            <TextInput
              id="name"
              name="name"
              value={name}
              isRequired
              isDisabled={Boolean(existing)}
              pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
              title="A Kubernetes object name: lowercase letters, digits and dashes"
              onChange={(_e, value) => setName(value)}
            />
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={4}>
          <FormGroup label="Kubernetes version" fieldId="kubernetesVersion">
            <FormSelect
              id="kubernetesVersion"
              name="kubernetesVersion"
              isDisabled={Boolean(existing)}
              value={spec.kubernetesVersion}
              onChange={(_e, value) => patch({ kubernetesVersion: value })}
            >
              {(existing && !KUBERNETES_VERSIONS.includes(spec.kubernetesVersion)
                ? [spec.kubernetesVersion, ...KUBERNETES_VERSIONS]
                : KUBERNETES_VERSIONS
              ).map((version, i) => (
                <FormSelectOption key={version} value={version} label={`${version}${i === 0 && !existing ? " (latest)" : ""}`} />
              ))}
            </FormSelect>
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={4}>
          <FormGroup label="Infrastructure provider" fieldId="provider">
            <FormSelect
              id="provider"
              name="provider"
              isDisabled={Boolean(existing)}
              value={spec.provider}
              onChange={(_e, value) => patch({ provider: value as WorkloadClusterSpec["provider"] })}
            >
              <FormSelectOption value="Incus" label="Incus / LXC" />
            </FormSelect>
          </FormGroup>
        </GridItem>
      </Grid>

      <Title headingLevel="h2">Control plane</Title>
      {existing && (
        <Content component="small">
          Not resizable/reconfigurable after creation - deprovision and register a
          replacement to change it.
        </Content>
      )}
      <FormGroup label="Control plane replicas" fieldId="controlPlaneReplicas">
        <TextInput
          id="controlPlaneReplicas"
          name="controlPlaneReplicas"
          type="number"
          min={1}
          isDisabled={Boolean(existing)}
          value={spec.controlPlane.replicas}
          onChange={(_e, value) => patch({ controlPlane: { replicas: Number(value) } })}
        />
      </FormGroup>

      <Title headingLevel="h2">Worker pools</Title>
      {spec.workerPools.map((pool, i) => (
        <Grid hasGutter md={4} key={i} style={{ marginBottom: "1rem", alignItems: "flex-end" }}>
          <GridItem span={12} md={4}>
            <FormGroup label="Pool name" isRequired fieldId={`pool-${i}-name`}>
              <TextInput
                id={`pool-${i}-name`}
                value={pool.name}
                isRequired
                pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
                title="Lowercase letters, digits and dashes"
                onChange={(_e, value) => patchPool(i, { name: value })}
              />
            </FormGroup>
          </GridItem>
          <GridItem span={12} md={3}>
            <FormGroup label="Replicas" fieldId={`pool-${i}-replicas`}>
              <TextInput
                id={`pool-${i}-replicas`}
                type="number"
                min={1}
                value={pool.replicas}
                onChange={(_e, value) => patchPool(i, { replicas: Number(value) })}
              />
            </FormGroup>
          </GridItem>
          <GridItem span={12} md={4}>
            <FormGroup label="Flavor" fieldId={`pool-${i}-flavor`}>
              <FormSelect
                id={`pool-${i}-flavor`}
                value={pool.incus?.flavor ?? INSTANCE_FLAVORS[1]}
                onChange={(_e, value) => patchPool(i, { incus: { ...pool.incus, flavor: value } })}
              >
                {INSTANCE_FLAVORS.map((flavor) => (
                  <FormSelectOption key={flavor} value={flavor} label={flavorLabel(flavor)} />
                ))}
              </FormSelect>
            </FormGroup>
          </GridItem>
          <GridItem span={12} md={1}>
            <Button
              variant="plain"
              aria-label={`Remove pool ${pool.name}`}
              isDisabled={spec.workerPools.length <= 1}
              onClick={() => removePool(i)}
            >
              <TrashIcon />
            </Button>
          </GridItem>
        </Grid>
      ))}
      <Button variant="secondary" onClick={addPool}>
        Add worker pool
      </Button>

      {spec.provider === "Incus" && (
        <>
          <Title headingLevel="h2">Incus / LXC</Title>
          {existing && (
            <Content component="small">
              Applies to new/edited worker pools only - the control plane and
              already-provisioned pools keep what they were created with.
            </Content>
          )}
          <Grid hasGutter md={4}>
            <GridItem span={12} md={4}>
              <FormGroup label="LXC daemon connection secret" isRequired fieldId="lxcSecretRef">
                <TextInput
                  id="lxcSecretRef"
                  name="lxcSecretRef"
                  isRequired
                  isDisabled={Boolean(existing)}
                  value={spec.incus?.lxcSecretRef.name ?? ""}
                  placeholder="lxc-secret"
                  onChange={(_e, value) => patchIncus({ lxcSecretRef: { name: value } })}
                />
              </FormGroup>
            </GridItem>
            <GridItem span={12} md={4}>
              <FormGroup label="Image" fieldId="image">
                <TextInput
                  id="image"
                  name="image"
                  value={spec.incus?.image ?? ""}
                  placeholder="ubuntu:24.04"
                  onChange={(_e, value) => patchIncus({ image: value })}
                />
              </FormGroup>
            </GridItem>
            <GridItem span={12} md={4}>
              <FormGroup label="Profiles (comma-separated)" fieldId="profiles">
                <TextInput
                  id="profiles"
                  name="profiles"
                  value={(spec.incus?.profiles ?? []).join(",")}
                  placeholder="default"
                  onChange={(_e, value) =>
                    patchIncus({
                      profiles: value
                        .split(",")
                        .map((p) => p.trim())
                        .filter(Boolean),
                    })
                  }
                />
              </FormGroup>
            </GridItem>
          </Grid>
          <FormGroup label="Control plane flavor" fieldId="controlPlaneFlavor">
            <FormSelect
              id="controlPlaneFlavor"
              name="controlPlaneFlavor"
              isDisabled={Boolean(existing)}
              value={spec.incus?.controlPlane?.flavor ?? INSTANCE_FLAVORS[1]}
              onChange={(_e, value) => patchIncus({ controlPlane: { flavor: value } })}
            >
              {INSTANCE_FLAVORS.map((flavor) => (
                <FormSelectOption key={flavor} value={flavor} label={flavorLabel(flavor)} />
              ))}
            </FormSelect>
          </FormGroup>
        </>
      )}

      <ActionGroup>
        <Button type="submit" isDisabled={saving}>
          {saving ? "Saving..." : existing ? "Save changes" : "Register cluster"}
        </Button>
        <Button variant="link" onClick={() => router.push("/clusters")}>
          Cancel
        </Button>
      </ActionGroup>
    </Form>
  );
}
