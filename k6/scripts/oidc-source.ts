import http from "k6/http";
import { check, fail } from "k6";
import type { Stages } from "./bench.ts";
import { authentikBase, benchOptions, expectRedirect, followToFlow, iteration, newTrace, params, walkFlow } from "./bench.ts";

// Drives a federated login through an authentik OAuth source pointed at dex
// (an OpenID provider with one static password user), the "log in with an
// external IdP" button. The sequence was read off authentik's own source
// (authentik/sources/oauth/views/{redirect,callback}.py,
// authentik/core/sources/flow_manager.py) and dex's password connector:
//   0. GET authentik /source/oauth/login/<slug>/ -> 302 to dex's /dex/auth,
//      which (with one connector) redirects to its local password form.
//   1. POST the form (login=<email>, password) -> 303 to dex's approval
//      endpoint, skipped by skipApprovalScreen -> 303 to authentik's
//      /source/oauth/callback/<slug>/?code=...&state=...
//   2. authentik exchanges the code with dex back-channel, then redirects to
//      /if/flow/<default-source-enrollment|authentication>/?code=...
//   3. Walk that flow with ?query=<that querystring> (user_write/user_login,
//      no input), then confirm the session via /api/v3/core/users/me/.
// The test credentials are dex's static user, not the fixture users, so every
// VU logs in as the same person - see setup().
const sourceSlug = __ENV.OIDC_IDP_SOURCE_SLUG || "";
const idpUsername = __ENV.OIDC_IDP_USERNAME || "";
const idpPassword = __ENV.OIDC_IDP_PASSWORD || "";
if (!sourceSlug || !idpUsername || !idpPassword) {
    throw new Error(
        "OIDC_IDP_SOURCE_SLUG/USERNAME/PASSWORD are required for the oidc-source template - does the template list the oidc-idp dependency?"
    );
}

export const options = benchOptions();

// The source already established who the user is, so the default source
// flows only have user_write/user_login stages left, neither of which needs
// input (user_login only challenges when remember-me is offered).
const stages: Stages = {
    "ak-stage-user-login": () => ({ remember_me: false }),
    "ak-stage-prompt": () =>
        fail(
            "The source flow prompted for input (likely the enrollment username) - " +
                "the source did not map a username from the IdP, or that username is already taken in authentik."
        ),
};

function login(): boolean {
    const trace = newTrace("oidc-source");

    // Step 0. Redirects followed: the hops into dex end at its login form.
    let res = http.get(`${authentikBase}/source/oauth/login/${sourceSlug}/`, params(trace));
    if (res.status !== 200 || res.html().find('input[name="password"]').size() === 0) {
        fail(`expected dex's password form, got status ${res.status} from ${res.url}`);
    }

    // Step 1/2.
    res = res.submitForm({
        formSelector: "form",
        fields: { login: idpUsername, password: idpPassword },
        params: params(trace, { redirects: 0 }),
    });
    const flow = followToFlow(trace, res, "dex login (a 200 here usually means dex rejected the credentials)");

    // Step 3.
    expectRedirect(flow, walkFlow(trace, flow, stages));
    const me = http.get(`${authentikBase}/api/v3/core/users/me/`, params(trace));
    return check(me, {
        "oidc-source login completed": (r) => r.status === 200 && !!r.json("user.username"),
    });
}

// Every VU logs in as the same IdP user, so the first login enrolls that
// user in authentik (default-source-enrollment) and every later one only
// authenticates (default-source-authentication). setup() does the
// enrollment once, before any VU starts: concurrent first logins would race
// to create the same user and connection, and the measured run would mix
// two different flows.
export function setup(): void {
    login();
}

export default iteration(login);
