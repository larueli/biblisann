# 0017. Sharing: roles, groups and custom permission profiles

Date: 2026-11-23
Status: Accepted

## Context

Libraries are shared with users and groups, with built-in roles or custom permission profiles (spec section 6).

## Decision

- A grant is a built-in role (`read`, `edit`, `admin`) or a `custom` grant pointing to a **permission profile** defined in that library: a non-empty set of `library.read`, `instance.write`, `place.write`, `loan.write`, `list.write`, `library.share`, `library.admin`. `admin` implies every permission.
- Grants go to a user (`library_members`) or a group (`library_group_shares`). A user's effective permissions are the union of the direct grant and the grants of all their groups.
- Services call `RequirePerm(libraryID, permission)`. The older `Require(role)` maps `read`/`edit`/`admin` to `library.read`/`instance.write`/`library.admin`.
- No access at all answers 404; missing permission answers 403. The last direct administrator of a library cannot be demoted or removed.
- Groups are created by instance administrators; group membership is managed by them (OIDC group sync adds to this in Slice 3).
- Personal access tokens carry scopes (`read`, `write`) and may be limited to a list of libraries; a token never exceeds its owner's rights and cannot create tokens.

## Consequences

- Loans and lists follow the same permissions; personal lists, reading status and grades are per user and never shared.
