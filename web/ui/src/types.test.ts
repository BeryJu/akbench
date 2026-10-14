import { TestCaseSpec, testCaseBaseline } from "./types";

import { describe, expect, it } from "vitest";

const spec = (patch: Partial<TestCaseSpec>): TestCaseSpec => ({
    profile: "Burn",
    vus: 1,
    ...patch,
});

describe("testCaseBaseline", () => {
    const deploymentRefs = [{ name: "a" }, { name: "b" }];

    it("uses baselineRef while it names one of the deployments", () => {
        expect(testCaseBaseline(spec({ deploymentRefs, baselineRef: { name: "b" } }))).toBe("b");
    });

    it("falls back to the first deployment when unset or stale", () => {
        expect(testCaseBaseline(spec({ deploymentRefs }))).toBe("a");
        expect(testCaseBaseline(spec({ deploymentRefs, baselineRef: { name: "gone" } }))).toBe("a");
    });

    it("reads the deprecated singular deploymentRef", () => {
        expect(testCaseBaseline(spec({ deploymentRef: { name: "old" } }))).toBe("old");
    });

    it("is undefined without deployments", () => {
        expect(testCaseBaseline(spec({}))).toBeUndefined();
    });
});
