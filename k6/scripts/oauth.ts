import http from "k6/http";
import { check, fail } from "k6";
import { SharedArray } from "k6/data";
import type { Options, PerVUIterationsScenario } from "k6/options";

// Drives an OAuth2/OIDC authorization-code flow through an oidc-test-client
// relying party (github.com/BeryJu/oidc-test-client) sitting in front of
// authentik, reusing the same authentik flow-executor stage walk login.ts
// already proved out (identification -> password -> optional MFA), plus
// the extra request-chasing an OAuth authorization needs on top of a plain
// login. The mechanical sequence below - which fields/headers to read at
// each step, which hops are plain 302s vs. JSON "xak-flow-redirect"
// challenges, and that the flow-executor API needs the *same*
// `query=<...>` querystring re-attached to every request in the walk - was
// verified directly against authentik's own source
// (authentik/providers/oauth2/views/authorize.py, authentik/policies/views.py,
// authentik/flows/planner.py, authentik/flows/views/executor.py), not
// assumed or copied from anywhere else, since no OAuth-driving script
// existed anywhere in this project's history to crib from.
//
// The whole redirect-chasing mechanic (steps 1-5 below) was exercised
// end-to-end against a throwaway mock authentik+oidc-test-client, and later
// against a real live cluster, before this was considered done, not just
// reasoned about from source - this caught several real bugs a source read
// alone wouldn't have:
//   - k6 (0.57.0) has no global URL/WHATWG URL API (splitPathAndQuery/
//     resolveLocation below exist because of this).
//   - Every Location/"to" value authentik itself emits here is a relative
//     path (Django's request.get_full_path() never includes scheme/host),
//     not absolute, so each one needs resolving against BENCH_HOST/PORT
//     before it can be requested directly.
//   - The flow-executor API's own "advance to the next stage" mechanism
//     (authentik/flows/views/executor.py's to_stage_response(): a redirect
//     whose target equals the current request's own path is returned as a
//     bare, empty-bodied HTTP redirect rather than wrapped in a JSON
//     "xak-flow-redirect" challenge) means a client MUST let these requests
//     follow redirects normally, exactly like login.ts already does -
//     disabling redirects here (as an earlier version of this file did, to
//     make Location-header reading elsewhere in this file easier) turned
//     every such "advance stage" response into a false "Flow executor did
//     not return 200" failure, 100% reproducible even single-VU/
//     single-threaded, so this was never a load/timing artifact. See
//     walkFlow's own doc comment.
//   - default-provider-authorization-implicit-consent (the flow the OAuth2
//     provider's own `authorization_flow` names) is NOT stage-less despite
//     the name - confirmed live that it needs its own flow-executor walk
//     (an "ak-stage-consent" stage) exactly like the initial authentication
//     flow, not a direct 302 straight to redirect_uri.
//   - Under real concurrent load (multiple VUs authorizing at once), that
//     second (consent) flow-executor walk can occasionally 500 - root-caused
//     to a real, upstream authentik concurrency bug (not something this
//     script can work around): authentik/flows/planner.py's FlowPlan cache
//     key collapses to one shared key per flow for *all* anonymous sessions
//     (request.user.pk is None for every anonymous requester), with no
//     locking around the read-modify-write, so a plan built for one
//     anonymous session's request context can leak into a concurrent,
//     unrelated one. Left as a real, surfaced benchmark finding rather than
//     retried/hidden here - a transient 500 under concurrent authorization
//     load is exactly the kind of thing this benchmark exists to catch.
//
// One link in this chain is still taken on faith rather than verified live:
// oidc-test-client's own "/" handler is assumed to build the OAuth2
// authorize URL from its own OIDC_CLIENT_ID/OIDC_PROVIDER/OIDC_ROOT_URL and
// issue a redirect there (the standard shape for a minimal OIDC relying
// party) - confirm this against the real image first if the flow doesn't
// complete as expected.
//
// The full sequence, for reference:
//   0. GET oidc-test-client "/" -> 302 to authentik's own
//      /application/o/authorize/?client_id=...&redirect_uri=...&state=...
//      (built by oidc-test-client itself - never constructed here).
//   1. GET that authorize URL, unauthenticated -> 302 to
//      /if/flow/<authentication-flow-slug>/?<original params>&next=<...>
//      (authentik/policies/views.py's handle_no_permission()) - the slug
//      and exact next= value are config/session-dependent and must be read
//      from this Location header, not guessed.
//   2. Walk /api/v3/flows/executor/<slug>/ via walkFlow() exactly like
//      login.ts's own inline loop, but with ?query=<the querystring from
//      step 1's Location> re-attached to every GET/POST - this is what
//      makes the flow's own completion redirect back into the OAuth2
//      authorization instead of authentik's root page
//      (authentik/flows/views/executor.py's _flow_done()).
//   3. The flow's completing "xak-flow-redirect" JSON body's `to` field is
//      the same authorize URL from step 0/1, now with an authenticated
//      session.
//   4. GET that `to` URL -> either a direct 302 straight to
//      oidc-test-client's own redirect_uri with the real authorization
//      code, or (confirmed live: always, given oidc-test-client's own
//      prompt=consent) another /if/flow/.../ redirect for the provider's
//      own authorization_flow - walked via walkFlow() exactly like step 2,
//      looping in case that ever chains further.
//   5. GET that redirect_uri (oidc-test-client's own /auth/callback) with
//      redirects followed normally - it exchanges the code for tokens
//      server-side; a 200 here confirms the whole round trip completed.
interface User {
    username: string;
    email: string;
    password: string;
}

