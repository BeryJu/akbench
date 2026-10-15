import http from "k6/http";
import { check, fail } from "k6";
import type { Stages } from "./bench.ts";
import { authentikBase, benchOptions, expectRedirect, followToFlow, iteration, newTrace, params, walkFlow } from "./bench.ts";

// Drives a federated login through an authentik SAML source pointed at
// saml-test-idp (github.com/BeryJu/saml-test-idp, a crewjam/saml IdP with
// built-in users). The sequence was read off authentik's own source
// (authentik/sources/saml/views.py, authentik/core/sources/flow_manager.py)
// and crewjam/saml's samlidp:
//   0. GET authentik /source/saml/<slug>/ -> 302 straight to the IdP's
//      /sso?SAMLRequest=...&RelayState=... (the source uses the Redirect
//      binding, so no pre-authentication flow runs).
//   1. The IdP has no session yet and renders its login form, which carries
//      the SAMLRequest along; POST it with user/password.
//   2. The IdP answers with an auto-submitting form posting SAMLResponse and
//      RelayState to authentik's /source/saml/<slug>/acs/; submit it ->
//      302 to /if/flow/<default-source-enrollment|authentication>/?...
//   3. Walk that flow with ?query=<that querystring> (user_write/user_login,
//      no input), then confirm the session via /api/v3/core/users/me/.
// The test credentials are one of saml-test-idp's built-in users, not the
// fixture users, so every VU logs in as the same person - see setup().
const sourceSlug = __ENV.SAML_IDP_SOURCE_SLUG || "";
const idpUsername = __ENV.SAML_IDP_USERNAME || "";
const idpPassword = __ENV.SAML_IDP_PASSWORD || "";
if (!sourceSlug || !idpUsername || !idpPassword) {
    throw new Error(
        "SAML_IDP_SOURCE_SLUG/USERNAME/PASSWORD are required for the saml-source template - does the template list the saml-idp dependency?"
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
    const trace = newTrace("saml-source");

    // Step 0. Redirects followed: authentik's one hop ends at the IdP's form.
    let res = http.get(`${authentikBase}/source/saml/${sourceSlug}/`, params(trace));
    if (res.status !== 200 || res.html().find('input[name="password"]').size() === 0) {
        fail(`expected saml-test-idp's login form, got status ${res.status} from ${res.url}`);
    }

    // Step 1.
    res = res.submitForm({
        formSelector: "form",
        fields: { user: idpUsername, password: idpPassword },
        params: params(trace),
    });
    if (res.status !== 200 || res.html().find('input[name="SAMLResponse"]').size() === 0) {
        fail(`saml-test-idp did not return a SAMLResponse form (status ${res.status}) - wrong credentials re-render the login form.`);
    }

    // Step 2.
    res = res.submitForm({ formSelector: "form", params: params(trace, { redirects: 0 }) });
    const flow = followToFlow(trace, res, "authentik SAML ACS");

    // Step 3.
    expectRedirect(flow, walkFlow(trace, flow, stages));
    const me = http.get(`${authentikBase}/api/v3/core/users/me/`, params(trace));
    return check(me, {
        "saml-source login completed": (r) => r.status === 200 && !!r.json("user.username"),
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
