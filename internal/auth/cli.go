package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
)

// ResetLinkForEmail backs `inventory reset-password <email>`: whoever can
// run the binary against the data directory may mint a one-time reset link
// for any user, valid 24 hours. It returns the link (with the public URL in
// front when configured, otherwise just the path) and its expiry.
func ResetLinkForEmail(ctx context.Context, d *db.DB, cfg config.Config, email string) (string, time.Time, error) {
	s := New(d, cfg)
	q := sqlc.New(d.Write)
	user, err := q.GetUserByEmail(ctx, normEmail(email))
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, fmt.Errorf("no user with email %q", email)
	} else if err != nil {
		return "", time.Time{}, err
	}
	households, err := q.ListUserHouseholds(ctx, user.ID)
	if err != nil {
		return "", time.Time{}, err
	}
	if len(households) == 0 {
		return "", time.Time{}, fmt.Errorf("user %q belongs to no household", email)
	}
	return s.issueResetLink(ctx, q, households[0].HouseholdID, user.ID, user.ID)
}
