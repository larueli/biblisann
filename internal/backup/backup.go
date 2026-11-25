// Package backup writes and restores a logical, versioned backup: one tar archive holding the
// database content as NDJSON (one JSON object per row) plus the cover files.
//
// Rows are exported with to_jsonb and restored with jsonb_populate_record, so a backup taken on
// an older schema restores on a newer one: columns added later take their defaults and columns
// that no longer exist are ignored. Database-level disaster recovery (pg_dump, CNPG/barman) is
// documented separately; this format is for moving between versions and storage layouts.
//
// A backup contains password hashes and access-token hashes: protect it like the database.
package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/secretbox"
	"github.com/larueli/biblisann/internal/storage"
)

// Format identifies the archive; Version is bumped for incompatible layout changes.
const (
	Format  = "biblisann-backup"
	Version = 2

	dataFile    = "data.ndjson"
	coverPrefix = "covers/"
	maxLine     = 16 << 20
)

// tables lists what is backed up, in foreign-key order. Derived or ephemeral tables (sessions,
// caches, idempotency keys, change log, notification marks, River queues) are not included: the
// change log is rebuilt on restore so offline clients resynchronize.
var tables = []struct{ name, order string }{
	{"users", "id"}, {"groups", "id"}, {"group_members", "group_id, user_id"}, {"libraries", "id"},
	{"user_totp", "user_id"}, {"recovery_codes", "user_id, code_hash"}, {"identities", "issuer, subject"},
	{"webauthn_credentials", "id"},
	{"permission_profiles", "id"}, {"library_members", "library_id, user_id"}, {"library_group_shares", "library_id, group_id"},
	{"works", "id"}, {"covers", "id"}, {"editions", "id"}, {"places", "created_at, id"}, {"instances", "id"},
	{"reading_states", "user_id, work_id"}, {"grades", "user_id, work_id"}, {"external_grades", "work_id, source"},
	{"lists", "id"}, {"list_items", "id"}, {"contacts", "id"}, {"loans", "id"},
	{"notification_channels", "id"}, {"access_tokens", "id"}, {"instance_settings", "key"},
}

