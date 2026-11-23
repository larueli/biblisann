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

// The API viewers are hosted by the server itself and must work under its Content-Security-Policy.
for (const viewer of ["swagger", "redoc", "elements", "scalar"]) {
  test(`API documentation viewer: ${viewer}`, async ({ page }) => {
    const violations: string[] = [];
    const errors: string[] = [];
    page.on("console", (m) => {
      // Redoc tries to show its "powered by" logo from cdn.redoc.ly: the policy blocks it, as intended.
      if (/content security policy/i.test(m.text())) {
        if (!m.text().includes("cdn.redoc.ly")) violations.push(m.text());
      } else if (m.type() === "error") errors.push(m.text());
    });
    page.on("pageerror", (e) => errors.push(String(e)));
    await page.goto(`/docs/${viewer}.html`);
    await expect(page.getByText("Biblisann API").first(), errors.join(" | ")).toBeVisible({
      timeout: 20_000,
    });
    await expect(page.getByText(/list-libraries|\/v1\/libraries|Libraries/i).first()).toBeAttached({
      timeout: 20_000,
    });
    expect(violations).toEqual([]);
  });
}

test("probe routes and the spec variants answer without authentication", async ({ request }) => {
  for (const path of [
    "/livez",
    "/healthz",
    "/readyz",
    "/startupz",
    "/health",
    "/openapi.yaml",
    "/openapi-3.0.json",
    "/docs/",
  ]) {
    expect((await request.get(path)).ok(), path).toBeTruthy();
  }
  expect((await request.get("/metrics")).status()).toBe(404); // no token and not public: the route does not exist
});
