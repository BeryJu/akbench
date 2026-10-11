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

import { lazy, Suspense, useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router';
import { useAuth } from './auth';

// xterm.js only loads once the shell is first opened.
const ShellWorkspace = lazy(async () => ({ default: (await import('./shell/ShellWorkspace')).ShellWorkspace }));

// Grouped by what you're doing with it, not alphabetically or by when it
// was added: overview first, then the things you define/configure, then the
// things that show what already happened. Shell stays separate below a
// divider - it's a tool, not a resource view.
const navSections: { label?: string; items: { to: string; label: string; end?: boolean }[] }[] = [
  { items: [{ to: '/', label: 'Dashboard', end: true }] },
  {
    label: 'Configure',
    items: [
      { to: '/clusters', label: 'Clusters' },
      { to: '/deployments', label: 'Deployments' },
      { to: '/testcases', label: 'Test cases' },
      { to: '/testcasetemplates', label: 'Test case templates' },
    ],
  },
  {
    label: 'Activity',
    items: [
      { to: '/testruns', label: 'Test runs' },
      { to: '/compare', label: 'Compare' },
      { to: '/alerts', label: 'Alerts' },
      { to: '/grafana', label: 'Grafana' },
    ],
  },
];

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
          <div key={section.label ?? i}>
            {section.label && <div className="nav-section-label">{section.label}</div>}
            {section.items.map(item => (
              <NavLink key={item.to} to={item.to} end={item.end} className="nav-link">
                {item.label}
              </NavLink>
            ))}
          </div>
        ))}
        <div className="nav-sep" />
        <NavLink to="/shell" className="nav-link">
          Shell
        </NavLink>
        <div className="sidebar-foot">
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
