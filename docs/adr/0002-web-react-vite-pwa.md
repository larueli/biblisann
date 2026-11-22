# 0002. Web: React + Vite + TypeScript SPA, PWA first

Date: 2026-11-22
Status: Accepted

## Context

One UI must serve desktop and Android (iOS maybe later). The maintainer is not a frontend specialist and relies on AI assistance, so ecosystem size and strong typing matter. The app sits behind a login: no SEO or SSR need.

## Decision

- React + Vite + TypeScript (strict) as an SPA served by the Go binary.
- Installable PWA: service worker, IndexedDB (Dexie) for the offline index and outbox, `BarcodeDetector` with a WASM fallback for scanning.
- API client generated from the OpenAPI spec so API drift is a compile error.
- A Capacitor wrapper is the escape hatch if scanning quality or push on iOS requires it. It reuses the same web code.

## Consequences

- No store distribution or "enter server URL" friction on Android.
- Scanning robustness is the main risk; Slice 1 starts with a spike on a real phone and records the outcome in a new ADR.
- iOS Web Push only works for installed PWAs (16.4+).
