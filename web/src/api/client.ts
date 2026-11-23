import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";

export type Schemas = components["schemas"];

// The CSRF token comes with the session; it must accompany every state-changing request.
let csrfToken = "";
export function setCsrfToken(token: string) {
  csrfToken = token;
}

const csrf: Middleware = {
  onRequest({ request }) {
    if (csrfToken && request.method !== "GET" && request.method !== "HEAD") {
      request.headers.set("X-CSRF-Token", csrfToken);
    }
    return request;
  },
};

// Types come from the OpenAPI spec (make generate). Never hand-write API types.
export const api = createClient<paths>({
  baseUrl: window.location.origin,
  credentials: "same-origin",
  // Resolve fetch at call time so tests (and polyfills) can replace it.
  fetch: (request) => globalThis.fetch(request),
});
api.use(csrf);

/** Error carrying the HTTP status, so callers can tell "offline" from "refused". */
export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export function problemMessage(error: unknown): string {
  if (error && typeof error === "object" && "detail" in error) {
    const detail = (error as { detail?: unknown }).detail;
    if (typeof detail === "string") return detail;
  }
  return "request failed";
}