const host = __ENV.BENCH_HOST || "localhost";
const port = __ENV.BENCH_PORT || "9000";
const oauthHost = __ENV.OAUTH_TEST_CLIENT_HOST || "";
const oauthPort = __ENV.OAUTH_TEST_CLIENT_PORT || "9009";
const vus = Number(__ENV.VUS || 8);
const usersFile = __ENV.USERS_FILE || "./users.json";

const testid = __ENV.TEST_ID || "oauth";
const target = __ENV.TARGET || host;
const traceSampleRate = Number(__ENV.TRACE_SAMPLE_RATE || 0);

const profile = __ENV.PROFILE || "quick";
const iterations = Number(__ENV.ITERATIONS || 1000);
const quickDuration = __ENV.QUICK_DURATION || "150s";
const burnDuration = __ENV.BURN_DURATION || "8760h";
const burnIterations = 1000000000;

const profiles: Record<string, PerVUIterationsScenario> = {
    quick: {
        executor: "per-vu-iterations",
        vus: vus,
        iterations: iterations,
        maxDuration: quickDuration,
    },
    burn: {
        executor: "per-vu-iterations",
        vus: vus,
        iterations: burnIterations,
        maxDuration: burnDuration,
    },
};

const scenario = profiles[profile];
if (!scenario) {
    throw new Error(`Unknown PROFILE "${profile}", expected one of: ${Object.keys(profiles).join(", ")}`);
}

const users = new SharedArray<User>("users", () => JSON.parse(open(usersFile)) as User[]);
if (users.length === 0) {
    throw new Error(`No credentials found in ${usersFile}`);
}
if (!oauthHost) {
    throw new Error("OAUTH_TEST_CLIENT_HOST is required for the oauth-oidc template - does the template list the oidc-client dependency?");
}

export const options: Options = {
    scenarios: {
        [testid]: {
            ...scenario,
            tags: {
                testid: testid,
                target: target,
                profile: profile,
            },
        },
    },
};

function hex(bytes: number): string {
    let out = "";
    for (let i = 0; i < bytes; i++) {
        out += ((Math.random() * 256) | 0).toString(16).padStart(2, "0");
    }
    return out;
}

// authentik's flow-executor API needs the *same* query=<...> querystring on
// every request throughout one flow walk (see this file's own header
// comment, step 2) - dropping it after the first request is an easy
// mistake that silently loses the OAuth continuation.
function withQuery(url: string, query: string): string {
    return query ? `${url}?query=${encodeURIComponent(query)}` : url;
}

// splitPathAndQuery pulls the path and querystring out of a Location header
// that may be relative (authentik's own redirect()/reverse() typically
// emit these) or absolute - k6 has no global URL/WHATWG URL API (confirmed
// live: "ReferenceError: URL is not defined" on k6 0.57.0), so this is a
// small manual parser rather than relying on one.
function splitPathAndQuery(location: string): { pathname: string; search: string } {
    const withoutOrigin = location.replace(/^https?:\/\/[^/]+/, "");
    const qIndex = withoutOrigin.indexOf("?");
    if (qIndex === -1) {
        return { pathname: withoutOrigin, search: "" };
    }
    return { pathname: withoutOrigin.slice(0, qIndex), search: withoutOrigin.slice(qIndex + 1) };
}

