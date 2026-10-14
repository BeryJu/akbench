import http from "k6/http";
import type { Params, Response } from "k6/http";
import { check, fail } from "k6";
import { SharedArray } from "k6/data";
import type { Options, PerVUIterationsScenario } from "k6/options";

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
interface User {
    username: string;
    email: string;
    password: string;
}

const host = __ENV.BENCH_HOST || "localhost";
const port = __ENV.BENCH_PORT || "9000";
const spHost = __ENV.SAML_TEST_SP_HOST || "";
const spPort = __ENV.SAML_TEST_SP_PORT || "9009";
const vus = Number(__ENV.VUS || 8);
const usersFile = __ENV.USERS_FILE || "./users.json";

const testid = __ENV.TEST_ID || "saml";
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
if (!spHost) {
    throw new Error("SAML_TEST_SP_HOST is required for the saml template - does the template list the saml-client dependency?");
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

type FlowResult = { kind: "redirect"; to: string } | { kind: "autosubmit"; url: string; attrs: Record<string, string> };

// walkFlow drives one flow-executor sequence to its end, like login.ts's
// loop, re-attaching ?query=<the flow URL's querystring> to every request so
// the flow resumes what sent the user there. A flow ends either in an
// "xak-flow-redirect" or, for SAMLFlowFinalView, an "ak-stage-autosubmit".
//
// Redirects are followed here: the executor answers some requests with a
// bare same-URL redirect that a client is expected to follow (see oauth.ts).
function walkFlow(jar: http.CookieJar, trace: Trace, flow: FlowRef, user: User): FlowResult {
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
            return { kind: "redirect", to: res.json("to") as string };
        } else if (component === "ak-stage-autosubmit") {
            return {
                kind: "autosubmit",
                url: res.json("url") as string,
                attrs: res.json("attrs") as Record<string, string>,
            };
        } else if (component === "ak-stage-identification") {
            payload.uid_field = user.username;
        } else if (component === "ak-stage-password") {
            payload.password = user.password;
        } else if (component === "ak-stage-authenticator-validate") {
            payload.code = "staticToken";
        } else if (component === "ak-stage-consent") {
            // Approving is re-submitting the component name.
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

export default function (): void {
    const jar = new http.CookieJar();
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
        console.log(`sampled saml testid=${testid} target=${target} traceID=${trace.id}`);
    }
    nextSpan(trace);
    const navParams: Params = { jar: jar, redirects: 0, headers: trace.headers };
    const user = users[(__VU - 1 + __ITER * vus) % users.length] as User;

    // Step 0/1.
    const res = http.get(`http://${spHost}:${spPort}/`, navParams);
    const loginFlow = followToFlow(res, navParams, 'saml-test-sp "/"');

    // Step 2.
    let result = walkFlow(jar, trace, loginFlow, user);

    // Step 3. Normally one more hop (the authorization flow); looped in case
    // a policy chains further.
    for (let hop = 0; hop < 3 && result.kind === "redirect"; hop++) {
        nextSpan(trace);
        const sso = http.get(resolveURL(authentikBase, result.to), navParams);
        result = walkFlow(jar, trace, followToFlow(sso, navParams, "SAML SSO endpoint"), user);
    }
    if (result.kind !== "autosubmit") {
        fail(`expected the authorization flow to end in an ak-stage-autosubmit to the SP's ACS, got a redirect to ${result.to}`);
    }

    // Step 4.
    nextSpan(trace);
    const acs = http.post(result.url, result.attrs, { jar: jar, headers: trace.headers });
    check(acs, {
        "saml flow completed": (r) => r.status === 200 && r.json("saml-session") === true,
    });
}
