import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import i18n from "../../i18n";
import { HomePage } from "./HomePage";

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <HomePage />
    </QueryClientProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("HomePage", () => {
  it("shows the server version in the current language", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ version: "1.2.3" })),
    );
    await i18n.changeLanguage("fr");
    renderPage();
    expect(await screen.findByText("Version du serveur : 1.2.3")).toBeInTheDocument();
    await i18n.changeLanguage("en");
    expect(await screen.findByText("Server version: 1.2.3")).toBeInTheDocument();
  });

  it("shows an error when the server is unreachable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("network");
      }),
    );
    await i18n.changeLanguage("en");
    renderPage();
    expect(await screen.findByText("The server cannot be reached.")).toBeInTheDocument();
  });
});
