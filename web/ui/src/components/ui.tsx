/*
 * Copyright 2026
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// The handful of layout/feedback primitives every page shares.
import { ReactNode } from 'react';
import { toast } from './Toast';

export function Page({
  title,
  subtitle,
  actions,
  children,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <>
      <div className="page-head">
        <div>
          <h1>{title}</h1>
          {subtitle && <div className="sub">{subtitle}</div>}
        </div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children}
    </>
  );
}

export function Alert({ kind = 'error', children }: { kind?: 'error' | 'info' | 'success'; children: ReactNode }) {
  if (!children) {
    return null;
  }
  return <div className={`alert ${kind}`}>{children}</div>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>;
}

export interface Column<T> {
  label: string;
  render: (row: T) => ReactNode;
  className?: string;
}

export function Table<T>({
  columns,
  rows,
  rowKey,
  empty,
}: {
  columns: Column<T>[];
  rows: T[] | undefined;
  rowKey: (row: T) => string;
  empty: ReactNode;
}) {
  if (rows === undefined) {
    return <div className="muted">Loading…</div>;
  }
  if (rows.length === 0) {
    return <Empty>{empty}</Empty>;
  }
  return (
    <table className="table">
      <thead>
        <tr>
          {columns.map(c => (
            <th key={c.label}>{c.label}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map(row => (
          <tr key={rowKey(row)}>
            {columns.map(c => (
              <td key={c.label} className={c.className}>
                {c.render(row)}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function Field({ label, help, children }: { label: string; help?: ReactNode; children: ReactNode }) {
  return (
    <label className="field">
      {label}
      {children}
      {help && <span className="help">{help}</span>}
    </label>
  );
}

export function Stat({ value, label, hint }: { value: ReactNode; label: string; hint?: ReactNode }) {
  return (
    <div className="panel stat">
      <div className="value">{value}</div>
      <div className="label">{label}</div>
      {hint && <div className="hint">{hint}</div>}
    </div>
  );
}

/** Segmented is a small single-choice toggle (time ranges, windows). */
export function Segmented<T extends string | number>({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
}) {
  return (
    <div className="seg" role="group" aria-label={label}>
      {options.map(o => (
        <button
          key={o.value}
          type="button"
          className={o.value === value ? 'on' : undefined}
          aria-pressed={o.value === value}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * confirmAndRun asks first, then runs action, toasting the outcome: a
 * success message if given (most actions here are a delete/kick with
 * nothing more interesting to say than "done", which the row vanishing or
 * refreshing already shows - pass one only when it adds information), and
 * always the error on failure.
 */
export async function confirmAndRun(
  question: string,
  action: () => Promise<unknown>,
  after?: () => void,
  successMessage?: string
) {
  if (!window.confirm(question)) {
    return;
  }
  try {
    await action();
    after?.();
    if (successMessage) {
      toast.success(successMessage);
    }
  } catch (err) {
    toast.error(errorMessage(err));
  }
}

export function formatTimestamp(value: string | undefined): string {
  return value ? new Date(value).toLocaleString() : '-';
}