// resolveLocation makes a Location/redirect-challenge "to" value absolute
// against base if it isn't already. Confirmed live against a mock server
// that this matters, not a defensive-only guess: authentik's own
// _flow_done() (authentik/flows/views/executor.py) builds its "to" value
// from next_param, which for an OAuth2 authorize redirect is
// request.get_full_path() (authentik/policies/views.py) - Django's
// get_full_path() never includes a scheme/host, only path+query, so the
// "to" field is relative and passing it straight to http.get fails with
// "unsupported protocol scheme".
function resolveLocation(base: string, location: string): string {
    if (/^https?:\/\//.test(location)) {
        return location;
    }
    return base.replace(/\/$/, "") + location;
}

// walkFlow drives one authentik flow-executor sequence (identification ->
// password -> optional MFA -> completion) to its "xak-flow-redirect" stage
// and returns that stage's "to" field, exactly like login.ts's own inline
// loop - factored out here because the OAuth2 authorization itself can
// require a *second*, independent flow-executor walk (see the "Step 4"
// comment below), not just the initial authentication one.
//
// Deliberately does NOT set `redirects: 0` on these requests (unlike the
// OAuth-level navigation calls below, which need to read raw Location
// headers) - confirmed live, via authentik's own source
// (authentik/flows/views/executor.py's to_stage_response(): a redirect
// whose target equals the current request's own path is returned as a bare
// HTTP redirect rather than wrapped in a JSON "xak-flow-redirect" challenge,
// which happens whenever a flow's plan has to be (re)initiated for this
// session) that the flow-executor API can legitimately answer a request
// with a same-URL redirect that a client is expected to just follow, not
// treat as a failure. `redirects: 0` here caused every such response to be
// misread as "Flow executor did not return 200" - reproduced deterministically
// with a single VU and zero concurrency, so this was never a load/timing
// issue. login.ts never hit this because it never set `redirects: 0` on its
// own equivalent loop.
function walkFlow(
    jar: http.CookieJar,
    headers: Record<string, string>,
    host: string,
    port: string,
    traceId: string,
    sampled: boolean,
    executorBase: string,
    innerQuery: string,
    user: User
): string {
    const execParams = { jar: jar, headers: headers };
    let res = http.get(withQuery(executorBase, innerQuery), execParams);
    let requests = 0;
    let previousComponent: string | undefined;
    while (true) {
        if (requests > 10) {
            fail(`OAuth flow made more than 10 queries without completing (last stage: ${previousComponent ?? "none"}).`);
        }
        check(res, {
            "status is 200": (r) => r.status === 200,
        });
        if (res.status !== 200) {
            fail("Flow executor did not return 200.");
        }

        const component = res.json("component") as string | undefined;
        if (component !== undefined && component === previousComponent) {
            fail(
                `Flow stage "${component}" was submitted but the flow did not advance - ` +
                    "likely a failed submission (e.g. a stale or wrong static token) being retried instead of erroring."
            );
        }
        previousComponent = component;

        if (component === "xak-flow-redirect") {
            const to = res.json("to") as string | undefined;
            if (!to) {
                fail("Flow completed without a redirect target.");
            }
            return to as string;
        }

        const payload: Record<string, string> = {};
        if (component === "ak-stage-identification") {
            payload.uid_field = user.username;
        } else if (component === "ak-stage-password") {
            payload.password = user.password;
        } else if (component === "ak-stage-authenticator-validate") {
            payload.code = "staticToken";
        } else if (component === "ak-stage-consent") {
            // Nothing to fill in - authentik's own consent stage frontend
            // just re-submits the component name to approve.
        } else {
            fail(`Unknown flow stage component: ${component}`);
        }
        payload.component = component;
        headers.traceparent = `00-${traceId}-${hex(8)}-0${sampled ? 1 : 0}`;
        res = http.post(withQuery(executorBase, innerQuery), JSON.stringify(payload), {
            jar: jar,
            headers: { ...headers, "Content-Type": "application/json" },
        });
        requests++;
    }
}

