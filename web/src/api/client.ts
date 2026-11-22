import createClient from "openapi-fetch";
import type { paths } from "./schema";

// Types come from the OpenAPI spec (make generate). Never hand-write API types.
export const api = createClient<paths>({
  baseUrl: window.location.origin,
  // Resolve fetch at call time so tests (and polyfills) can replace it.
  fetch: (request) => globalThis.fetch(request),
});
