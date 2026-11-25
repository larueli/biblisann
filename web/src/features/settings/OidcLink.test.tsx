import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "../../i18n";
import { OidcLink } from "./OidcLink";

describe("OidcLink", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("starts linking using an authenticated POST and shows a refusal", async () => {
    let post = false;
    vi.stubGlobal("fetch", async (req: Request) => {
      if (req.method === "GET") return Response.json({ enabled: true, label: "Example" });
      post = req.method === "POST" && new URL(req.url).pathname === "/v1/auth/oidc/link";
      return Response.json({ detail: "link unavailable" }, { status: 403 });
    });
    render(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
          })
        }
      >
        <OidcLink />
      </QueryClientProvider>,
    );
    fireEvent.click(await screen.findByRole("button", { name: /Example/ }));
    await waitFor(() => expect(post).toBe(true));
    expect(await screen.findByRole("alert")).toBeTruthy();
  });
});
