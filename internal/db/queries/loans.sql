-- name: CreateContact :one
INSERT INTO contacts (owner_id, name, email, notes) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListContacts :many
SELECT * FROM contacts WHERE owner_id = $1 ORDER BY name;

-- name: GetContact :one
SELECT * FROM contacts WHERE id = $1;

-- name: DeleteContact :execrows
DELETE FROM contacts WHERE id = $1 AND owner_id = $2;

-- name: CreateLoan :one
INSERT INTO loans (library_id, owner_id, direction, instance_id, contact_id, counterparty, title, lent_on, due_on, notes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING *;

-- name: GetLoan :one
SELECT * FROM loans WHERE id = $1;

-- name: ReturnLoan :one
UPDATE loans SET returned_on = $2 WHERE id = $1 AND returned_on IS NULL RETURNING *;

-- name: UpdateLoanDue :one
UPDATE loans SET due_on = $2, notes = $3 WHERE id = $1 RETURNING *;

-- name: ListLoansInLibraries :many
-- Loans of the given libraries (the caller already checked loan.write on each one).
SELECT l.*, c.name AS contact_name FROM loans l LEFT JOIN contacts c ON c.id = l.contact_id
WHERE l.library_id = ANY($1::uuid[]) AND ($2::boolean OR l.returned_on IS NULL)
ORDER BY l.returned_on IS NOT NULL, l.due_on NULLS LAST, l.created_at DESC LIMIT $3;

-- name: OpenLoansDueBy :many
-- Open loans whose due date is on or before the given day (reminder candidates).
SELECT l.*, c.name AS contact_name, u.email AS owner_email, u.locale AS owner_locale
FROM loans l JOIN users u ON u.id = l.owner_id LEFT JOIN contacts c ON c.id = l.contact_id
WHERE l.returned_on IS NULL AND l.due_on IS NOT NULL AND l.due_on <= $1;

-- name: LoanNotificationSent :one
SELECT EXISTS (SELECT 1 FROM loan_notifications WHERE loan_id = $1 AND kind = $2 AND bucket = $3);

-- name: MarkLoanNotified :exec
INSERT INTO loan_notifications (loan_id, kind, bucket) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- name: OpenLoansForFeed :many
SELECT l.id, l.title, l.direction, l.due_on, l.counterparty, c.name AS contact_name
FROM loans l LEFT JOIN contacts c ON c.id = l.contact_id
WHERE l.owner_id = $1 AND l.returned_on IS NULL AND l.due_on IS NOT NULL ORDER BY l.due_on;

-- name: CreateChannel :one
INSERT INTO notification_channels (user_id, kind, target, config) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListChannels :many
SELECT * FROM notification_channels WHERE user_id = $1 ORDER BY created_at;

-- name: ListEnabledChannels :many
SELECT * FROM notification_channels WHERE user_id = $1 AND enabled;

-- name: DeleteChannel :execrows
DELETE FROM notification_channels WHERE id = $1 AND user_id = $2;

-- name: SetChannelEnabled :exec
UPDATE notification_channels SET enabled = $3 WHERE id = $1 AND user_id = $2;

-- name: ListLoansInLibrary :many
SELECT l.*, e.isbn13 AS edition_isbn13 FROM loans l
LEFT JOIN instances i ON i.id = l.instance_id LEFT JOIN editions e ON e.id = i.edition_id
WHERE l.library_id = $1 AND l.owner_id = $2 ORDER BY l.created_at, l.id;

-- name: ListAllLoansOfOwner :many
SELECT * FROM loans WHERE owner_id = $1 ORDER BY created_at, id;
