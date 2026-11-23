// Package gc removes data nothing refers to any more: soft-deleted copies past their undo window,
// editions and works nobody uses, cover files without an edition, superseded change-log rows and
// expired short-lived records. It runs daily as a background job and on demand (`biblisann gc`).
// Every step is idempotent and safe to interrupt.
package gc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/storage"
)

// Options are the retention settings, in days unless noted.
type Options struct {
	SoftDeleteDays   int // a deleted copy can be restored for this long (default 90)
	UnusedEditions   int // an edition nobody uses is dropped after this long (default 30)
	CoverGraceHours  int // a cover without edition survives this long (default 24)
	SupersededDays   int // redundant change-log rows older than this are compacted (default 30)
	TombstoneDays    int // deletions older than this are forgotten; slower clients resync (default 180)
	ProviderCacheDay int // cached provider answers older than this are dropped (default 90)
}

// Defaults returns the default retention.
func Defaults() Options {
	return Options{SoftDeleteDays: 90, UnusedEditions: 30, CoverGraceHours: 24, SupersededDays: 30, TombstoneDays: 180, ProviderCacheDay: 90}
}

// Report counts what a run removed.
type Report struct {
	Instances, Editions, Works, Covers, CoverFiles, Superseded, Tombstones, Expired int64
}

func (r Report) String() string {
	return fmt.Sprintf("instances=%d editions=%d works=%d covers=%d cover_files=%d changes_compacted=%d tombstones=%d expired=%d",
		r.Instances, r.Editions, r.Works, r.Covers, r.CoverFiles, r.Superseded, r.Tombstones, r.Expired)
}

// Run executes every step. Storage failures are logged and skipped (the next run retries them:
// the database row is only removed after its files).
func Run(ctx context.Context, pool *pgxpool.Pool, covers storage.Store, o Options, log *slog.Logger) (Report, error) {
	q := dbsqlc.New(pool)
	var r Report
	var errs []error
	// Every step is independent: one failing step is reported but never stops the others, so a
	// single bad row cannot make the database grow forever.
	step := func(name string, f func() error) {
		if err := f(); err != nil {
			log.WarnContext(ctx, "gc: step failed", "step", name, "error", err)
			errs = append(errs, fmt.Errorf("gc: %s: %w", name, err))
		}
	}
	step("instances", func() (err error) {
		r.Instances, err = q.GCPurgeSoftDeletedInstances(ctx, int32(o.SoftDeleteDays))
		return
	})
	step("editions", func() (err error) { r.Editions, err = q.GCPurgeUnusedEditions(ctx, int32(o.UnusedEditions)); return })
	step("works", func() (err error) { r.Works, err = q.GCPurgeEmptyWorks(ctx); return })
	step("covers", func() error {
		unused, err := q.GCUnreferencedCovers(ctx, int32(o.CoverGraceHours))
		if err != nil {
			return err
		}
		for _, c := range unused {
			key := hex.EncodeToString(c.Sha256)
			okFiles := true
			for _, k := range []string{key, key + ".thumb"} {
				if err := covers.Delete(ctx, k); err != nil {
					log.WarnContext(ctx, "gc: cover file not removed, will retry", "key", k, "error", err)
					okFiles = false
					continue
				}
				r.CoverFiles++
			}
			if okFiles {
				if err := q.GCDeleteCover(ctx, c.ID); err != nil {
					return fmt.Errorf("cover row: %w", err)
				}
				r.Covers++
			}
		}
		return nil
	})
	step("changes", func() (err error) {
		r.Superseded, err = q.GCCompactSupersededChanges(ctx, int32(o.SupersededDays))
		return
	})
	step("tombstones", func() error {
		pruned, err := q.GCPruneOldTombstones(ctx, int32(o.TombstoneDays))
		if err != nil {
			return err
		}
		for _, p := range pruned {
			r.Tombstones++
			if err := q.GCSetPrunedThrough(ctx, dbsqlc.GCSetPrunedThroughParams{ID: p.LibraryID, ChangesPrunedThrough: p.Seq}); err != nil {
				return fmt.Errorf("horizon: %w", err)
			}
		}
		return nil
	})
	step("expired", func() (err error) { r.Expired, err = q.GCExpireMisc(ctx, int32(o.ProviderCacheDay)); return })
	return r, errors.Join(errs...)
}
