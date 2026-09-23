package postgres

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestCheckSchema(t *testing.T) {
	for _, test := range []struct {
		name      string
		registry  bool
		versions  []string
		wantReady bool
	}{
		{name: "missing registry"},
		{name: "empty registry", registry: true},
		{name: "schema without grants", registry: true, versions: []string{"20260904184133"}},
		{name: "grants without schema", registry: true, versions: []string{MinimumMigration}},
		{name: "required migrations", registry: true, versions: []string{"20260904184133", MinimumMigration}, wantReady: true},
		{name: "later migrations allowed", registry: true, versions: []string{"20260904184133", MinimumMigration, "20260906000000"}, wantReady: true},
		{name: "later version cannot replace prerequisites", registry: true, versions: []string{"20260906000000"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			database.SetMaxOpenConns(1)
			if _, err := database.Exec("ATTACH DATABASE ':memory:' AS public"); err != nil {
				t.Fatal(err)
			}
			if test.registry {
				if _, err := database.Exec("CREATE TABLE public.schema_migrations (version TEXT PRIMARY KEY)"); err != nil {
					t.Fatal(err)
				}
				for _, version := range test.versions {
					if _, err := database.Exec("INSERT INTO public.schema_migrations VALUES ($1)", version); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := database.Exec("PRAGMA query_only = ON"); err != nil {
				t.Fatal(err)
			}
			err = CheckSchema(t.Context(), database)
			if (err == nil) != test.wantReady {
				t.Fatalf("CheckSchema() = %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), MinimumMigration) {
				t.Fatalf("error does not identify required migration: %v", err)
			}
		})
	}
}
