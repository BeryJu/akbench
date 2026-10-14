import http from "k6/http";
import type { Params, Response } from "k6/http";
import { check, fail } from "k6";
import type { Options, PerVUIterationsScenario } from "k6/options";

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
const host = __ENV.BENCH_HOST || "localhost";
const port = __ENV.BENCH_PORT || "9000";
const sourceSlug = __ENV.SAML_IDP_SOURCE_SLUG || "";
const idpUsername = __ENV.SAML_IDP_USERNAME || "";
const idpPassword = __ENV.SAML_IDP_PASSWORD || "";
const vus = Number(__ENV.VUS || 8);

const testid = __ENV.TEST_ID || "saml-source";
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

if (!sourceSlug || !idpUsername || !idpPassword) {
    throw new Error(
        "SAML_IDP_SOURCE_SLUG/USERNAME/PASSWORD are required for the saml-source template - does the template list the saml-idp dependency?"
    );
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

const authentikBase = `http://${host}:${port}`;

function hex(bytes: number): string {
    let out = "";
    for (let i = 0; i < bytes; i++) {
        out += ((Math.random() * 256) | 0).toString(16).padStart(2, "0");
    }
    return out;
}

interface Trace {
    id: string;
    sampled: boolean;
    headers: Record<string, string>;
}

// nextSpan gives the following request its own span ID within the
// iteration's trace.
function nextSpan(trace: Trace): void {
    trace.headers.traceparent = `00-${trace.id}-${hex(8)}-0${trace.sampled ? 1 : 0}`;
}

// resolveURL makes a Location header absolute against the URL that returned
// it. k6 has no WHATWG URL API, and authentik's own redirects are relative.
function resolveURL(base: string, location: string): string {
    if (/^https?:\/\//.test(location)) {
        return location;
    }
    const origin = (base.match(/^https?:\/\/[^/]+/) as RegExpMatchArray)[0];
    if (location.startsWith("/")) {
        return origin + location;
    }
    const path = base.slice(origin.length).split("?")[0];
    return origin + path.slice(0, path.lastIndexOf("/") + 1) + location;
}

interface FlowRef {
    slug: string;
    query: string;
}

function flowRef(url: string): FlowRef | undefined {
    const match = url.match(/\/if\/flow\/([^/?#]+)\/?(?:\?([^#]*))?/);
    if (!match) {
        return undefined;
    }
    return { slug: match[1], query: match[2] || "" };
}

// followToFlow chases plain redirects by hand (requests made with
// redirects: 0) until one points at an authentik flow interface, and returns
// that flow's slug and querystring instead of loading its HTML shell.
function followToFlow(res: Response, params: Params, step: string): FlowRef {
    for (let hop = 0; hop < 10; hop++) {
        if (res.status < 300 || res.status >= 400) {
            fail(`${step}: expected a redirect, got status ${res.status} from ${res.url}`);
        }
        const location = res.headers["Location"];
        if (!location) {
            fail(`${step}: redirect from ${res.url} had no Location header`);
        }
        const next = resolveURL(res.url, location);
        const flow = flowRef(next);
        if (flow) {
            return flow;
        }
        res = http.get(next, params);
    }
    fail(`${step}: more than 10 redirects without reaching an authentik flow`);
}

// walkFlow drives one source authentication/enrollment flow to its end, like
// login.ts's loop, re-attaching ?query=<the flow URL's querystring> to every
// request. The source already established who the user is, so the default
// flows only have user_write/user_login stages left, neither of which needs
// input (user_login only challenges when remember-me is offered).
//
// Redirects are followed here: the executor answers some requests with a
// bare same-URL redirect that a client is expected to follow (see oauth.ts).
function walkFlow(jar: http.CookieJar, trace: Trace, flow: FlowRef): string {
    const url = `${authentikBase}/api/v3/flows/executor/${flow.slug}/` + (flow.query ? `?query=${encodeURIComponent(flow.query)}` : "");
    nextSpan(trace);
    let res = http.get(url, { jar: jar, headers: trace.headers });
    let previousComponent: string | undefined;
    for (let requests = 0; requests <= 10; requests++) {
        check(res, {
            "status is 200": (r) => r.status === 200,
        });
        if (res.status !== 200) {
            fail(`Flow executor for ${flow.slug} returned ${res.status}.`);
        }
        const component = res.json("component") as string | undefined;
        if (component !== undefined && component === previousComponent) {
            fail(`Flow stage "${component}" was submitted but the flow did not advance.`);
        }
        previousComponent = component;

        const payload: Record<string, unknown> = { component: component };
        if (component === "xak-flow-redirect") {
            return res.json("to") as string;
        } else if (component === "ak-stage-prompt") {
            fail(
                `${flow.slug} prompted for input (likely the enrollment username) - ` +
                    "the source did not map a username from the IdP, or that username is already taken in authentik."
            );
        } else if (component === "ak-stage-user-login") {
            payload.remember_me = false;
        } else {
            fail(`Unknown flow stage component in ${flow.slug}: ${component}`);
        }
        nextSpan(trace);
        res = http.post(url, JSON.stringify(payload), {
            jar: jar,
            headers: { ...trace.headers, "Content-Type": "application/json" },
        });
    }
    fail(`Flow ${flow.slug} made more than 10 queries without completing (last stage: ${previousComponent ?? "none"}).`);
}

function login(): void {
    const jar = new http.CookieJar();
    const trace = newTrace();
    const navParams: Params = { jar: jar, redirects: 0, headers: trace.headers };

    // Step 0. Redirects followed: authentik's one hop ends at the IdP's form.
    let res = http.get(`${authentikBase}/source/saml/${sourceSlug}/`, { jar: jar, headers: trace.headers });
    if (res.status !== 200 || res.html().find('input[name="password"]').size() === 0) {
        fail(`expected saml-test-idp's login form, got status ${res.status} from ${res.url}`);
    }

    // Step 1.
    nextSpan(trace);
    res = res.submitForm({
        formSelector: "form",
        fields: { user: idpUsername, password: idpPassword },
        params: { jar: jar, headers: trace.headers },
    });
    if (res.status !== 200 || res.html().find('input[name="SAMLResponse"]').size() === 0) {
        fail(`saml-test-idp did not return a SAMLResponse form (status ${res.status}) - wrong credentials re-render the login form.`);
    }

    // Step 2.
    nextSpan(trace);
    res = res.submitForm({ formSelector: "form", params: navParams });
    const flow = followToFlow(res, navParams, "authentik SAML ACS");

    // Step 3.
    finishSourceLogin(jar, trace, flow);
}

function newTrace(): Trace {
    const trace: Trace = {
        id: hex(16),
        sampled: Math.random() < traceSampleRate,
        headers: {
            Accept: "*/*",
            "X-Benchmark-Test": testid,
            "X-Benchmark-Target": target,
        },
    };
    if (trace.sampled) {
        console.log(`sampled saml-source testid=${testid} target=${target} traceID=${trace.id}`);
    }
    nextSpan(trace);
    return trace;
}

// finishSourceLogin walks the source's authentication or enrollment flow and
// confirms the session is now logged in.
function finishSourceLogin(jar: http.CookieJar, trace: Trace, flow: FlowRef): void {
    walkFlow(jar, trace, flow);
    nextSpan(trace);
    const me = http.get(`${authentikBase}/api/v3/core/users/me/`, { jar: jar, headers: trace.headers });
    check(me, {
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

export default function (): void {
    login();
}
