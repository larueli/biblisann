# 0013. UI language and metadata lookup scope

Date: 2026-11-22
Status: Accepted

## Context

The UI is multilingual (`en`, `fr`). Metadata lookups send an ISBN to third parties, which leaks what a user owns (spec section 4), and the wanted metadata language differs by library and sometimes by request.

## Decision

- **UI language**: detected from the browser (`navigator.languages`), with `en` as the fallback and the source language of all strings. A user preference overrides detection. The server never guesses from IP.
- **Metadata lookup settings** are layered, the most specific wins: request parameter, then library setting, then user setting, then instance default (env/config). A setting contains: the ordered list of enabled providers, and the preferred languages. An instance admin can disable a provider globally; lower layers cannot re-enable it.
- The request-level override is part of the API from the first lookup endpoint (`providers` and `languages` parameters), not retrofitted later.

## Consequences

- Provider resolution is one function with a single precedence rule, covered by table-driven tests when the provider interface lands (Slice 1).
- The data model needs a `settings` JSON column on libraries and users (Slice 1 schema, no Slice 0 impact).
