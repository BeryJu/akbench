// Deep links into the shell page. Everything else in the UI hands off to the
// shell this way rather than re-implementing kubectl's views.

/**
 * shellLink opens the shell on a kubeconfig context, optionally with a
 * command typed at the prompt - typed only, never run: the user still
 * presses Enter (see sanitizePrefill).
 */
export function shellLink(context: string, prefill?: string): string {
    const params = new URLSearchParams({ context });
    if (prefill) {
        params.set("prefill", prefill);
    }
    return `/shell?${params.toString()}`;
}

/**
 * shellLinkForCluster is shellLink keyed by a WorkloadCluster name; the
 * shell page resolves it to that cluster's context (CAPI's kubeconfig names
 * it <name>-admin@<name>, but the page reads the real mapping from the
 * backend rather than assuming that).
 */
export function shellLinkForCluster(cluster: string | undefined, prefill?: string): string {
    if (!cluster) {
        return shellLink("management", prefill);
    }
    const params = new URLSearchParams({ cluster });
    if (prefill) {
        params.set("prefill", prefill);
    }
    return `/shell?${params.toString()}`;
}

/**
 * sanitizePrefill strips every control character - above all CR/LF, which
 * would turn "typed at the prompt" into "executed", and ESC, which could
 * drive the terminal. A crafted link must never be able to run anything
 * without the user pressing Enter themselves.
 */
export function sanitizePrefill(value: string | null): string {
    // eslint-disable-next-line no-control-regex
    return (value ?? "").replace(/[\u0000-\u001f\u007f-\u009f]/g, " ").slice(0, 2000);
}
