import Editor, { loader } from '@monaco-editor/react';
import * as monaco from 'monaco-editor';
import EditorWorker from 'monaco-editor/editor/editor.worker?worker';
import TsWorker from 'monaco-editor/language/typescript/ts.worker?worker';
import { useDarkTheme } from '../theme';
import type { CodeEditorProps } from './CodeEditor';

self.MonacoEnvironment = {
  getWorker(_id, label) {
    return label === 'typescript' || label === 'javascript' ? new TsWorker() : new EditorWorker();
  },
};
loader.config({ monaco });

export default function MonacoEditor({ value, onChange, language, height = '300px' }: CodeEditorProps) {
  // Follows html[data-theme] like the rest of the app, not the OS media
  // query directly, so it can never disagree with the page around it.
  const dark = useDarkTheme();
  return (
    <Editor
      height={height}
      language={language}
      theme={dark ? 'vs-dark' : 'light'}
      value={value}
      onChange={v => onChange(v ?? '')}
      options={{ minimap: { enabled: false }, scrollBeyondLastLine: false, fontSize: 13, wordWrap: 'on' }}
    />
  );
}
