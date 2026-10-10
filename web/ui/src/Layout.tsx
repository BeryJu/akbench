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

const nav = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/deployments', label: 'Deployments' },
  { to: '/testcases', label: 'Test cases' },
  { to: '/testcasetemplates', label: 'Test case templates' },
  { to: '/clusters', label: 'Clusters' },
  { to: '/testruns', label: 'Test runs' },
  { to: '/compare', label: 'Compare' },
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
        {nav.map(item => (
          <NavLink key={item.to} to={item.to} end={item.end} className="nav-link">
            {item.label}
          </NavLink>
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
