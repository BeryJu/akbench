// Login. Two ways in, both ending in a plain Bearer token on every request
// (api.ts):
//
// - OIDC (when cmd/apiserver reports an issuer at /api/v1/auth/config):
//   authorization code + PKCE as a public client via oidc-client-ts, so no
//   client secret ever reaches the browser. The access token is the bearer
//   token; cmd/apiserver verifies it against the issuer's JWKS
//   (internal/api/oidc.go). Register <origin>/auth/callback and
//   <origin>/auth/silent as redirect URIs on the provider.
// - The static API token, pasted once and kept in localStorage - for dev
//   and for a backend without OIDC.
import { User, UserManager, WebStorageStateStore } from 'oidc-client-ts';
import { createContext, ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { api, AuthConfig, configureAuth } from './api';

const TOKEN_KEY = 'bench.apiToken';

export const CALLBACK_PATH = '/auth/callback';
export const SILENT_PATH = '/auth/silent';

interface AuthState {
  /** undefined while the auth config is still loading. */
  config: AuthConfig | undefined;
  configError: string | undefined;
  authenticated: boolean;
  /** The signed-in user's display name, when known (OIDC only). */
  identity: string | undefined;
  /** Why the user was sent back to the login screen, if they were. */
  notice: string | undefined;
  loginWithOIDC: () => Promise<void>;
  loginWithToken: (token: string) => void;
  logout: () => Promise<void>;
  /** Completes the OIDC redirect; returns the path to go back to. */
  completeLogin: () => Promise<string>;
  completeSilentLogin: () => Promise<void>;
}

// The redirect's code/state can only be redeemed once, but React (StrictMode
// in dev, or a remount) may ask twice - share the one attempt.
let pendingCallback: Promise<User> | undefined;

const AuthContext = createContext<AuthState | undefined>(undefined);

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth outside AuthProvider');
  }
  return ctx;
}

function userManagerFor(config: AuthConfig): UserManager | undefined {
  if (!config.oidc) {
    return undefined;
  }
  const origin = window.location.origin;
  return new UserManager({
    authority: config.oidc.issuer,
    client_id: config.oidc.clientId,
    scope: config.oidc.scopes.join(' '),
    redirect_uri: origin + CALLBACK_PATH,
    silent_redirect_uri: origin + SILENT_PATH,
    post_logout_redirect_uri: origin + '/',
    response_type: 'code',
    automaticSilentRenew: true,
    // sessionStorage (the default) would log the user out of every new tab.
    userStore: new WebStorageStateStore({ store: window.localStorage }),
  });
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<AuthConfig>();
  const [configError, setConfigError] = useState<string>();
  const [staticToken, setStaticToken] = useState(() => localStorage.getItem(TOKEN_KEY) ?? undefined);
  const [user, setUser] = useState<User | null>(null);
  const [notice, setNotice] = useState<string>();

  useEffect(() => {
    api
      .authConfig()
      .then(setConfig)
      .catch(err => setConfigError(err instanceof Error ? err.message : String(err)));
  }, []);

  const manager = useMemo(() => (config ? userManagerFor(config) : undefined), [config]);

  useEffect(() => {
    if (!manager) {
      return;
    }
    void manager.getUser().then(u => setUser(u && !u.expired ? u : null));
    const onLoaded = (u: User) => setUser(u);
    const onUnloaded = () => setUser(null);
    manager.events.addUserLoaded(onLoaded);
    manager.events.addUserUnloaded(onUnloaded);
    manager.events.addAccessTokenExpired(onUnloaded);
    manager.events.addSilentRenewError(onUnloaded);
    return () => {
      manager.events.removeUserLoaded(onLoaded);
      manager.events.removeUserUnloaded(onUnloaded);
      manager.events.removeAccessTokenExpired(onUnloaded);
      manager.events.removeSilentRenewError(onUnloaded);
    };
  }, [manager]);

  const clearCredentials = useCallback(() => {
    if (localStorage.getItem(TOKEN_KEY) !== null || user) {
      setNotice('The server rejected your credentials - sign in again.');
    }
    localStorage.removeItem(TOKEN_KEY);
    setStaticToken(undefined);
    if (manager) {
      void manager.removeUser();
    }
    setUser(null);
  }, [manager, user]);

  const bearer = user && !user.expired ? user.access_token : staticToken;

  // Configured synchronously during render, so the very first request a
  // child component makes already carries the current credential.
  configureAuth(async () => bearer, clearCredentials);

  const state: AuthState = {
    config,
    configError,
    authenticated: Boolean(bearer),
    identity: user?.profile.preferred_username ?? user?.profile.email ?? user?.profile.name,
    notice,
    loginWithOIDC: async () => {
      setNotice(undefined);
      await manager?.signinRedirect({
        state: window.location.pathname + window.location.search,
      });
    },
    loginWithToken: token => {
      setNotice(undefined);
      localStorage.setItem(TOKEN_KEY, token);
      setStaticToken(token);
    },
    logout: async () => {
      const hadOIDC = Boolean(user);
      localStorage.removeItem(TOKEN_KEY);
      setStaticToken(undefined);
      await manager?.removeUser();
      setUser(null);
      if (hadOIDC && manager) {
        await manager.signoutRedirect().catch(() => undefined);
      }
    },
    completeLogin: async () => {
      if (!manager) {
        return '/';
      }
      pendingCallback ??= manager.signinRedirectCallback();
      const signedIn = await pendingCallback;
      setUser(signedIn);
      const back = typeof signedIn.state === 'string' ? signedIn.state : '/';
      // Only ever a same-origin path - never let state redirect off-site.
      return back.startsWith('/') && !back.startsWith('//') ? back : '/';
    },
    completeSilentLogin: async () => {
      await manager?.signinSilentCallback();
    },
  };

  return <AuthContext.Provider value={state}>{children}</AuthContext.Provider>;
}
