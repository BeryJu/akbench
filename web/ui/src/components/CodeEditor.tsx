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

// Monaco, lazy-loaded so its multi-megabyte bundle only downloads on the
// pages that actually edit code, and bundled locally (monaco.ts) rather than
// fetched from a CDN at runtime the way @monaco-editor/react defaults to.
import { lazy, Suspense } from 'react';

const MonacoEditor = lazy(() => import('./monaco'));

export interface CodeEditorProps {
  value: string;
  onChange: (value: string) => void;
  language: 'yaml' | 'typescript';
  height?: string;
}

export function CodeEditor(props: CodeEditorProps) {
  return (
    <div className="editor">
      <Suspense fallback={<div className="muted" style={{ height: props.height ?? '300px', padding: 12 }}>Loading editor…</div>}>
        <MonacoEditor {...props} />
      </Suspense>
    </div>
  );
}
