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
