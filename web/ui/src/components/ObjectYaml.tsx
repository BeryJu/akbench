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
