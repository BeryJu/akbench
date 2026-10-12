// The full object as YAML, on every view page: the CRD schema is the source
// of truth for what fields exist, and this is how none of them stay hidden
// just because no form knows about them. Editing anything a form doesn't
// cover is `kubectl edit` in the shell.
import { useMemo } from 'react';
import { stringify } from 'yaml';

export function ObjectYaml({ object, open = false }: { object: unknown; open?: boolean }) {
  const text = useMemo(() => {
    if (!object) {
      return '';
    }
    // managedFields is noise for a human reader.
    const copy = structuredClone(object) as { metadata?: Record<string, unknown> };
    delete copy.metadata?.managedFields;
    return stringify(copy);
  }, [object]);
  if (!object) {
    return null;
  }
  return (
    <details open={open}>
      <summary>YAML</summary>
      <pre className="yaml">{text}</pre>
    </details>
  );
}
