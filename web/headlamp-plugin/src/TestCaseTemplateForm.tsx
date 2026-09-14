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

// Create/edit form for TestCaseTemplate - the same Monaco editor
// TestCaseForm.tsx already uses for a TestCase's own inline script, since a
// template's spec.script is exactly that same kind of content (k6
// TypeScript), just stored on a shared, reusable object instead of one
// TestCase.
import { Router } from '@kinvolk/headlamp-plugin/lib';
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import Editor from '@monaco-editor/react';
import { Alert, Box, Button, Checkbox, FormControlLabel, Grid, TextField, Typography, useTheme } from '@mui/material';
import { useState } from 'react';
import { useHistory } from 'react-router-dom';
import { api, ApiError } from './api';
import { TestCaseTemplate, TestCaseTemplateSpec } from './types';

function emptySpec(): TestCaseTemplateSpec {
  return { description: '', script: '', requiresOAuthClient: false };
}

export function TestCaseTemplateForm({ existing }: { existing?: TestCaseTemplate }) {
  const history = useHistory();
  const theme = useTheme();
  const [name, setName] = useState(existing?.metadata.name ?? '');
  const [spec, setSpec] = useState<TestCaseTemplateSpec>(existing?.spec ?? emptySpec());
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const patch = (changes: Partial<TestCaseTemplateSpec>) => setSpec(current => ({ ...current, ...changes }));

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      if (existing) {
        await api.updateTestCaseTemplate(existing.metadata.name, { spec });
      } else {
        await api.createTestCaseTemplate({ metadata: { name }, spec });
      }
      history.push(Router.createRouteURL('authentik-bench-testcasetemplates'));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
      setSaving(false);
    }
  }

  return (
    <SectionBox title={existing ? `Edit ${existing.metadata.name}` : 'New test case template'}>
      <Box component="form" onSubmit={submit}>
        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </Alert>
        )}

        <Grid container spacing={2} sx={{ mb: 2 }}>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              required
              label="Name"
              helperText={'Referenced by a TestCase\'s own spec.template (e.g. "login")'}
              value={name}
              disabled={Boolean(existing)}
              onChange={e => setName(e.target.value)}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label="Description"
              value={spec.description ?? ''}
              onChange={e => patch({ description: e.target.value })}
            />
          </Grid>
        </Grid>

        <FormControlLabel
          sx={{ mb: 2 }}
          control={
            <Checkbox
              checked={Boolean(spec.requiresOAuthClient)}
              onChange={e => patch({ requiresOAuthClient: e.target.checked })}
            />
          }
          label="Requires an OAuth client (provisions an oidc-test-client + OAuth2 provider/application for any TestCase selecting this template)"
        />

        <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
          k6 script (TypeScript) - mirrored verbatim into every TestCase selecting this template.
        </Typography>
        <Editor
          height="500px"
          language="typescript"
          theme={theme.palette.mode === 'dark' ? 'vs-dark' : 'light'}
          value={spec.script}
          onChange={value => patch({ script: value ?? '' })}
        />

        <Box sx={{ display: 'flex', gap: 2, mt: 3 }}>
          <Button type="submit" variant="contained" disabled={saving}>
            {saving ? 'Saving…' : existing ? 'Save changes' : 'Create template'}
          </Button>
          <Button onClick={() => history.push(Router.createRouteURL('authentik-bench-testcasetemplates'))}>
            Cancel
          </Button>
        </Box>
      </Box>
    </SectionBox>
  );
}
