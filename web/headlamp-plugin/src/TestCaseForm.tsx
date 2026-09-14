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

// Ported from the deleted web/frontend/components/testcase-form.tsx, on MUI +
// Headlamp's own SectionBox instead of PatternFly/Next.js's router.
import { Router } from '@kinvolk/headlamp-plugin/lib';
import { Link, SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import Editor from '@monaco-editor/react';
import { Alert, Box, Button, Grid, MenuItem, TextField, Typography, useTheme } from '@mui/material';
import { useState } from 'react';
import { useHistory } from 'react-router-dom';
import { api, ApiError } from './api';
import { TestCase, TestCaseSpec, TestProfile } from './types';
import { useResource } from './useResource';

const INLINE_TEMPLATE = '__inline__';

function emptySpec(): TestCaseSpec {
  return {
    deploymentRef: { name: '' },
    template: 'login',
    profile: 'Quick',
    vus: 8,
    iterations: 1000,
    flow: 'default-authentication-flow',
    quickDuration: '150s',
    burnDuration: '8760h',
    traceSampleRate: '0.01',
  };
}

export function TestCaseForm({ existing }: { existing?: TestCase }) {
  const history = useHistory();
  const theme = useTheme();
  const deployments = useResource(() => api.deployments(), [], 0);
  const templates = useResource(() => api.testCaseTemplates(), [], 0);
  const [name, setName] = useState(existing?.metadata.name ?? '');
  const [spec, setSpec] = useState<TestCaseSpec>(existing?.spec ?? emptySpec());
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const patch = (changes: Partial<TestCaseSpec>) => setSpec(current => ({ ...current, ...changes }));

  // scriptConfigMapRef (an externally-managed ConfigMap) is a power-user
  // escape hatch this form doesn't expose - only template vs. inline.
  const scriptChoice = spec.inline !== undefined ? INLINE_TEMPLATE : (spec.template ?? 'login');

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      if (existing) {
        await api.updateTestCase(existing.metadata.name, { spec });
      } else {
        await api.createTestCase({ metadata: { name }, spec });
      }
      history.push(Router.createRouteURL('authentik-bench-testcases'));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
      setSaving(false);
    }
  }

  return (
    <SectionBox title={existing ? `Edit ${existing.metadata.name}` : 'New test case'}>
      <Box component="form" onSubmit={submit}>
        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </Alert>
        )}

        <Typography variant="h6" gutterBottom>
          Identity
        </Typography>
        <Grid container spacing={2} sx={{ mb: 3 }}>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              required
              label="Name"
              helperText="This is the k6 testid every dashboard splits on"
              value={name}
              disabled={Boolean(existing)}
              onChange={e => setName(e.target.value)}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              select
              fullWidth
              required
              label="Deployment to drive"
              value={spec.deploymentRef.name}
              onChange={e => patch({ deploymentRef: { name: e.target.value } })}
            >
              <MenuItem value="">
                <em>Choose a deployment</em>
              </MenuItem>
              {(deployments.data ?? []).map(deployment => (
                <MenuItem key={deployment.metadata.name} value={deployment.metadata.name}>
                  {deployment.metadata.name}
                </MenuItem>
              ))}
            </TextField>
          </Grid>
        </Grid>

        <Typography variant="h6" gutterBottom>
          Test template
        </Typography>
        <Grid container spacing={2} sx={{ mb: 3 }}>
          <Grid item xs={12} md={6}>
            <TextField
              select
              fullWidth
              label="Template"
              value={scriptChoice}
              onChange={e => {
                const value = e.target.value;
                if (value === INLINE_TEMPLATE) {
                  patch({ inline: spec.inline ?? '', template: undefined });
                } else {
                  patch({ template: value, inline: undefined });
                }
              }}
            >
              {(templates.data ?? []).map(t => (
                <MenuItem key={t.metadata.name} value={t.metadata.name}>
                  {t.metadata.name}
                  {t.spec.description ? ` - ${t.spec.description}` : ''}
                </MenuItem>
              ))}
              <MenuItem value={INLINE_TEMPLATE}>Custom - inline k6 script</MenuItem>
            </TextField>
          </Grid>
          <Grid item xs={12}>
            <Link routeName="authentik-bench-testcasetemplates">Manage test case templates</Link>
          </Grid>
          {scriptChoice === INLINE_TEMPLATE && (
            <Grid item xs={12}>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                Inline k6 script (TypeScript) - stored directly on this TestCase.
              </Typography>
              <Editor
                height="300px"
                language="typescript"
                theme={theme.palette.mode === 'dark' ? 'vs-dark' : 'light'}
                value={spec.inline ?? ''}
                onChange={value => patch({ inline: value ?? '' })}
              />
            </Grid>
          )}
        </Grid>

        <Typography variant="h6" gutterBottom>
          Load
        </Typography>
        <TextField
          select
          fullWidth
          label="Profile"
          sx={{ mb: 2 }}
          value={spec.profile}
          onChange={e => patch({ profile: e.target.value as TestProfile })}
        >
          <MenuItem value="Quick">Quick - a bounded run, started from a test run</MenuItem>
          <MenuItem value="Burn">Burn - sustained load until stopped</MenuItem>
        </TextField>
        <Grid container spacing={2}>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              type="number"
              label="Virtual users"
              inputProps={{ min: 1 }}
              value={spec.vus}
              onChange={e => patch({ vus: Number(e.target.value) })}
            />
          </Grid>
          {spec.profile === 'Quick' ? (
            <>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label="Logins per virtual user"
                  inputProps={{ min: 1 }}
                  value={spec.iterations ?? 1000}
                  onChange={e => patch({ iterations: Number(e.target.value) })}
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  label="Time limit"
                  helperText="Caps a run that cannot finish its logins"
                  value={spec.quickDuration ?? ''}
                  placeholder="150s"
                  onChange={e => patch({ quickDuration: e.target.value })}
                />
              </Grid>
            </>
          ) : (
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label="Time limit"
                value={spec.burnDuration ?? ''}
                placeholder="8760h"
                onChange={e => patch({ burnDuration: e.target.value })}
              />
            </Grid>
          )}
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label="Flow slug"
              value={spec.flow ?? ''}
              placeholder="default-authentication-flow"
              onChange={e => patch({ flow: e.target.value })}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label="Trace sample rate"
              value={spec.traceSampleRate ?? ''}
              placeholder="0.01"
              onChange={e => patch({ traceSampleRate: e.target.value })}
            />
          </Grid>
        </Grid>

        <Box sx={{ display: 'flex', gap: 2, mt: 3 }}>
          <Button type="submit" variant="contained" disabled={saving}>
            {saving ? 'Saving…' : existing ? 'Save changes' : 'Create test case'}
          </Button>
          <Button onClick={() => history.push(Router.createRouteURL('authentik-bench-testcases'))}>Cancel</Button>
        </Box>
      </Box>
    </SectionBox>
  );
}
