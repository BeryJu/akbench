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

// List + create/edit views for TestCaseTemplate, mirroring
// TestCasesPage.tsx's own structure. Makes the operator's built-in "login"/
// "oauth-oidc" templates (seeded by internal/controller/seedtemplates.go)
// and any custom ones editable here instead of only via raw kubectl/YAML -
// the actual point of this CRD existing at all (see TestCaseForm.tsx's own
// template picker, which lists these same objects).
import { EmptyContent, Link, SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Button, Chip, Stack } from '@mui/material';
import { useParams } from 'react-router-dom';
import { api, ApiError, MANAGEMENT_CLUSTER } from './api';
import { TestCaseTemplateSection } from './DetailSections';
import { PrimaryLinkButton } from './PrimaryLinkButton';
import { StatusChip } from './StatusPopover';
import { TestCaseTemplateForm } from './TestCaseTemplateForm';
import { useResource } from './useResource';

export function TestCaseTemplatesPage() {
  const templates = useResource(() => api.testCaseTemplates());

  async function remove(name: string) {
    if (!window.confirm(`Delete test case template "${name}"? Any TestCase still referencing it will stop reconciling.`)) {
      return;
    }
    try {
      await api.deleteTestCaseTemplate(name);
      templates.reload();
    } catch (err) {
      window.alert(err instanceof ApiError ? err.message : String(err));
    }
  }

  return (
    <SectionBox
      title="Test case templates"
      headerProps={{
        actions: [
          <PrimaryLinkButton key="new" routeName="authentik-bench-testcasetemplates-new" label="New template" />,
        ],
      }}
    >
      {templates.error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {templates.error}
        </Alert>
      )}
      <SimpleTable
        columns={[
          {
            label: 'Name',
            getter: template => (
              <Link routeName="authentik-bench-testcasetemplates-edit" params={{ name: template.metadata.name }}>
                {template.metadata.name}
              </Link>
            ),
          },
          { label: 'Description', getter: template => template.spec.description ?? '' },
          {
            label: 'OAuth client',
            getter: template =>
              template.spec.requiresOAuthClient ? <Chip size="small" label="Required" color="warning" /> : null,
          },
          {
            label: 'Status',
            getter: template => (
              <StatusChip
                kind="TestCaseTemplate"
                name={template.metadata.name}
                namespace={template.metadata.namespace ?? 'default'}
                cluster={MANAGEMENT_CLUSTER}
                phase={template.status?.phase}
                conditions={template.status?.conditions}
              />
            ),
          },
          {
            label: '',
            getter: template => (
              <Stack direction="row" spacing={1}>
                <Button size="small" color="error" onClick={() => void remove(template.metadata.name)}>
                  Delete
                </Button>
              </Stack>
            ),
          },
        ]}
        data={templates.data ?? []}
        emptyMessage="No test case templates yet."
      />
      {templates.data?.length === 0 && (
        <EmptyContent>
          The operator seeds "login" and "oauth-oidc" automatically on startup - if neither shows up here, check the
          operator's own logs.
        </EmptyContent>
      )}
    </SectionBox>
  );
}

export function TestCaseTemplateCreatePage() {
  return <TestCaseTemplateForm />;
}

export function TestCaseTemplateEditPage() {
  const { name } = useParams<{ name: string }>();
  const existing = useResource(() => api.testCaseTemplate(name), [name]);
  if (existing.loading) {
    return null;
  }
  if (existing.error) {
    return (
      <SectionBox title="Edit test case template">
        <Alert severity="error">{existing.error}</Alert>
      </SectionBox>
    );
  }
  return (
    <>
      <TestCaseTemplateForm existing={existing.data} />
      <SectionBox title="Used by">
        <TestCaseTemplateSection name={name} />
      </SectionBox>
    </>
  );
}
