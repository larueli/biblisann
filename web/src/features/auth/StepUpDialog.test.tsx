import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "../../i18n";
import { StepUpDialog } from "./StepUpDialog";
import { requestStepUp } from "./stepup";

const security = {
  mfa_available: true,
  mfa_enabled: false,
  reset_available: false,
  passkeys_available: false,
  has_password: true,
  sso_available: false,
};

describe("StepUpDialog", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("opens on request, confirms with the password and resolves the waiting request", async () => {
    vi.stubGlobal("fetch", async (req: Request) => {
      const url = new URL(req.url);
      if (url.pathname === "/v1/me/security") return Response.json(security);
      if (url.pathname === "/v1/auth/step-up")
        return Response.json({ until: new Date().toISOString() });
      return new Response(null, { status: 404 });
    });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <StepUpDialog />
      </QueryClientProvider>,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    let result: Promise<boolean> | undefined;
    act(() => {
      result = requestStepUp();
    });
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toBeTruthy();
    fireEvent.change(await screen.findByPlaceholderText(/password|mot de passe/i), {
      target: { value: "tulips in the garden" },
    });
    fireEvent.click(screen.getByRole("button", { name: /confirm|confirmer/i }));
    expect(await result).toBe(true);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("resolves false when closed with Escape", async () => {
    vi.stubGlobal("fetch", async () => Response.json(security));
    render(
      <QueryClientProvider client={new QueryClient()}>
        <StepUpDialog />
      </QueryClientProvider>,
    );
    let result: Promise<boolean> | undefined;
    act(() => {
      result = requestStepUp();
    });
    await screen.findByRole("dialog");
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    expect(await result).toBe(false);
  });
});
