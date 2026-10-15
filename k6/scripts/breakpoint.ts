import type { RampingVUsScenario } from "k6/options";
import { benchOptions, expectRedirect, flow, iteration, loadUsers, loginStages, newTrace, userFor, vus, walkFlow } from "./bench.ts";

// Breakpoint test: instead of a fixed load, add VUS virtual users every
// stepDuration until authentik collapses, then abort. The VU count at the
// abort is the breaking point. Run it as a Quick TestRun; ITERATIONS and
// *_DURATION are ignored - the step schedule below is the whole load shape.
// Edit these two constants in the template to change the pace.
const stepDuration = "2m";
const maxSteps = 100;
const maxVUs = vus * maxSteps;
// "Collapsed" means: more than 10% of requests, checks or iterations failing,
// or p95 latency above 10s. delayAbortEval keeps a cold start from aborting
// early.
const abort = { abortOnFail: true, delayAbortEval: "1m" };

const scenario: RampingVUsScenario = {
    executor: "ramping-vus",
    startVUs: 0,
    // Ramp to the next level over 10s, then hold it for the rest of the step.
    stages: Array.from({ length: maxSteps }, (_, i) => [
        { duration: "10s", target: vus * (i + 1) },
        { duration: stepDuration, target: vus * (i + 1) },
    ]).flat(),
    gracefulRampDown: "0s",
};

const users = loadUsers();

export const options = benchOptions(scenario, {
    http_req_failed: [{ threshold: "rate<0.1", ...abort }],
    checks: [{ threshold: "rate>0.9", ...abort }],
    http_req_duration: [{ threshold: "p(95)<10000", ...abort }],
    iteration_failed: [{ threshold: "rate<0.1", ...abort }],
});

export default iteration(() => {
    const ref = { slug: flow };
    expectRedirect(ref, walkFlow(newTrace("login"), ref, loginStages(userFor(users, maxVUs))));
    return true;
});
