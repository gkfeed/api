package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// MinimumMigration includes the canonical schema and application grants.
const MinimumMigration = "20260905082946"

func CheckSchema(ctx context.Context, database *sql.DB) error {
	var ready bool
	err := database.QueryRowContext(ctx, `SELECT EXISTS (
  SELECT 1 FROM public.schema_migrations WHERE version = $1
 ) AND EXISTS (
  SELECT 1 FROM public.schema_migrations WHERE version = $2
 )`, "20260904184133", MinimumMigration).Scan(&ready)
	if err != nil {
		return fmt.Errorf("check infra migration %s: %w", MinimumMigration, err)
	}
	if !ready {
		return fmt.Errorf("required infra migrations through %s are not applied", MinimumMigration)
	}
	return nil
}
