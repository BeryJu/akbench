import http from "k6/http";
import { check, fail } from "k6";
import { SharedArray } from "k6/data";
import type { Options, PerVUIterationsScenario } from "k6/options";

// Mixed-usage test plan: each iteration picks one of several realistic
// authentik interactions (see `actions` below) instead of running the same
// login over and over. The pick is drawn from a seeded PRNG, not
// Math.random(), so a TestCase replays the exact same action sequence on
// every run and against every target it drives - SEED is fixed per TestCase
// by the operator (TestCase.Seed: spec.seed, or a hash of the TestCase's own
// name), which keeps runs of this template comparable with each other the
// same way login.ts's fixed per-VU work already does.
interface User {
    username: string;
    email: string;
    password: string;
}

const host = __ENV.BENCH_HOST || "localhost";
const port = __ENV.BENCH_PORT || "9000";
const vus = Number(__ENV.VUS || 8);
const usersFile = __ENV.USERS_FILE || "./users.json";
const flow = __ENV.FLOW || "default-authentication-flow";
const seed = Number(__ENV.SEED || 0) >>> 0;

const testid = __ENV.TEST_ID || "mixed-usage";
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

// mulberry32: small, fast, good-enough 32-bit PRNG - deterministic for a
// given seed, which is the whole point here.
function mulberry32(a: number): () => number {
    return function (): number {
        a = (a + 0x6d2b79f5) | 0;
        let t = Math.imul(a ^ (a >>> 15), 1 | a);
        t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
}

// One independent stream per VU, derived from the TestCase seed, so a VU's
// sequence doesn't depend on how fast the other VUs happen to run. Created
// lazily: __VU is only meaningful inside the VU's own default function.
let rng: (() => number) | undefined;
function random(): number {
    if (!rng) {
        rng = mulberry32((seed ^ Math.imul(__VU, 0x9e3779b1)) >>> 0);
    }
    return rng();
}

// Trace/span IDs stay on Math.random(): they only need to be unique, and
// keeping them off the seeded stream means toggling traceSampleRate never
// shifts the action sequence.
function hex(bytes: number): string {
    let out = "";
    for (let i = 0; i < bytes; i++) {
        out += ((Math.random() * 256) | 0).toString(16).padStart(2, "0");
    }
    return out;
}

interface Ctx {
    user: User;
    params: {
        jar: http.CookieJar;
        headers: Record<string, string>;
        tags: Record<string, string>;
    };
    newSpan: () => void;
}

const executorURL = `http://${host}:${port}/api/v3/flows/executor/${flow}/`;

// login walks the authentication flow exactly like login.ts, optionally
// submitting a wrong password instead - in which case it stops at the first
// password challenge that comes back with errors. Returns whether the flow
// ended where expected.
function login(ctx: Ctx, wrongPassword: boolean): boolean {
    let res = http.get(executorURL, ctx.params);
    let requests = 0;
    let previousComponent: string | undefined;
    while (true) {
        if (requests > 10) {
            fail(`Test made more than 10 queries without completing (last stage: ${previousComponent ?? "none"}).`);
        }
        check(res, {
            "status is 200": (r) => r.status === 200,
        });
        if (res.status !== 200) {
            fail("Endpoint did not return 200.");
        }

        const component = res.json("component") as string | undefined;
        if (wrongPassword && component === "ak-stage-password" && previousComponent === "ak-stage-password") {
            return res.json("response_errors") !== null && res.json("response_errors") !== undefined;
        }
        if (component !== undefined && component === previousComponent) {
            fail(
                `Flow stage "${component}" was submitted but the flow did not advance - ` +
                    "likely a failed submission (e.g. a stale or wrong static token) being retried instead of erroring."
            );
        }
        previousComponent = component;
        const payload: Record<string, string> = {};
        if (component === "ak-stage-identification") {
            payload.uid_field = ctx.user.username;
        } else if (component === "ak-stage-password") {
            payload.password = wrongPassword ? `${ctx.user.password}-wrong` : ctx.user.password;
        } else if (component === "ak-stage-authenticator-validate") {
            payload.code = "staticToken";
        } else if (component === "xak-flow-redirect") {
            return !wrongPassword;
        } else {
            console.log(`Unknown component type: ${component}`);
            return false;
        }

        payload.component = component;
        ctx.newSpan();
        res = http.post(executorURL, JSON.stringify(payload), ctx.params);
        requests++;
    }
}

// The pages an already-logged-in user's browser hits right after landing on
// the user interface - authenticated by the session cookie login() left in
// the jar.
const browsePaths = ["/api/v3/core/users/me/", "/api/v3/core/applications/"];

function browse(ctx: Ctx): boolean {
    let ok = true;
    for (const path of browsePaths) {
        ctx.newSpan();
        const res = http.get(`http://${host}:${port}${path}`, ctx.params);
        ok = check(res, { "api status is 200": (r) => r.status === 200 }) && ok;
    }
    return ok;
}

interface Action {
    name: string;
    weight: number;
    run: (ctx: Ctx) => boolean;
}

// The usage mix. Weights are relative, not percentages - edit them (or add
// actions) freely in the TestCaseTemplate; the seeded sequence stays stable
// for a given seed and mix.
const actions: Action[] = [
    { name: "login", weight: 45, run: (ctx) => login(ctx, false) },
    { name: "login-browse", weight: 30, run: (ctx) => login(ctx, false) && browse(ctx) },
    { name: "failed-login", weight: 15, run: (ctx) => login(ctx, true) },
    {
        name: "flow-page",
        weight: 10,
        run: (ctx) => {
            const res = http.get(`http://${host}:${port}/if/flow/${flow}/`, ctx.params);
            return check(res, { "flow page status is 200": (r) => r.status === 200 });
        },
    },
];
const totalWeight = actions.reduce((sum, a) => sum + a.weight, 0);

function pickAction(): Action {
    let roll = random() * totalWeight;
    for (const action of actions) {
        roll -= action.weight;
        if (roll < 0) {
            return action;
        }
    }
    return actions[actions.length - 1];
}

export default function (): void {
    const action = pickAction();
    const traceId = hex(16);
    const sampled = Math.random() < traceSampleRate;
    if (sampled) {
        console.log(`sampled ${action.name} testid=${testid} target=${target} traceID=${traceId}`);
    }
    const headers: Record<string, string> = {
        "Content-Type": "application/json",
        Accept: "*/*",
        "X-Benchmark-Test": testid,
        "X-Benchmark-Target": target,
        traceparent: "",
    };
    const ctx: Ctx = {
        // Walk the user list so concurrent VUs never authenticate as the same user.
        user: users[(__VU - 1 + __ITER * vus) % users.length] as User,
        // Tagged per action, so dashboards can split latency/errors by it.
        params: { jar: new http.CookieJar(), headers: headers, tags: { action: action.name } },
        newSpan: () => {
            headers.traceparent = `00-${traceId}-${hex(8)}-0${sampled ? 1 : 0}`;
        },
    };
    ctx.newSpan();
    const ok = action.run(ctx);
    check(ok, {
        [`${action.name} completed`]: (c) => c,
    });
}
