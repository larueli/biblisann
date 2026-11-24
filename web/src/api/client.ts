import createClient, { type Middleware } from "openapi-fetch";
import { requestStepUp, stepUpRequiredDetail } from "../features/auth/stepup";
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

// A sensitive action answers 403 "step_up_required" until the user re-authenticates. Ask once,
// then replay the original request (the CSRF header is already on the clone).
const replayable = new Map<string, Request>();
const stepUp: Middleware = {
  onRequest({ request, id }) {
    if (
      request.method !== "GET" &&
      request.method !== "HEAD" &&
      !request.url.includes("/v1/auth/step-up")
    ) {
      replayable.set(id, request.clone());
    }
    return request;
  },
  async onResponse({ response, id }) {
    const original = replayable.get(id);
    replayable.delete(id);
    if (!original || response.status !== 403) return undefined;
    const body = (await response
      .clone()
      .json()
      .catch(() => null)) as { detail?: unknown } | null;
    if (body?.detail !== stepUpRequiredDetail) return undefined;
    if (!(await requestStepUp())) return undefined;
    return globalThis.fetch(original);
  },
  onError({ id }) {
    replayable.delete(id);
  },
};
api.use(stepUp);

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
