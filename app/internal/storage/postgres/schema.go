package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// ApplicationRolesMigration includes the canonical schema and application grants.
const ApplicationRolesMigration = "20260905082946"
const ItemSyncMigration = "20260927071753"

func CheckSchema(ctx context.Context, database *sql.DB) error {
	var ready bool
	err := database.QueryRowContext(ctx, `SELECT EXISTS (
  SELECT 1 FROM public.schema_migrations WHERE version = $1
 ) AND EXISTS (
  SELECT 1 FROM public.schema_migrations WHERE version = $2
 ) AND EXISTS (
  SELECT 1 FROM public.schema_migrations WHERE version = $3
 )`, "20260904184133", ApplicationRolesMigration, ItemSyncMigration).Scan(&ready)
	if err != nil {
		return fmt.Errorf("check infra migration %s: %w", ItemSyncMigration, err)
	}
	if !ready {
		return fmt.Errorf("required infra migrations through %s are not applied", ItemSyncMigration)
	}
	return nil
}
