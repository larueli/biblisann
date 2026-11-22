import { expect, test } from "@playwright/test";

test("home page loads and shows the server version", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Biblisann", level: 1 })).toBeVisible();
  await expect(page.getByRole("status")).toContainText(/version|Version/);
});

test("exposes an installable manifest and the OpenAPI document", async ({ request }) => {
  const manifest = await request.get("/manifest.webmanifest");
  expect(manifest.ok()).toBeTruthy();
  expect((await manifest.json()).name).toBe("Biblisann");
  const spec = await request.get("/openapi.json");
  expect((await spec.json()).openapi).toMatch(/^3\.1/);
});
