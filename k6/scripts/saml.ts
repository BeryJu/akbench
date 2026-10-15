import http from "k6/http";
import { check, fail } from "k6";
import { authentikBase, benchOptions, followToFlow, iteration, loadUsers, loginStages, newTrace, params, resolveURL, userFor, walkFlow } from "./bench.ts";

// Drives an SP-initiated SAML login through a saml-test-sp service provider
// (github.com/BeryJu/saml-test-sp, crewjam/saml) sitting in front of
// authentik, reusing login.ts's flow-executor stage walk. The sequence was
// read off authentik's own source (authentik/providers/saml/views/sso.py,
// authentik/policies/views.py, authentik/flows/planner.py) and
// saml-test-sp/crewjam's samlsp middleware:
//   0. GET saml-test-sp "/" -> 302 to authentik's
//      /application/saml/<slug>/sso/binding/redirect/?SAMLRequest=...&RelayState=...
//      (the SP prefers the Redirect binding authentik's metadata advertises),
//      plus a request-tracking cookie on the SP.
//   1. GET that, unauthenticated -> 302 to /if/flow/<authentication-flow>/?...&next=<1>.
//   2. Walk the authentication flow (identification -> password -> optional
//      MFA) with ?query=<that querystring>, ending in a redirect back to <1>.
//   3. GET <1> again, now authenticated -> 302 to the provider's
//      authorization flow. Its plan ends in SAMLFlowFinalView, which for the
//      provider's sp_binding=post answers with an "ak-stage-autosubmit"
//      challenge: the SP's ACS URL plus SAMLResponse/RelayState.
//   4. POST those to the SP's /saml/acs (what the browser's auto-submitting
//      form does) -> 302 to "/" with the SP's session cookie -> the SP's JSON
//      session page.
const spHost = __ENV.SAML_TEST_SP_HOST || "";
const spPort = __ENV.SAML_TEST_SP_PORT || "9009";
if (!spHost) {
    throw new Error("SAML_TEST_SP_HOST is required for the saml template - does the template list the saml-client dependency?");
}

const users = loadUsers();

export const options = benchOptions();

export default iteration(() => {
    const trace = newTrace("saml");
    const stages = loginStages(userFor(users));

    // Step 0/1.
    const res = http.get(`http://${spHost}:${spPort}/`, params(trace, { redirects: 0 }));
    const loginFlow = followToFlow(trace, res, 'saml-test-sp "/"');

    // Step 2.
    let result = walkFlow(trace, loginFlow, stages);

    // Step 3. Normally one more hop (the authorization flow); looped in case
    // a policy chains further.
    for (let hop = 0; hop < 3 && result.kind === "redirect"; hop++) {
        const sso = http.get(resolveURL(authentikBase, result.to), params(trace, { redirects: 0 }));
        result = walkFlow(trace, followToFlow(trace, sso, "SAML SSO endpoint"), stages);
    }
    if (result.kind !== "autosubmit") {
        fail(`expected the authorization flow to end in an ak-stage-autosubmit to the SP's ACS, got ${JSON.stringify(result)}`);
    }

    // Step 4.
    const acs = http.post(result.url, result.attrs, params(trace));
    return check(acs, {
        "saml flow completed": (r) => r.status === 200 && r.json("saml-session") === true,
    });
});
