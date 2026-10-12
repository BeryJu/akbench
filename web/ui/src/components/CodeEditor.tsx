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
