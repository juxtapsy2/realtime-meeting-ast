package storage

import (
	"strings"
	"testing"
)

func TestParseMigrationName(t *testing.T) {
	version, name, err := parseMigrationName("migrations/0002_runtime_settings.sql")
	if err != nil {
		t.Fatalf("parseMigrationName: %v", err)
	}
	if version != 2 {
		t.Errorf("version = %d, want 2", version)
	}
	if name != "0002_runtime_settings.sql" {
		t.Errorf("name = %q", name)
	}

	for _, bad := range []string{
		"migrations/abc_thing.sql",
		"migrations/0002.sql",
		"migrations/_leading.sql",
		"migrations/2_thing.txt",
	} {
		if _, _, err := parseMigrationName(bad); err == nil {
			t.Errorf("parseMigrationName(%q) should fail", bad)
		}
	}
}

func TestLoadMigrationsOrderedAndUnique(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) < 4 {
		t.Fatalf("expected at least 4 migrations, got %d", len(migrations))
	}
	for i, m := range migrations {
		if i == 0 {
			continue
		}
		if m.version <= migrations[i-1].version {
			t.Fatalf("migrations not ordered: %d after %d", m.version, migrations[i-1].version)
		}
		if strings.TrimSpace(m.body) == "" {
			t.Errorf("migration %d (%s) has an empty body", m.version, m.name)
		}
	}
}
