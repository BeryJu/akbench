import { benchOptions, expectRedirect, flow, iteration, loadUsers, loginStages, newTrace, userFor, walkFlow } from "./bench.ts";

// Walks the authentik flow executor through a username/password (+ optional
// static-token MFA) login. See bench.ts for the env contract and profiles.
const users = loadUsers();

export const options = benchOptions();

export default iteration(() => {
    const ref = { slug: flow };
    expectRedirect(ref, walkFlow(newTrace("login"), ref, loginStages(userFor(users))));
    return true;
});