type header struct {
	Format    string    `json:"format"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	App       string    `json:"app_version"`
}

type line struct {
	Table string          `json:"t"`
	Row   json.RawMessage `json:"r"`
}

// Export writes a backup archive. Rows are read in one repeatable-read transaction, so the
// export is a consistent snapshot even while the instance keeps running.
func Export(ctx context.Context, pool *pgxpool.Pool, covers storage.Store, w io.Writer, appVersion string) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("backup: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var data bytes.Buffer // NDJSON is built in memory: it holds metadata only, covers go straight to the tar
	enc := json.NewEncoder(&data)
	if err := enc.Encode(header{Format, Version, time.Now().UTC(), appVersion}); err != nil {
		return err
	}
	var coverHashes []string
	for _, t := range tables {
		rows, err := tx.Query(ctx, fmt.Sprintf("SELECT to_jsonb(t) FROM %s t ORDER BY %s", t.name, t.order)) //nolint:gosec // table names come from the fixed list above
		if err != nil {
			return fmt.Errorf("backup: read %s: %w", t.name, err)
		}
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				rows.Close()
				return fmt.Errorf("backup: scan %s: %w", t.name, err)
			}
			if err := enc.Encode(line{t.name, raw}); err != nil {
				rows.Close()
				return err
			}
			if t.name == "covers" {
				var c struct {
					Sha string `json:"sha256"` // bytea is rendered as \x<hex>
				}
				if json.Unmarshal(raw, &c) == nil {
					coverHashes = append(coverHashes, strings.TrimPrefix(c.Sha, `\x`))
				}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("backup: read %s: %w", t.name, err)
		}
	}

	tw := tar.NewWriter(w)
	if err := writeEntry(tw, dataFile, data.Bytes()); err != nil {
		return err
	}
	for _, h := range coverHashes {
		for _, suffix := range []string{"", ".thumb"} {
			if err := copyCover(ctx, covers, tw, h+suffix); err != nil {
				return err
			}
		}
	}
	return tw.Close()
}

func writeEntry(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: time.Now()}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func copyCover(ctx context.Context, covers storage.Store, tw *tar.Writer, key string) error {
	rc, err := covers.Open(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil // a missing file must not make the whole backup fail
	}
	if err != nil {
		return fmt.Errorf("backup: cover %s: %w", key, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, 16<<20))
	if err != nil {
		return err
	}
	return writeEntry(tw, coverPrefix+key, data)
}

// ErrNotEmpty is returned when restoring into an instance that already has accounts.
var ErrNotEmpty = errors.New("backup: the target instance is not empty")

// ErrFormat is returned for archives this version cannot read.
var ErrFormat = errors.New("backup: unsupported or corrupt archive")

// Restore loads a backup into an empty instance (no users). It is all or nothing for the
// database; cover files are written first, and are harmless if the database load fails.
func Restore(ctx context.Context, pool *pgxpool.Pool, covers storage.Store, r io.Reader, secretKey []byte) error {
	tr := tar.NewReader(r)
	var data []byte
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrFormat, err)
		}
		switch {
		case h.Name == dataFile:
			if data, err = io.ReadAll(io.LimitReader(tr, 1<<31)); err != nil {
				return fmt.Errorf("%w: %w", ErrFormat, err)
			}
		case strings.HasPrefix(h.Name, coverPrefix):
			key := strings.TrimPrefix(h.Name, coverPrefix)
			buf, err := io.ReadAll(io.LimitReader(tr, 16<<20))
			if err != nil {
				return fmt.Errorf("%w: %w", ErrFormat, err)
			}
			if _, err := hex.DecodeString(strings.TrimSuffix(key, ".thumb")); err != nil {
				return fmt.Errorf("%w: bad cover name", ErrFormat)
			}
			if err := covers.Put(ctx, key, buf); err != nil {
				return fmt.Errorf("backup: store cover: %w", err)
			}
		}
	}
	if data == nil {
		return fmt.Errorf("%w: no data file", ErrFormat)
	}
	return restoreRows(ctx, pool, data, secretKey)
}

func restoreRows(ctx context.Context, pool *pgxpool.Pool, data, secretKey []byte) error {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), maxLine)
	if !sc.Scan() {
		return ErrFormat
	}
	var h header
	if err := json.Unmarshal(sc.Bytes(), &h); err != nil || h.Format != Format || h.Version != Version {
		return ErrFormat
	}
	allowed := map[string]bool{}
	for _, t := range tables {
		allowed[t.name] = true
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("backup: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('biblisann.register'))"); err != nil {
		return err
	}
	var users int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&users); err != nil {
		return err
	}
	if users > 0 {
		return ErrNotEmpty
	}
	for sc.Scan() {
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil || !allowed[l.Table] {
			return fmt.Errorf("%w: bad row", ErrFormat)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT * FROM jsonb_populate_record(NULL::%s, $1::jsonb)", l.Table, l.Table), []byte(l.Row)); err != nil { //nolint:gosec // l.Table is checked against the allow-list
			return fmt.Errorf("backup: restore %s: %w", l.Table, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrFormat, err)
	}
	// Validate every restored authenticator before committing: otherwise a missing or wrong key
	// can leave the restored instance unusable, or tempt an operator to discard its MFA records.
	rows, err := tx.Query(ctx, "SELECT secret_enc FROM user_totp")
	if err != nil {
		return fmt.Errorf("backup: read restored factors: %w", err)
	}
	for rows.Next() {
		var sealed []byte
		if err := rows.Scan(&sealed); err != nil {
			rows.Close()
			return fmt.Errorf("backup: read restored factor: %w", err)
		}
		if _, err := secretbox.Open(secretKey, sealed); err != nil {
			rows.Close()
			return fmt.Errorf("backup: original secret key required to restore MFA: %w", err)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("backup: read restored factors: %w", err)
	}
	// The change log is not backed up: rebuild it so every offline client does a full resync.
	if _, err := tx.Exec(ctx, `INSERT INTO changes (library_id, entity, entity_id, op)
		SELECT library_id, 'instance', id, 'upsert' FROM instances WHERE deleted_at IS NULL`); err != nil {
		return fmt.Errorf("backup: rebuild changes: %w", err)
	}
	return tx.Commit(ctx)
}
