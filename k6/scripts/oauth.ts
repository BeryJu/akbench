import http from "k6/http";
import { check, fail } from "k6";
import { authentikBase, benchOptions, expectRedirect, flowRef, followToFlow, iteration, loadUsers, loginStages, newTrace, params, resolveURL, userFor, walkFlow } from "./bench.ts";

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
//   - The flow-executor API answers some requests with a bare same-URL
//     redirect that must be followed - see bench.ts's walkFlow.
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
const oauthHost = __ENV.OAUTH_TEST_CLIENT_HOST || "";
const oauthPort = __ENV.OAUTH_TEST_CLIENT_PORT || "9009";
if (!oauthHost) {
    throw new Error("OAUTH_TEST_CLIENT_HOST is required for the oauth-oidc template - does the template list the oidc-client dependency?");
}

const users = loadUsers();

export const options = benchOptions();

export default iteration(() => {
    const trace = newTrace("oauth");
    const stages = loginStages(userFor(users));
    // Only for the OAuth-level navigation hops, where Location headers are
    // read by hand - never for a flow-executor walk itself.
    const nav = () => params(trace, { redirects: 0 });

    // Step 0/1.
    let res = http.get(`http://${oauthHost}:${oauthPort}/`, nav());
    let flow = followToFlow(trace, res, 'oidc-test-client "/"');

    // Step 2/3.
    let to = expectRedirect(flow, walkFlow(trace, flow, stages));

    // Step 4. Loop in case a policy chains further.
    let callbackURL: string | undefined;
    for (let hop = 0; hop < 3 && !callbackURL; hop++) {
        res = http.get(resolveURL(authentikBase, to), nav());
        if (res.status < 300 || res.status >= 400 || !res.headers["Location"]) {
            fail(`re-authorizing did not redirect (status ${res.status}).`);
        }
        const location = resolveURL(res.url, res.headers["Location"]);
        if (location.includes("code=")) {
            callbackURL = location;
            break;
        }
        flow = flowRef(location) ?? fail(`could not extract a flow slug from ${location}`);
        to = expectRedirect(flow, walkFlow(trace, flow, stages));
    }
    if (!callbackURL) {
        fail(`expected a redirect to redirect_uri with an authorization code, last redirect was ${to}`);
    }

    // Step 5.
    res = http.get(callbackURL, params(trace));
    return check(res, {
        "oauth flow completed": (r) => r.status === 200,
    });
});
