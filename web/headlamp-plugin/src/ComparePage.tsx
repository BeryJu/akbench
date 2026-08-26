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

// Ported from the deleted web/frontend/app/compare/page.tsx, on Headlamp's
// own SectionBox/SimpleTable instead of PatternFly.
import { SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Checkbox, FormControlLabel, FormGroup, Grid, MenuItem, TextField, Typography } from '@mui/material';
import { useMemo, useState } from 'react';
import { api } from './api';
import { formatMetricValue } from './format';
import { MetricsChart } from './MetricsChart';
import { CompareMetric, CompareResponse } from './types';
import { useResource } from './useResource';

const SUMMARY_WINDOWS = ['1m', '5m', '15m', '1h'];
const CHART_RANGES = [
  { label: 'Last 15 minutes', seconds: 900 },
  { label: 'Last 1 hour', seconds: 3600 },
  { label: 'Last 6 hours', seconds: 21600 },
  { label: 'Last 24 hours', seconds: 86400 },
];

/** best picks the winning test per metric, honouring the metric's direction. */
function best(response: CompareResponse, key: string, better: string): string | undefined {
  const scored = response.results
    .filter(result => result.values[key] !== undefined)
    .map(result => ({ testid: result.testid, value: result.values[key] as number }));
  if (scored.length < 2) {
    return undefined;
  }
  return scored.reduce((a, b) => ((better === 'lower' ? b.value < a.value : b.value > a.value) ? b : a))
    .testid;
}

export function ComparePage() {
  const testCases = useResource(() => api.testCases(), [], 0);
  const [selected, setSelected] = useState<string[]>([]);
  const [window, setWindow] = useState('5m');
  const [rangeSeconds, setRangeSeconds] = useState(CHART_RANGES[0].seconds);

  const comparison = useResource(
    () =>
      selected.length
        ? api.compare(selected, window)
        : Promise.resolve(undefined as unknown as CompareResponse),
    [selected.join(','), window],
    10000
  );

  const metrics = useMemo(() => comparison.data?.metrics ?? [], [comparison.data]);
  const results = comparison.data?.results ?? [];

  function toggle(name: string) {
    setSelected(current => (current.includes(name) ? current.filter(id => id !== name) : [...current, name]));
  }

  const summaryColumns = [
    {
      label: 'Metric',
      getter: (m: CompareMetric) => (
        <>
          {m.label}{' '}
          <Typography component="span" variant="caption" color="text.secondary">
            ({m.better} is better)
          </Typography>
        </>
      ),
    },
    ...results.map(result => ({
      label: result.testid,
      getter: (m: CompareMetric) => {
        const winner = comparison.data ? best(comparison.data, m.key, m.better) : undefined;
        return (
          <code>
            {formatMetricValue(result.values[m.key], m.unit)}
            {winner === result.testid ? ' ✓' : ''}
          </code>
        );
      },
    })),
  ];

  return (
    <SectionBox
      title="Compare"
      subtitle={`Pick the tests to put side by side. The table summarizes the last ${window} of each; the charts below plot them over time.`}
    >
      {(comparison.error || testCases.error) && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {comparison.error ?? testCases.error}
        </Alert>
      )}

      <Typography variant="h6">Tests</Typography>
      <FormGroup row sx={{ mb: 2 }}>
        {(testCases.data ?? []).map(testCase => (
          <FormControlLabel
            key={testCase.metadata.name}
            control={
              <Checkbox
                checked={selected.includes(testCase.metadata.name)}
                onChange={() => toggle(testCase.metadata.name)}
              />
            }
            label={testCase.metadata.name}
          />
        ))}
        {testCases.data?.length === 0 && (
          <Typography variant="body2" color="text.secondary">
            No test cases yet.
          </Typography>
        )}
      </FormGroup>

      <Grid container spacing={2} sx={{ mb: 3 }}>
        <Grid item xs={12} md={4}>
          <TextField select fullWidth label="Summary window" value={window} onChange={e => setWindow(e.target.value)}>
            {SUMMARY_WINDOWS.map(option => (
              <MenuItem key={option} value={option}>
                {option}
              </MenuItem>
            ))}
          </TextField>
        </Grid>
        <Grid item xs={12} md={4}>
          <TextField
            select
            fullWidth
            label="Chart range"
            value={rangeSeconds}
            onChange={e => setRangeSeconds(Number(e.target.value))}
          >
            {CHART_RANGES.map(option => (
              <MenuItem key={option.seconds} value={option.seconds}>
                {option.label}
              </MenuItem>
            ))}
          </TextField>
        </Grid>
      </Grid>

      {selected.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          Select at least one test.
        </Typography>
      ) : (
        <>
          <Typography variant="h6" gutterBottom>
            Summary
          </Typography>
          <SimpleTable columns={summaryColumns} data={metrics} emptyMessage="No metrics yet." />

          <Typography variant="h6" gutterBottom sx={{ mt: 3 }}>
            Over time
          </Typography>
          <Grid container spacing={2}>
            {metrics.map(metric => (
              <Grid item xs={12} md={6} key={metric.key}>
                <MetricsChart metric={metric.key} testids={selected} rangeSeconds={rangeSeconds} />
              </Grid>
            ))}
          </Grid>
        </>
      )}
    </SectionBox>
  );
}
