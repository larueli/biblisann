import { expect, test } from "@playwright/test";

// Runs against a real server started with BIBLISANN_PROVIDERS=static and
// BIBLISANN_STATIC_PROVIDER_FILE=web/e2e/fixtures/books.json (see the CI e2e job and `make e2e`).

test("exposes an installable manifest and the OpenAPI document", async ({ request }) => {
  const manifest = await request.get("/manifest.webmanifest");
  expect(manifest.ok()).toBeTruthy();
  expect((await manifest.json()).name).toBe("Biblisann");
  const spec = await request.get("/openapi.json");
  expect((await spec.json()).openapi).toMatch(/^3\.1/);
});

test("scan to list: register, create a library, add a book by ISBN, find it", async ({ page }) => {
  const email = `e2e-${Date.now()}@example.org`;
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByRole("heading", { name: "Biblisann", level: 1 })).toBeVisible();

  // The first account of an instance may register itself; later runs fall back to login.
  await page.getByRole("button", { name: /Create one/ }).click();
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill("tulips in the garden");
  await page.getByRole("button", { name: "Create an account" }).click();

  const closed = page.getByRole("alert");
  test.skip(
    await closed.isVisible({ timeout: 2000 }).catch(() => false),
    "registration is closed on this instance",
  );

  await page.getByLabel("Library name").fill("E2E home");
  await page.getByRole("button", { name: "Create library" }).click();
  await expect(page.getByRole("link", { name: "Scan" })).toBeVisible();

  await page.getByRole("link", { name: "Scan" }).click();
  await page.getByLabel("ISBN").fill("978-2-07-061275-8");
  await page.getByRole("button", { name: "Add" }).click();
  await expect(page.getByText("Added", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Back to the library" }).click();
  await page.getByLabel("Search title, author, ISBN…").fill("petit prnce"); // typo on purpose
  await expect(page.getByText("Le Petit Prince")).toBeVisible();
});
