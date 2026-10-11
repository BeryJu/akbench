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

import { FormEvent, ReactNode, useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { useAuth } from './auth';
import { Alert, errorMessage, Field } from './components/ui';

/** AuthGate renders its children once signed in, the login screen before. */
export function AuthGate({ children }: { children: ReactNode }) {
  const auth = useAuth();
  if (auth.configError) {
    return (
      <div className="login">
        <Alert>Could not reach the backend: {auth.configError}</Alert>
      </div>
    );
  }
  if (!auth.config) {
    return null;
  }
  return auth.authenticated ? <>{children}</> : <Login />;
}

function Login() {
  const auth = useAuth();
  const [token, setToken] = useState('');
  const [error, setError] = useState<string>();

  function submit(e: FormEvent) {
    e.preventDefault();
    if (token.trim()) {
      auth.loginWithToken(token.trim());
    }
  }

  return (
    <div className="login panel">
      <div className="brand">
        <img src="/favicon.svg" alt="" />
        authentik bench
      </div>
      <Alert kind="info">{auth.notice}</Alert>
      <Alert>{error}</Alert>
      {auth.config?.oidc && (
        <>
          <button
            className="primary"
            onClick={() => auth.loginWithOIDC().catch(err => setError(errorMessage(err)))}
          >
            Sign in with SSO
          </button>
          <div className="or">or use an API token</div>
        </>
      )}
      <form onSubmit={submit} className="row" style={{ flexDirection: 'column', alignItems: 'stretch' }}>
        <Field label="API token" help="cmd/apiserver's BENCH_API_TOKEN / token Secret">
          <input type="password" value={token} onChange={e => setToken(e.target.value)} autoFocus={!auth.config?.oidc} />
        </Field>
        <button type="submit" disabled={!token.trim()}>
          Sign in with token
        </button>
      </form>
    </div>
  );
}

/** OIDCCallback finishes the authorization-code redirect. */
export function OIDCCallback() {
  const auth = useAuth();
  const navigate = useNavigate();
  const [error, setError] = useState<string>();
  useEffect(() => {
    if (!auth.config) {
      return;
    }
    auth
      .completeLogin()
      .then(back => navigate(back, { replace: true }))
      .catch(err => setError(errorMessage(err)));
    // Runs once the auth config is known; completeLogin is single-shot.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.config]);
  return (
    <div className="login">
      {error ? (
        <>
          <Alert>Sign-in failed: {error}</Alert>
          <a href="/">Back</a>
        </>
      ) : (
        <div className="muted">Signing in…</div>
      )}
    </div>
  );
}

/** OIDCSilent runs inside oidc-client-ts's hidden renewal iframe. */
export function OIDCSilent() {
  const auth = useAuth();
  useEffect(() => {
    if (auth.config) {
      void auth.completeSilentLogin();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.config]);
  return null;
}
