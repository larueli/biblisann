-- name: CreateGroup :one
INSERT INTO groups (name, external_id) VALUES ($1, $2) RETURNING *;

-- name: ListGroups :many
SELECT * FROM groups ORDER BY name;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = $1;

-- name: DeleteGroup :exec
DELETE FROM groups WHERE id = $1;

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: RemoveGroupMember :exec
DELETE FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: ListGroupMembers :many
SELECT u.id, u.email, u.display_name FROM group_members gm JOIN users u ON u.id = gm.user_id
WHERE gm.group_id = $1 ORDER BY u.email;

-- name: CreateProfile :one
INSERT INTO permission_profiles (library_id, name, permissions) VALUES ($1, $2, $3) RETURNING *;

-- name: ListProfiles :many
SELECT * FROM permission_profiles WHERE library_id = $1 ORDER BY name;

-- name: GetProfile :one
SELECT * FROM permission_profiles WHERE id = $1;

-- name: UpdateProfile :one
UPDATE permission_profiles SET name = $2, permissions = $3 WHERE id = $1 RETURNING *;

-- name: DeleteProfile :exec
DELETE FROM permission_profiles WHERE id = $1;

-- name: ShareWithUser :exec
INSERT INTO library_members (library_id, user_id, role, profile_id) VALUES ($1, $2, $3, $4)
ON CONFLICT (library_id, user_id) DO UPDATE SET role = EXCLUDED.role, profile_id = EXCLUDED.profile_id;

-- name: UnshareUser :execrows
DELETE FROM library_members WHERE library_id = $1 AND user_id = $2;

-- name: ShareWithGroup :exec
INSERT INTO library_group_shares (library_id, group_id, role, profile_id) VALUES ($1, $2, $3, $4)
ON CONFLICT (library_id, group_id) DO UPDATE SET role = EXCLUDED.role, profile_id = EXCLUDED.profile_id;

-- name: UnshareGroup :execrows
DELETE FROM library_group_shares WHERE library_id = $1 AND group_id = $2;

-- name: ListLibraryMembers :many
SELECT m.user_id, u.email, u.display_name, m.role, m.profile_id FROM library_members m JOIN users u ON u.id = m.user_id
WHERE m.library_id = $1 ORDER BY u.email;

-- name: ListLibraryGroupShares :many
SELECT s.group_id, g.name, s.role, s.profile_id FROM library_group_shares s JOIN groups g ON g.id = s.group_id
WHERE s.library_id = $1 ORDER BY g.name;

-- name: CountLibraryAdmins :one
SELECT count(*) FROM library_members WHERE library_id = $1 AND role = 'admin';
