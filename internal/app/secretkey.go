package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/config"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/secretbox"
)

// ErrSecretKey is returned at startup when BIBLISANN_SECRET_KEY does not match the data it sealed.
var ErrSecretKey = errors.New("BIBLISANN_SECRET_KEY problem")

// CheckSecretKey refuses to start when the secret key is missing or wrong while sealed data exists.
// Silently carrying on would switch two-factor authentication off (missing key) or lock users out
// (wrong key). Recovery: restore the original key, or, if it is truly lost, delete the affected
// rows on purpose (docs/user/security.md).
func CheckSecretKey(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) error {
	q := dbsqlc.New(pool)
	totp, err := q.SealedTOTPSamples(ctx)
	if err != nil {
		return fmt.Errorf("check secret key: %w", err)
	}
	hc, err := q.SealedHardcoverSamples(ctx)
	if err != nil {
		return fmt.Errorf("check secret key: %w", err)
	}
	sealed := append(append([][]byte{}, totp...), hc...)
	if cfg.SecretKey == "" {
		if len(sealed) > 0 {
			return fmt.Errorf("%w: it is not set, but users have two-factor authentication or a Hardcover link that it protects; "+
				"start with the original key (refusing to start rather than letting accounts sign in without their second factor)", ErrSecretKey)
		}
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(cfg.SecretKey.Reveal())
	if err != nil || len(key) != 32 {
		return fmt.Errorf("%w: it must be the base64 encoding of 32 bytes", ErrSecretKey)
	}
	for _, s := range sealed {
		if _, err := secretbox.Open(key, s); err != nil {
			return fmt.Errorf("%w: it does not open data sealed earlier (a different key than before?); "+
				"start with the original key", ErrSecretKey)
		}
	}
	return nil
}
