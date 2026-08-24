"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
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
    <form onSubmit={submit}>
      <ErrorBanner error={error} />

      <fieldset>
        <legend>Identity</legend>
        <div className="grid2">
          <label>
            <span>Name - this is the k6 testid every dashboard splits on</span>
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
            <span>Topology to drive</span>
            <select
              name="topology"
              required
              value={spec.topologyRef.name}
              onChange={(e) => patch({ topologyRef: { name: e.target.value } })}
            >
              <option value="">Choose a topology</option>
              {(topologies.data ?? []).map((topology) => (
                <option key={topology.metadata.name} value={topology.metadata.name}>
                  {topology.metadata.name}
                </option>
              ))}
            </select>
          </label>
        </div>
      </fieldset>

      <fieldset>
        <legend>Load</legend>
        <label>
          <span>Profile</span>
          <select
            name="profile"
            value={spec.profile}
            onChange={(e) => patch({ profile: e.target.value as TestProfile })}
          >
            <option value="Quick">Quick - a bounded run, started from a test run</option>
            <option value="Burn">Burn - sustained load until stopped</option>
          </select>
        </label>
        <div className="grid2">
          <label>
            <span>Virtual users</span>
            <input
              name="vus"
              type="number"
              min={1}
              value={spec.vus}
              onChange={(e) => patch({ vus: Number(e.target.value) })}
            />
          </label>
          {spec.profile === "Quick" ? (
            <>
              <label>
                <span>Logins per virtual user</span>
                <input
                  name="iterations"
                  type="number"
                  min={1}
                  value={spec.iterations ?? 1000}
                  onChange={(e) => patch({ iterations: Number(e.target.value) })}
                />
              </label>
              <label>
                <span>Time limit - caps a run that cannot finish its logins</span>
                <input
                  name="quickDuration"
                  value={spec.quickDuration ?? ""}
                  placeholder="150s"
                  onChange={(e) => patch({ quickDuration: e.target.value })}
                />
              </label>
            </>
          ) : (
            <label>
              <span>Time limit</span>
              <input
                name="burnDuration"
                value={spec.burnDuration ?? ""}
                placeholder="8760h"
                onChange={(e) => patch({ burnDuration: e.target.value })}
              />
            </label>
          )}
          <label>
            <span>Flow slug</span>
            <input
              name="flow"
              value={spec.flow ?? ""}
              placeholder="default-authentication-flow"
              onChange={(e) => patch({ flow: e.target.value })}
            />
          </label>
          <label>
            <span>Trace sample rate</span>
            <input
              name="traceSampleRate"
              value={spec.traceSampleRate ?? ""}
              placeholder="0.01"
              onChange={(e) => patch({ traceSampleRate: e.target.value })}
            />
          </label>
        </div>
      </fieldset>

      <div className="row">
        <button type="submit" className="primary" disabled={saving}>
          {saving ? "Saving..." : existing ? "Save changes" : "Create test case"}
        </button>
        <button type="button" onClick={() => router.push("/testcases")}>
          Cancel
        </button>
      </div>
    </form>
  );
}
