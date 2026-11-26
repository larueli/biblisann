// Package covercheck finds covers whose database row has no file in the cover storage (local
// directory or S3 bucket): a lost disk, a bucket emptied by hand, or an import whose best-effort
// file write failed. It only reports; it never changes data. It runs weekly as a background job and
// on demand (`biblisann covers-check`).
package covercheck

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

// GraceHours is how long a new cover row may wait for its file (imports store files after the
// database commit).
const GraceHours = 1

// maxLogged bounds the warnings of one run; the report still counts everything.
const maxLogged = 50

// Report counts what a run found.
type Report struct {
	Checked      int   // covers whose files were looked up
	MissingFull  int   // covers without their full-size file
	MissingThumb int   // covers without their thumbnail
	Editions     int64 // editions that show a cover with a missing file
}

// Missing reports whether any file is missing.
func (r Report) Missing() bool { return r.MissingFull > 0 || r.MissingThumb > 0 }

func (r Report) String() string {
	return fmt.Sprintf("checked=%d missing_full=%d missing_thumb=%d editions_affected=%d", r.Checked, r.MissingFull, r.MissingThumb, r.Editions)
}

// Run compares the covers table with the keys the store holds. The store must be able to list its
// keys (both backends can). One listing is cheaper than one lookup per cover on S3.
func Run(ctx context.Context, pool *pgxpool.Pool, covers storage.Store, log *slog.Logger) (Report, error) {
	lister, ok := covers.(storage.Lister)
	if !ok {
		return Report{}, errors.New("covercheck: the cover storage cannot list its keys")
	}
	keys, err := lister.List(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("covercheck: list files: %w", err)
	}
	present := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		present[k] = struct{}{}
	}
	rows, err := dbsqlc.New(pool).CoversForFileCheck(ctx, GraceHours)
	if err != nil {
		return Report{}, fmt.Errorf("covercheck: list covers: %w", err)
	}
	var r Report
	logged := 0
	for _, c := range rows {
		r.Checked++
		key := hex.EncodeToString(c.Sha256)
		_, full := present[key]
		_, thumb := present[key+".thumb"]
		if full && thumb {
			continue
		}
		if !full {
			r.MissingFull++
		}
		if !thumb {
			r.MissingThumb++
		}
		r.Editions += c.Editions
		if logged < maxLogged {
			logged++
			log.WarnContext(ctx, "cover file missing", "cover", c.ID, "sha256", key, "full", !full, "thumb", !thumb, "editions", c.Editions)
		}
	}
	return r, nil
}
