import { lazy, Suspense, useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router';
import { api } from './api';
import { buildLabel } from './format';
import { useAuth } from './auth';
import { useResource } from './useResource';

// xterm.js only loads once the shell is first opened.
const ShellWorkspace = lazy(async () => ({ default: (await import('./shell/ShellWorkspace')).ShellWorkspace }));

// Grouped by what you're doing with it: overview first, then setting up
// and running load tests, then reading their results. Infrastructure -
// clusters and the machines under them, set up once and rarely touched -
// sits apart at the bottom of the sidebar, with the shell.
type NavSection = { label?: string; items: { to: string; label: string; end?: boolean }[] };

const navSections: NavSection[] = [
  { items: [{ to: '/', label: 'Dashboard', end: true }] },
  {
    label: 'Load tests',
    items: [
      { to: '/testcases', label: 'Test cases' },
      { to: '/deployments', label: 'Deployments' },
      { to: '/testcasetemplates', label: 'Test case templates' },
    ],
  },
  {
    label: 'Results',
    items: [
      { to: '/testruns', label: 'Test runs' },
      { to: '/compare', label: 'Compare' },
      { to: '/alerts', label: 'Alerts' },
      { to: '/grafana', label: 'Grafana' },
    ],
  },
  {
    label: 'Infrastructure',
    items: [
      { to: '/infrastructure', label: 'Providers' },
      { to: '/clusters', label: 'Clusters' },
      { to: '/shell', label: 'Shell' },
    ],
  }
];

const REPO_URL = 'https://github.com/BeryJu/akbench';

/** BuildBadge identifies the running build, so a screenshot or bug report
 * always says which one it came from. Links to the commit when known. */
function BuildBadge() {
  const build = useResource(() => api.version(), [], 0);
  const info = build.data;
  if (!info) {
    return null;
  }
  const title = [info.commit && `Commit ${info.commit}`, info.goVersion && `Built with ${info.goVersion}`]
    .filter(Boolean)
    .join('\n');
  return info.commit ? (
    <a className="build-badge" href={`${REPO_URL}/commit/${info.commit}`} target="_blank" rel="noreferrer" title={title}>
      {buildLabel(info)}
    </a>
  ) : (
    <span className="build-badge" title={title || undefined}>
      {buildLabel(info)}
    </span>
  );
}

function NavSectionLinks({ section }: { section: NavSection }) {
  return (
    <div>
      {section.label && <div className="nav-section-label">{section.label}</div>}
      {section.items.map(item => (
        <NavLink key={item.to} to={item.to} end={item.end} className="nav-link">
          {item.label}
        </NavLink>
      ))}
    </div>
  );
}

export function Layout() {
  const { identity, logout } = useAuth();
  const location = useLocation();
  const onShell = location.pathname === '/shell';
  // The shell workspace mounts on first visit and then stays mounted (just
  // hidden), so its sessions survive navigating elsewhere in the app.
  const [shellMounted, setShellMounted] = useState(onShell);
  useEffect(() => {
    if (onShell) {
      setShellMounted(true);
    }
  }, [onShell]);

  return (
    <div className="app">
      <nav className="sidebar">
        <div className="brand">
          <img src="/favicon.svg" alt="" />
          authentik bench
        </div>
        {navSections.map((section, i) => (
          <NavSectionLinks key={section.label ?? i} section={section} />
        ))}
        <div className="sidebar-foot">
          <BuildBadge />
          {identity && <span>Signed in as {identity}</span>}
          <button className="small" onClick={() => void logout()}>
            Sign out
          </button>
        </div>
      </nav>
      <main className={onShell ? 'main full' : 'main'}>
        {!onShell && <Outlet />}
        {shellMounted && (
          <Suspense fallback={null}>
            <ShellWorkspace visible={onShell} />
          </Suspense>
        )}
      </main>
    </div>
  );
}
