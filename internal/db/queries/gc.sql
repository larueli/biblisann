-- name: GCPurgeSoftDeletedInstances :execrows
DELETE FROM instances WHERE deleted_at IS NOT NULL AND deleted_at < now() - make_interval(days => $1::int);

-- name: GCPurgeUnusedEditions :execrows
-- Editions nobody owns, listed or reads, long after their creation.
DELETE FROM editions e
WHERE e.created_at < now() - make_interval(days => $1::int)
  AND NOT EXISTS (SELECT 1 FROM instances i WHERE i.edition_id = e.id)
  AND NOT EXISTS (SELECT 1 FROM list_items li WHERE li.edition_id = e.id)
  AND NOT EXISTS (SELECT 1 FROM reading_states rs WHERE rs.work_id = e.work_id)
  AND NOT EXISTS (SELECT 1 FROM grades g WHERE g.work_id = e.work_id);

-- name: GCPurgeEmptyWorks :execrows
DELETE FROM works w WHERE NOT EXISTS (SELECT 1 FROM editions e WHERE e.work_id = w.id);

-- name: GCUnreferencedCovers :many
-- Covers no edition uses, past a grace period (a cover is stored just before its edition).
SELECT c.id, c.sha256 FROM covers c
WHERE c.created_at < now() - make_interval(hours => $1::int)
  AND NOT EXISTS (SELECT 1 FROM editions e WHERE e.cover_id = c.id);

-- name: GCDeleteCover :exec
DELETE FROM covers WHERE id = $1;

-- name: GCCompactSupersededChanges :execrows
-- A change is redundant once a later change exists for the same entity: the client reading from an
-- older cursor still sees the later one.
DELETE FROM changes c
WHERE c.created_at < now() - make_interval(days => $1::int)
  AND EXISTS (SELECT 1 FROM changes n WHERE n.library_id = c.library_id AND n.entity_id = c.entity_id AND n.seq > c.seq);

-- name: GCPruneOldTombstones :many
-- Last-known deletions older than the retention: forgotten, and the horizon moves up.
WITH gone AS (
    DELETE FROM changes c
    WHERE c.op = 'delete' AND c.created_at < now() - make_interval(days => $1::int)
    RETURNING c.library_id, c.seq
)
SELECT library_id, max(seq)::bigint AS seq FROM gone GROUP BY library_id;

-- name: GCSetPrunedThrough :exec
UPDATE libraries SET changes_pruned_through = greatest(changes_pruned_through, $2) WHERE id = $1;

-- name: GetPrunedThrough :one
SELECT changes_pruned_through FROM libraries WHERE id = $1;

-- name: GCExpireMisc :one
WITH a AS (DELETE FROM provider_cache WHERE fetched_at < now() - make_interval(days => $1::int) RETURNING 1),
     b AS (DELETE FROM mfa_challenges WHERE expires_at < now() RETURNING 1),
     c AS (DELETE FROM password_resets WHERE expires_at < now() - interval '1 day' OR used_at < now() - interval '1 day' RETURNING 1),
     d AS (DELETE FROM oidc_flows WHERE expires_at < now() RETURNING 1),
     e AS (DELETE FROM webauthn_sessions WHERE expires_at < now() RETURNING 1),
     f AS (DELETE FROM oauth_codes WHERE expires_at < now() RETURNING 1),
     g AS (DELETE FROM oauth_refresh_tokens WHERE expires_at < now() RETURNING 1),
     h AS (DELETE FROM oauth_access_tokens WHERE expires_at < now() RETURNING 1),
     i AS (DELETE FROM access_tokens WHERE expires_at IS NOT NULL AND expires_at < now() - interval '30 days' RETURNING 1),
     j AS (DELETE FROM loan_notifications n USING loans l WHERE n.loan_id = l.id AND l.returned_on IS NOT NULL AND l.returned_on < current_date - 90 RETURNING 1),
     k AS (DELETE FROM library_invitations WHERE expires_at < now() RETURNING 1)
SELECT ((SELECT count(*) FROM a) + (SELECT count(*) FROM b) + (SELECT count(*) FROM c) + (SELECT count(*) FROM d) + (SELECT count(*) FROM e)
      + (SELECT count(*) FROM f) + (SELECT count(*) FROM g) + (SELECT count(*) FROM h) + (SELECT count(*) FROM i) + (SELECT count(*) FROM j) + (SELECT count(*) FROM k))::bigint;
