import { lazy, StrictMode, Suspense } from 'react';
import { createRoot } from 'react-dom/client';
import { createBrowserRouter, Link, RouterProvider } from 'react-router';
import { AuthProvider, CALLBACK_PATH, SILENT_PATH } from './auth';
import { ToastHost } from './components/Toast';
import { EventsProvider } from './events';
import { Layout } from './Layout';
import { AuthGate, OIDCCallback, OIDCSilent } from './Login';
import './styles.css';

// @goauthentik/theme keys its dark-mode values off html[data-theme="dark"],
// not a prefers-color-scheme media query - bridge the two so this app still
// just follows the OS, same as before, including live switches (e.g. a
// laptop auto-dimming at night) without a page reload.
const darkMedia = window.matchMedia('(prefers-color-scheme: dark)');
const applyTheme = (isDark: boolean) => {
  document.documentElement.dataset.theme = isDark ? 'dark' : 'light';
};
applyTheme(darkMedia.matches);
darkMedia.addEventListener('change', e => applyTheme(e.matches));

// Every page is its own chunk, so the first load only pays for the page it
// lands on (charts, forms and the terminal each come in when first needed).
const page = <T extends Record<string, unknown>>(load: () => Promise<T>, name: keyof T) => {
  const Component = lazy(async () => ({ default: (await load())[name] as React.ComponentType }));
  return (
    <Suspense fallback={null}>
      <Component />
    </Suspense>
  );
};
const deployments = () => import('./pages/Deployments');
const testCases = () => import('./pages/TestCases');
const templates = () => import('./pages/TestCaseTemplates');
const clusters = () => import('./pages/Clusters');
const testRuns = () => import('./pages/TestRuns');
const grafana = () => import('./pages/Grafana');
const infrastructure = () => import('./pages/Infrastructure');

function NotFound() {
  return (
    <div>
      <h1>Not found</h1>
      <p>
        <Link to="/">Back to the dashboard</Link>
      </p>
    </div>
  );
}

const router = createBrowserRouter([
  { path: CALLBACK_PATH, element: <OIDCCallback /> },
  { path: SILENT_PATH, element: <OIDCSilent /> },
  {
    path: '/',
    element: (
      <AuthGate>
        <Layout />
      </AuthGate>
    ),
    children: [
      { index: true, element: page(() => import('./pages/Dashboard'), 'DashboardPage') },
      { path: 'deployments', element: page(deployments, 'DeploymentsPage') },
      { path: 'deployments/new', element: page(deployments, 'DeploymentCreatePage') },
      { path: 'deployments/:name', element: page(deployments, 'DeploymentViewPage') },
      { path: 'deployments/:name/edit', element: page(deployments, 'DeploymentEditPage') },
      { path: 'testcases', element: page(testCases, 'TestCasesPage') },
      { path: 'testcases/new', element: page(testCases, 'TestCaseCreatePage') },
      { path: 'testcases/:name', element: page(testCases, 'TestCaseViewPage') },
      { path: 'testcases/:name/edit', element: page(testCases, 'TestCaseEditPage') },
      { path: 'testcasetemplates', element: page(templates, 'TestCaseTemplatesPage') },
      { path: 'testcasetemplates/new', element: page(templates, 'TestCaseTemplateCreatePage') },
      { path: 'testcasetemplates/:name', element: page(templates, 'TestCaseTemplateViewPage') },
      { path: 'testcasetemplates/:name/edit', element: page(templates, 'TestCaseTemplateEditPage') },
      { path: 'clusters', element: page(clusters, 'ClustersPage') },
      { path: 'clusters/new', element: page(clusters, 'ClusterCreatePage') },
      { path: 'clusters/:name', element: page(clusters, 'ClusterViewPage') },
      { path: 'clusters/:name/edit', element: page(clusters, 'ClusterEditPage') },
      { path: 'infrastructure', element: page(infrastructure, 'InfrastructurePage') },
      { path: 'infrastructure/incus/new', element: page(infrastructure, 'IncusCredentialCreatePage') },
      { path: 'infrastructure/incus/:name', element: page(infrastructure, 'IncusCredentialViewPage') },
      { path: 'infrastructure/incus/:name/edit', element: page(infrastructure, 'IncusCredentialEditPage') },
      { path: 'testruns', element: page(testRuns, 'TestRunsPage') },
      { path: 'testruns/:name', element: page(testRuns, 'TestRunViewPage') },
      { path: 'compare', element: page(() => import('./pages/Compare'), 'ComparePage') },
      { path: 'alerts', element: page(() => import('./pages/Alerts'), 'AlertsPage') },
      { path: 'grafana', element: page(grafana, 'GrafanaPage') },
      { path: 'grafana/:cluster', element: page(grafana, 'GrafanaPage') },
      // Rendered by Layout's persistent ShellWorkspace, not an Outlet.
      { path: 'shell', element: null },
      { path: '*', element: <NotFound /> },
    ],
  },
]);

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AuthProvider>
      <EventsProvider>
        <RouterProvider router={router} />
      </EventsProvider>
    </AuthProvider>
    <ToastHost />
  </StrictMode>
);
