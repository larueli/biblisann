-- name: LockOAuthClientRegistration :exec
SELECT pg_advisory_xact_lock(hashtext('biblisann.oauth.client.registration'));

-- name: LockAbandonedOAuthClient :one
-- Preserve active grants and a grace period for clients completing their first authorization.
SELECT c.id FROM oauth_clients c
WHERE c.created_at < now() - interval '24 hours'
  AND NOT EXISTS (SELECT 1 FROM oauth_codes t WHERE t.client_id = c.id AND t.expires_at > now())
  AND NOT EXISTS (SELECT 1 FROM oauth_refresh_tokens t WHERE t.client_id = c.id AND t.expires_at > now())
  AND NOT EXISTS (SELECT 1 FROM oauth_access_tokens t WHERE t.client_id = c.id AND t.expires_at > now())
ORDER BY c.created_at, c.id LIMIT 1 FOR UPDATE OF c SKIP LOCKED;

-- name: DeleteAbandonedOAuthClient :execrows
-- Recheck after locking the client, with a fresh statement snapshot: a concurrent token insert
-- could have committed since the candidate was selected.
DELETE FROM oauth_clients c WHERE c.id = $1
  AND c.created_at < now() - interval '24 hours'
  AND NOT EXISTS (SELECT 1 FROM oauth_codes t WHERE t.client_id = c.id AND t.expires_at > now())
  AND NOT EXISTS (SELECT 1 FROM oauth_refresh_tokens t WHERE t.client_id = c.id AND t.expires_at > now())
  AND NOT EXISTS (SELECT 1 FROM oauth_access_tokens t WHERE t.client_id = c.id AND t.expires_at > now());
