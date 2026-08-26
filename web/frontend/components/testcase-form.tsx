"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import {
  ActionGroup,
  Button,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  Grid,
  GridItem,
  TextInput,
  Title,
} from "@patternfly/react-core";
import { api, ApiError } from "@/lib/api";
import type { TestCase, TestCaseSpec, TestProfile } from "@/lib/types";
import { ErrorBanner, useResource } from "@/components/ui";

function emptySpec(): TestCaseSpec {
  return {
    topologyRef: { name: "" },
    script: "login.ts",
    profile: "Quick",
    vus: 8,
    iterations: 1000,
    flow: "default-authentication-flow",
    quickDuration: "150s",
    burnDuration: "8760h",
    traceSampleRate: "0.01",
  };
}

export function TestCaseForm({ existing }: { existing?: TestCase }) {
  const router = useRouter();
  const topologies = useResource(() => api.topologies(), [], 0);
  const [name, setName] = useState(existing?.metadata.name ?? "");
  const [spec, setSpec] = useState<TestCaseSpec>(existing?.spec ?? emptySpec());
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const patch = (changes: Partial<TestCaseSpec>) => setSpec({ ...spec, ...changes });

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      if (existing) {
        await api.updateTestCase(existing.metadata.name, { spec });
      } else {
        await api.createTestCase({ metadata: { name }, spec });
      }
      router.push("/testcases");
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
          <FormGroup label="Name - this is the k6 testid every dashboard splits on" isRequired fieldId="name">
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
        <GridItem span={12} md={6}>
          <FormGroup label="Topology to drive" isRequired fieldId="topology">
            <FormSelect
              id="topology"
              name="topology"
              isRequired
              value={spec.topologyRef.name}
              onChange={(_e, value) => patch({ topologyRef: { name: value } })}
            >
              <FormSelectOption value="" label="Choose a topology" isPlaceholder />
              {(topologies.data ?? []).map((topology) => (
                <FormSelectOption
                  key={topology.metadata.name}
                  value={topology.metadata.name}
                  label={topology.metadata.name}
                />
              ))}
            </FormSelect>
          </FormGroup>
        </GridItem>
      </Grid>

      <Title headingLevel="h2">Load</Title>
      <FormGroup label="Profile" fieldId="profile">
        <FormSelect
          id="profile"
          name="profile"
          value={spec.profile}
          onChange={(_e, value) => patch({ profile: value as TestProfile })}
        >
          <FormSelectOption value="Quick" label="Quick - a bounded run, started from a test run" />
          <FormSelectOption value="Burn" label="Burn - sustained load until stopped" />
        </FormSelect>
      </FormGroup>
      <Grid hasGutter md={6}>
        <GridItem span={12} md={6}>
          <FormGroup label="Virtual users" fieldId="vus">
            <TextInput
              id="vus"
              name="vus"
              type="number"
              min={1}
              value={spec.vus}
              onChange={(_e, value) => patch({ vus: Number(value) })}
            />
          </FormGroup>
        </GridItem>
        {spec.profile === "Quick" ? (
          <>
            <GridItem span={12} md={6}>
              <FormGroup label="Logins per virtual user" fieldId="iterations">
                <TextInput
                  id="iterations"
                  name="iterations"
                  type="number"
                  min={1}
                  value={spec.iterations ?? 1000}
                  onChange={(_e, value) => patch({ iterations: Number(value) })}
                />
              </FormGroup>
            </GridItem>
            <GridItem span={12} md={6}>
              <FormGroup label="Time limit - caps a run that cannot finish its logins" fieldId="quickDuration">
                <TextInput
                  id="quickDuration"
                  name="quickDuration"
                  value={spec.quickDuration ?? ""}
                  placeholder="150s"
                  onChange={(_e, value) => patch({ quickDuration: value })}
                />
              </FormGroup>
            </GridItem>
          </>
        ) : (
          <GridItem span={12} md={6}>
            <FormGroup label="Time limit" fieldId="burnDuration">
              <TextInput
                id="burnDuration"
                name="burnDuration"
                value={spec.burnDuration ?? ""}
                placeholder="8760h"
                onChange={(_e, value) => patch({ burnDuration: value })}
              />
            </FormGroup>
          </GridItem>
        )}
        <GridItem span={12} md={6}>
          <FormGroup label="Flow slug" fieldId="flow">
            <TextInput
              id="flow"
              name="flow"
              value={spec.flow ?? ""}
              placeholder="default-authentication-flow"
              onChange={(_e, value) => patch({ flow: value })}
            />
          </FormGroup>
        </GridItem>
        <GridItem span={12} md={6}>
          <FormGroup label="Trace sample rate" fieldId="traceSampleRate">
            <TextInput
              id="traceSampleRate"
              name="traceSampleRate"
              value={spec.traceSampleRate ?? ""}
              placeholder="0.01"
              onChange={(_e, value) => patch({ traceSampleRate: value })}
            />
          </FormGroup>
        </GridItem>
      </Grid>

      <ActionGroup>
        <Button type="submit" isDisabled={saving}>
          {saving ? "Saving..." : existing ? "Save changes" : "Create test case"}
        </Button>
        <Button variant="link" onClick={() => router.push("/testcases")}>
          Cancel
        </Button>
      </ActionGroup>
    </Form>
  );
}
