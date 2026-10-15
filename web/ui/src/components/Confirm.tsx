// A confirmation modal built on the native <dialog> element, called from
// anywhere via the promise-returning `confirm()` - the same module-state
// pattern as Toast.tsx, so confirmAndRun (components/ui.tsx) can stay a
// plain function. showModal() gives us the backdrop, focus trapping, inert
// page and Escape-to-cancel for free.
import { useEffect, useRef, useSyncExternalStore } from "react";

export interface ConfirmOptions {
    message: string;
    title?: string;
    /** Label of the confirming button, e.g. "Delete". Defaults to "Confirm". */
    confirmLabel?: string;
    /** Styles the confirming button as destructive. */
    danger?: boolean;
    /** An extra opt-in shown under the message, e.g. "Also delete X". */
    checkbox?: { label: string; defaultChecked?: boolean };
}

/** ConfirmResult is the user's yes; null means they cancelled. */
export interface ConfirmResult {
    /** Whether the optional checkbox was ticked (false without one). */
    checked: boolean;
}

interface Pending extends ConfirmOptions {
    id: number;
    resolve: (result: ConfirmResult | null) => void;
}

// One at a time: a second confirm() while one is open cancels the first,
// rather than queueing a question the user never asked for.
let nextId = 1;
let pending: Pending | null = null;
const listeners = new Set<() => void>();

function emit() {
    listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}

function snapshot() {
    return pending;
}

function settle(result: ConfirmResult | null) {
    const current = pending;
    pending = null;
    emit();
    current?.resolve(result);
}

/** confirm asks the question in a modal and resolves to the user's answer. */
export function confirm(options: ConfirmOptions | string): Promise<ConfirmResult | null> {
    const opts = typeof options === "string" ? { message: options } : options;
    settle(null);
    return new Promise((resolve) => {
        pending = { ...opts, id: nextId++, resolve };
        emit();
    });
}

/** ConfirmHost renders the open dialog. Mount exactly once, at the app root. */
export function ConfirmHost() {
    const current = useSyncExternalStore(subscribe, snapshot);
    const ref = useRef<HTMLDialogElement>(null);

    useEffect(() => {
        const dialog = ref.current;
        if (!dialog) {
            return;
        }
        if (current && !dialog.open) {
            // returnValue sticks from the last submit; Escape leaves it alone.
            dialog.returnValue = "";
            dialog.showModal();
        } else if (!current && dialog.open) {
            dialog.close();
        }
    }, [current]);

    return (
        <dialog
            ref={ref}
            className="confirm"
            aria-labelledby={current?.title ? "confirm-title" : undefined}
            aria-describedby="confirm-message"
            // Escape and form submission both land here; returnValue tells
            // them apart (only the confirm button sets it).
            onClose={(e) => {
                if (pending) {
                    const dialog = e.currentTarget;
                    const checkbox = dialog.querySelector<HTMLInputElement>("input[name=option]");
                    settle(
                        dialog.returnValue === "confirm"
                            ? { checked: checkbox?.checked ?? false }
                            : null,
                    );
                }
            }}
            // A click on the backdrop targets the <dialog> itself.
            onClick={(e) => e.target === e.currentTarget && settle(null)}
        >
            {current && (
                // Keyed on the request so a replacing confirm() starts with a
                // fresh, uncontrolled checkbox.
                <form method="dialog" key={current.id}>
                    {current.title && <h2 id="confirm-title">{current.title}</h2>}
                    <p id="confirm-message">{current.message}</p>
                    {current.checkbox && (
                        <label className="check">
                            <input
                                type="checkbox"
                                name="option"
                                defaultChecked={current.checkbox.defaultChecked}
                            />
                            {current.checkbox.label}
                        </label>
                    )}
                    <div className="row confirm-actions">
                        <button type="submit" value="cancel" autoFocus>
                            Cancel
                        </button>
                        <button
                            type="submit"
                            value="confirm"
                            className={current.danger ? "danger-solid" : "primary"}
                        >
                            {current.confirmLabel ?? "Confirm"}
                        </button>
                    </div>
                </form>
            )}
        </dialog>
    );
}
