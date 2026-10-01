package storage

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migration is one versioned schema change loaded from a file named
// <version>_<name>.sql (e.g. 0002_runtime_settings.sql).
type migration struct {
	version int
	name    string
	body    string
}

// loadMigrations reads the embedded migration files and returns them ordered
// by version. Duplicate or unparsable versions are a hard error: silently
// skipping a migration would leave the schema ambiguous.
func loadMigrations() ([]migration, error) {
	entries, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("storage: list migrations: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("storage: no migrations found")
	}

	migrations := make([]migration, 0, len(entries))
	seen := make(map[int]string, len(entries))
	for _, entry := range entries {
		version, name, err := parseMigrationName(entry)
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("storage: duplicate migration version %d (%s and %s)", version, prev, name)
		}
		seen[version] = name

		body, err := migrationFS.ReadFile(entry)
		if err != nil {
			return nil, fmt.Errorf("storage: read migration %s: %w", entry, err)
		}
		migrations = append(migrations, migration{version: version, name: name, body: string(body)})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return migrations, nil
}

// parseMigrationName extracts the version and name from a migration filename.
func parseMigrationName(path string) (int, string, error) {
	base := path[strings.LastIndexByte(path, '/')+1:]
	underscore := strings.IndexByte(base, '_')
	if underscore <= 0 || !strings.HasSuffix(base, ".sql") {
		return 0, "", fmt.Errorf("storage: migration %q is not named <version>_<name>.sql", base)
	}
	version, err := strconv.Atoi(base[:underscore])
	if err != nil {
		return 0, "", fmt.Errorf("storage: migration %q has a non-numeric version: %w", base, err)
	}
	return version, base, nil
}

// Migrate applies every pending migration in version order. Each migration runs
// inside its own transaction and is recorded in schema_migrations, so Migrate
// is safe (and cheap) to call on every boot and resumes where it left off if a
// migration failed.
func (p *Postgres) Migrate() error {
	if _, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("storage: create schema_migrations: %w", err)
	}

	applied, err := p.appliedMigrations()
	if err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := p.applyMigration(m); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) appliedMigrations() (map[int]bool, error) {
	rows, err := p.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("storage: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("storage: scan schema_migrations: %w", err)
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

// applyMigration runs a single migration and records it atomically.
func (p *Postgres) applyMigration(m migration) error {
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("storage: begin migration %d: %w", m.version, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(m.body); err != nil {
		return fmt.Errorf("storage: apply migration %d (%s): %w", m.version, m.name, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`,
		m.version, m.name,
	); err != nil {
		return fmt.Errorf("storage: record migration %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit migration %d: %w", m.version, err)
	}
	return nil
}

// AppliedVersions reports the migrations recorded as applied. It is used by
// tests and diagnostics.
func (p *Postgres) AppliedVersions() ([]int, error) {
	rows, err := p.db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("storage: read schema_migrations: %w", err)
	}
	defer rows.Close()

	var out []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		out = append(out, version)
	}
	return out, rows.Err()
}
