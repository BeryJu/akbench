// A toast-notification stack, called from anywhere (not just components) via
// the module-level `toast` object - the same "mutable module state a
// Provider wires up" trick api.ts's configureAuth already uses, since
// confirmAndRun (components/ui.tsx) is a plain function, not a hook, and
// needs to report a result without being inside a component.
import { useEffect, useState } from 'react';

export type ToastKind = 'success' | 'error' | 'info';

interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
}

// Errors stay up longer than success/info - they're more likely to need
// re-reading, and less frequent, so the extra screen time doesn't clutter.
const TOAST_TTL_MS: Record<ToastKind, number> = { success: 4000, info: 4000, error: 8000 };

let nextId = 1;
let items: ToastItem[] = [];
let listener: ((items: ToastItem[]) => void) | undefined;

function emit() {
  listener?.(items);
}

function dismiss(id: number) {
  items = items.filter(item => item.id !== id);
  emit();
}

function push(message: string, kind: ToastKind): number {
  const id = nextId++;
  items = [...items, { id, kind, message }];
  emit();
  // Set once at creation, not re-armed by a later toast arriving/leaving -
  // an effect keyed off the whole list would restart every toast's timer
  // whenever any one of them changed.
  setTimeout(() => dismiss(id), TOAST_TTL_MS[kind]);
  return id;
}

export const toast = {
  success: (message: string) => push(message, 'success'),
  error: (message: string) => push(message, 'error'),
  info: (message: string) => push(message, 'info'),
};

/** ToastHost renders the stack. Mount exactly once, at the app root. */
export function ToastHost() {
  const [current, setCurrent] = useState<ToastItem[]>(items);

  useEffect(() => {
    listener = setCurrent;
    setCurrent(items);
    return () => {
      listener = undefined;
    };
  }, []);

  if (current.length === 0) {
    return null;
  }
  return (
    <div className="toast-host" role="status" aria-live="polite">
      {current.map(item => (
        <div
          key={item.id}
          className={`toast ${item.kind}`}
          role="button"
          tabIndex={0}
          onClick={() => dismiss(item.id)}
          onKeyDown={e => (e.key === 'Enter' || e.key === ' ') && dismiss(item.id)}
        >
          {item.message}
        </div>
      ))}
    </div>
  );
}
