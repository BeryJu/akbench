import { expect, test } from "@playwright/test";

// One walk through the whole UI, in the order an operator would: define a
// topology, define a test case against it, run the test case, then look at the
// numbers. Each step depends on the previous one having written to the API, so
// this is a real integration test of frontend, backend and CRD schema together.
//
// The control plane behind it is envtest, so nothing reconciles: a topology
// stays Pending and a run stays Pending. That is the line this test does not
// cross - the operator's own behaviour is covered by the envtest reconcile
// suite in internal/controller.

const topology = "e2e-topology";
const testCase = "e2e-login";

test("defines a topology, a test case, and starts a run", async ({ page }) => {
  await test.step("the seeded topology is visible", async () => {
    await page.goto("/topologies");
    // The Name column links to the topology; the Target column repeats the
    // same text (targetName defaults to the topology's name), so matching by
    // "cell" here is ambiguous - the link is the one unique locator.
    await expect(page.getByRole("link", { name: "seeded-topology" })).toBeVisible();
  });

  await test.step("create a topology", async () => {
    await page.getByRole("link", { name: "New topology" }).click();
    await page.locator('input[name="name"]').fill(topology);
    await page.locator('input[name="namespace"]').fill("ak-e2e");
    await page.locator('input[name="serverReplicas"]').fill("3");
    await page.locator('select[name="postgresMode"]').selectOption("CNPG");
    // The CNPG sub-form only exists once that mode is chosen.
    await expect(page.locator('input[name="cnpgInstances"]')).toBeVisible();
    await page.locator('input[name="cnpgInstances"]').fill("3");
    await page.getByRole("button", { name: "Create topology" }).click();

    await expect(page).toHaveURL(/\/topologies$/);
    // Same ambiguity as above: Name and Target columns both show the
    // topology's name when targetName is unset, so only the link is unique.
    await expect(page.getByRole("link", { name: topology, exact: true })).toBeVisible();
    await expect(page.getByRole("row", { name: new RegExp(topology) })).toContainText("CNPG");
  });

  await test.step("the topology round-trips into its edit form", async () => {
    await page.getByRole("link", { name: topology, exact: true }).click();
    await expect(page.locator('input[name="serverReplicas"]')).toHaveValue("3");
    await expect(page.locator('select[name="postgresMode"]')).toHaveValue("CNPG");
  });

  await test.step("create a test case against it", async () => {
    await page.goto("/testcases");
    await page.getByRole("link", { name: "New test case" }).click();
    await page.locator('input[name="name"]').fill(testCase);
    await page.locator('select[name="topology"]').selectOption(topology);
    await page.locator('input[name="vus"]').fill("16");
    await page.locator('input[name="iterations"]').fill("50");
    await page.getByRole("button", { name: "Create test case" }).click();

    await expect(page).toHaveURL(/\/testcases$/);
    const row = page.getByRole("row", { name: new RegExp(testCase) });
    await expect(row).toContainText("Quick");
    await expect(row).toContainText("16");
  });

  await test.step("burn profile hides the per-VU iteration count", async () => {
    await page.goto("/testcases/new");
    await page.locator('select[name="profile"]').selectOption("Burn");
    await expect(page.locator('input[name="iterations"]')).toHaveCount(0);
    await expect(page.locator('input[name="burnDuration"]')).toBeVisible();
  });

  await test.step("run it", async () => {
    await page.goto("/testcases");
    await page.locator(`button[name="run-${testCase}"]`).click();

    await expect(page).toHaveURL(new RegExp(`/testruns\\?testcase=${testCase}`));
    await expect(page.getByRole("cell", { name: new RegExp(`^${testCase}-`) })).toBeVisible();
  });

  await test.step("the run shows up in the test case's history", async () => {
    await page.goto(`/testcases/${testCase}`);
    await expect(page.getByRole("heading", { name: "Runs" })).toBeVisible();
    await expect(page.getByRole("cell", { name: new RegExp(`^${testCase}-`) })).toBeVisible();
  });

  await test.step("compare reads metrics for it", async () => {
    await page.goto("/compare");
    await page.locator(`input[name="testid-${testCase}"]`).check();
    await expect(page.getByRole("heading", { name: "Summary" })).toBeVisible();
    await expect(page.getByRole("cell", { name: /HTTP requests\/s/ })).toBeVisible();
    // The stub Prometheus answers 100 for everything not named login-b.
    await expect(page.getByRole("row", { name: /HTTP requests\/s/ })).toContainText("100");
  });
});

test("the home page counts what exists", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
  // "Topologies" also appears as the sidebar nav link and the stat card
  // label - the section heading is the one place it's unique.
  await expect(page.getByRole("heading", { name: "Topologies", level: 2 })).toBeVisible();
  await expect(page.getByRole("link", { name: "seeded-topology" })).toBeVisible();
});

test("clusters lists the management cluster", async ({ page }) => {
  await page.goto("/clusters");
  // Without exact:true this also matches the Role column's "Management" -
  // Playwright's text matching is case-insensitive by default.
  await expect(page.getByRole("cell", { name: "management", exact: true })).toBeVisible();
  await expect(page.getByRole("row", { name: /management/ })).toContainText("Management");
});
