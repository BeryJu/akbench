"use client";

import { useCallback, useEffect, useState } from "react";
import { Alert, Button, EmptyState, EmptyStateBody, Label } from "@patternfly/react-core";
import { ApiError } from "@/lib/api";
import type { Condition, TestRunPhase } from "@/lib/types";
import { readyCondition } from "@/lib/types";

/**
 * useResource polls a fetcher on an interval. Everything in this UI reflects
 * cluster state that a controller changes behind the user's back, so a page
 * that fetched once would be wrong within seconds of any action.
 */
export function useResource<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = [],
  intervalMs = 5000,
): { data: T | undefined; error: string | undefined; reload: () => void; loading: boolean } {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      setData(await fetcher());
      setError(undefined);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  useEffect(() => {
    void load();
    if (intervalMs <= 0) {
      return;
    }
    const timer = setInterval(() => void load(), intervalMs);
    return () => clearInterval(timer);
  }, [load, intervalMs]);

  return { data, error, reload: () => void load(), loading };
}

export function ErrorBanner({ error }: { error?: string }) {
  if (!error) {
    return null;
  }
  return (
    <Alert variant="danger" isInline title={error} style={{ marginBottom: "1rem" }} />
  );
}

export function ReadyBadge({ conditions }: { conditions?: Condition[] }) {
  const ready = readyCondition(conditions);
  if (!ready) {
    return <Label color="orange">Pending</Label>;
  }
  if (ready.status === "True") {
    return <Label color="green">Ready</Label>;
  }
  return (
    <Label color="red" title={ready.message ?? ready.reason}>
      {ready.reason ?? "Not ready"}
    </Label>
  );
}

export function PhaseBadge({ phase }: { phase?: TestRunPhase }) {
  const color = phase === "Succeeded" ? "green" : phase === "Failed" ? "red" : "orange";
  return <Label color={color}>{phase ?? "Pending"}</Label>;
}

/** Ages are relative because "3m ago" answers the only question being asked. */
export function Age({ time }: { time?: string }) {
  if (!time) {
    return <span style={{ color: "var(--pf-t--global--text--color--subtle)" }}>-</span>;
  }
  const seconds = Math.max(0, (Date.now() - new Date(time).getTime()) / 1000);
  const [value, unit] =
    seconds < 60
      ? [seconds, "s"]
      : seconds < 3600
        ? [seconds / 60, "m"]
        : seconds < 86400
          ? [seconds / 3600, "h"]
          : [seconds / 86400, "d"];
  return <span title={time}>{`${Math.floor(value)}${unit}`}</span>;
}

export function Empty({ children }: { children: React.ReactNode }) {
  return (
    <EmptyState variant="xs" headingLevel="h4" titleText="Nothing here yet">
      <EmptyStateBody>{children}</EmptyStateBody>
    </EmptyState>
  );
}

/**
 * ConfirmButton guards the destructive actions. Deleting a topology tears down
 * a real authentik install and its database, which is not something to lose to
 * a mis-click.
 */
export function ConfirmButton({
  children,
  onConfirm,
  confirmLabel = "Really?",
  danger = true,
}: {
  children: React.ReactNode;
  onConfirm: () => void | Promise<void>;
  confirmLabel?: string;
  danger?: boolean;
}) {
  const [armed, setArmed] = useState(false);
  useEffect(() => {
    if (!armed) {
      return;
    }
    const timer = setTimeout(() => setArmed(false), 4000);
    return () => clearTimeout(timer);
  }, [armed]);

  return (
    <Button
      variant={armed ? "danger" : danger ? "secondary" : "primary"}
      onClick={() => {
        if (armed) {
          setArmed(false);
          void onConfirm();
        } else {
          setArmed(true);
        }
      }}
    >
      {armed ? confirmLabel : children}
    </Button>
  );
}
