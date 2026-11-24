// Pacing against the server's request quota. The server announces it with the IETF RateLimit
// header fields (draft-ietf-httpapi-ratelimit-headers):
//   RateLimit-Policy: "default";q=600;w=60   (q requests' worth of tokens per w seconds)
//   RateLimit:        "default";r=12;t=30    (r tokens left, full again in t seconds)
// and, on a 429, Retry-After (seconds). A scan weighs SCAN_COST tokens.

export const SCAN_COST = 5;

function field(header: string | null, name: string): number | null {
  const m = header ? new RegExp(`(?:^|;)\\s*${name}=(\\d+)`).exec(header) : null;
  return m?.[1] ? Number(m[1]) : null;
}

/**
 * How long to wait (ms) before the next call so that it fits the quota. 0 when there is room or
 * the server announced no quota. A 429 waits for Retry-After.
 */
export function waitBeforeNext(response: Response, cost = SCAN_COST): number {
  if (response.status === 429) {
    const retry = Number(response.headers.get("Retry-After"));
    return (Number.isFinite(retry) && retry > 0 ? retry : 5) * 1000;
  }
  const left = field(response.headers.get("RateLimit"), "r");
  const q = field(response.headers.get("RateLimit-Policy"), "q");
  const w = field(response.headers.get("RateLimit-Policy"), "w");
  if (left === null || q === null || w === null || q === 0 || left >= cost) return 0;
  return Math.ceil(((cost - left) * w * 1000) / q);
}