export default function (): void {
    const jar = new http.CookieJar();
    const traceId = hex(16);
    const sampled = Math.random() < traceSampleRate;
    if (sampled) {
        console.log(`sampled oauth testid=${testid} target=${target} traceID=${traceId}`);
    }
    const headers: Record<string, string> = {
        Accept: "*/*",
        "X-Benchmark-Test": testid,
        "X-Benchmark-Target": target,
        traceparent: `00-${traceId}-${hex(8)}-0${sampled ? 1 : 0}`,
    };
    // Only for the outer OAuth-level navigation hops below, where Location
    // headers must be read manually - never for a flow-executor walk itself
    // (see walkFlow's own doc comment for why).
    const navParams = { jar: jar, redirects: 0, headers: headers };

    // Step 0.
    let res = http.get(`http://${oauthHost}:${oauthPort}/`, navParams);
    if (res.status !== 302 && res.status !== 303) {
        fail(`oidc-test-client "/" did not redirect (status ${res.status}) - expected a redirect into authentik's own authorize endpoint.`);
    }
    const authorizeURLRaw = res.headers["Location"] as string | undefined;
    if (!authorizeURLRaw) {
        fail('oidc-test-client "/" redirect had no Location header.');
    }
    const authorizeURL = resolveLocation(`http://${oauthHost}:${oauthPort}`, authorizeURLRaw as string);

    // Step 1.
    res = http.get(authorizeURL, navParams);
    if (res.status !== 302 && res.status !== 303) {
        fail(`authorize endpoint did not redirect to a login flow (status ${res.status}).`);
    }
    const flowRedirect = res.headers["Location"] as string | undefined;
    if (!flowRedirect) {
        fail("authorize endpoint's redirect had no Location header.");
    }

    function flowSlugAndQuery(location: string): { slug: string; query: string } {
        const parsed = splitPathAndQuery(location);
        const match = parsed.pathname.match(/\/if\/flow\/([^/]+)\/?$/);
        if (!match) {
            fail(`could not extract a flow slug from ${location}`);
        }
        return { slug: (match as RegExpMatchArray)[1], query: parsed.search };
    }

    const user = users[(__VU - 1 + __ITER * vus) % users.length] as User;

    // Step 2/3: walk the authentication flow to completion.
    let { slug: flowSlug, query: innerQuery } = flowSlugAndQuery(flowRedirect as string);
    let executorBase = `http://${host}:${port}/api/v3/flows/executor/${flowSlug}/`;
    let redirectTo = walkFlow(jar, headers, host, port, traceId, sampled, executorBase, innerQuery, user);

    // Step 4. Re-visiting the (now-authenticated) authorize URL either
    // redirects straight to redirect_uri with a code, or - since
    // oidc-test-client requests prompt=consent - into a *second*,
    // independent flow-executor walk for the provider's own
    // authorization_flow (its consent stage), exactly like step 1/2 above.
    // Confirmed live (not assumed from the "-implicit-consent" flow name
    // alone) that this second walk is real: without it, re-authorizing
    // always redirected into `/if/flow/default-provider-authorization-implicit-consent/...`
    // rather than the OAuth2 redirect_uri.
    let redirectURL = resolveLocation(`http://${host}:${port}`, redirectTo);
    let callbackURLRaw: string | undefined;
    for (let hop = 0; hop < 3; hop++) {
        res = http.get(redirectURL, navParams);
        if (res.status !== 302 && res.status !== 303) {
            fail(`re-authorizing did not redirect (status ${res.status}).`);
        }
        const location = res.headers["Location"] as string | undefined;
        if (!location) {
            fail("re-authorizing redirect had no Location header.");
        }
        if ((location as string).includes("code=")) {
            callbackURLRaw = location;
            break;
        }
        const inner = flowSlugAndQuery(location as string);
        executorBase = `http://${host}:${port}/api/v3/flows/executor/${inner.slug}/`;
        redirectTo = walkFlow(jar, headers, host, port, traceId, sampled, executorBase, inner.query, user);
        redirectURL = resolveLocation(`http://${host}:${port}`, redirectTo);
    }
    if (!callbackURLRaw) {
        fail(`expected a redirect to redirect_uri with an authorization code, got ${redirectURL}`);
    }
    const callbackURL = resolveLocation(`http://${host}:${port}`, callbackURLRaw as string);

    // Step 5.
    res = http.get(callbackURL, { jar: jar, headers: headers });
    check(res, {
        "oauth flow completed": (r) => r.status === 200,
    });
}
