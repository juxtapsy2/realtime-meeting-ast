// Package settings provides runtime-editable platform configuration.
//
// Environment variables are the baseline for every key. The superadmin can
// override non-secret values at runtime through the admin API; overrides are
// persisted in PostgreSQL so they survive restarts. Several layers (auth
// allowlist, default STT/LLM provider selection) read through this store,
// which is what lets settings changes take effect without a pod restart.
//
// API keys are deliberately NOT settable here. Platform keys live only in
// deployment secrets, and per-user keys live encrypted in
// user_provider_settings (see internal/providerconfig), so this table never
// holds a secret in plaintext.
package settings

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Key describes a runtime-settable configuration value. No key in this package
// is a secret: platform API keys are env-only, user API keys are encrypted in
// their own table.
type Key struct {
	Name   string
	Secret bool // reserved; when true the value must never be displayed in full
}

var (
	KeyAllowedEmails = Key{Name: "ALLOWED_EMAILS"}
	KeySTTProvider   = Key{Name: "STT_PROVIDER"}
	KeyLLMProvider   = Key{Name: "LLM_PROVIDER"}
	KeyLLMModel      = Key{Name: "LLM_MODEL"}
)

// All lists every runtime-settable key in canonical display order. Provider and
// model are platform-level only: they are settable here, and not per user.
var All = []Key{
	KeyAllowedEmails,
	KeySTTProvider,
	KeyLLMProvider,
	KeyLLMModel,
}

// Find resolves a key name to its Key definition.
func Find(name string) (Key, bool) {
	for _, k := range All {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

// MaskedValue returns a display-safe form of a value: full text for
// non-secret keys, otherwise the final four characters prefixed by bullets.
// Empty values stay empty.
func MaskedValue(k Key, v string) string {
	if !k.Secret || v == "" {
		return v
	}
	if len(v) <= 4 {
		return "••••"
	}
	return "••••" + v[len(v)-4:]
}

// action names recorded in the audit log.
const (
	ActionSet   = "set"
	ActionClear = "clear"
)

// Store is a thread-safe configuration store. The process environment is the
// baseline; rows in app_settings override it. All methods are safe for
// concurrent use; methods that hit the database require a non-nil db.
type Store struct {
	db *sql.DB

	mu sync.RWMutex
	// vals holds the effective values (DB override wins over environment).
	vals map[string]string
	// set records whether an explicit value exists for a key.
	set map[string]bool
}

// New seeds the store from the process environment.
func New(db *sql.DB) *Store {
	vals := make(map[string]string)
	set := make(map[string]bool)
	for _, k := range All {
		if v, ok := os.LookupEnv(k.Name); ok && v != "" {
			vals[k.Name] = v
			set[k.Name] = true
		}
	}
	return &Store{db: db, vals: vals, set: set}
}

// Load applies persisted overrides from the database on top of the
// environment baseline. Call it once at startup, after migrations.
func (s *Store) Load(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	defer rows.Close()

	overrides := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return fmt.Errorf("scan settings: %w", err)
		}
		overrides[k] = v
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate settings: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range overrides {
		if _, known := Find(k); !known {
			continue
		}
		s.vals[k] = v
		s.set[k] = true
	}
	return nil
}

// Get returns the effective value for key and whether one is configured.
func (s *Store) Get(key Key) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.set[key.Name] {
		return "", false
	}
	return s.vals[key.Name], true
}

// GetDefault returns the effective value, falling back to fallback when the
// key has no explicit value.
func (s *Store) GetDefault(key Key, fallback string) string {
	if v, ok := s.Get(key); ok {
		return v
	}
	return fallback
}

// Set persists an override for key. An empty value clears the override and
// reverts to the environment baseline. updatedBy is recorded as the actor.
func (s *Store) Set(ctx context.Context, key Key, value, updatedBy string) error {
	value = strings.TrimSpace(value)

	if value == "" {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM app_settings WHERE key = $1`, key.Name); err != nil {
			return fmt.Errorf("clear setting: %w", err)
		}
	} else {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO app_settings (key, value, updated_by, updated_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (key) DO UPDATE
			SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
			key.Name, value, updatedBy, time.Now().UTC()); err != nil {
			return fmt.Errorf("save setting: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	envValue, envSet := os.LookupEnv(key.Name)
	if value == "" {
		if envSet {
			s.vals[key.Name] = envValue
			s.set[key.Name] = true
			return nil
		}
		delete(s.vals, key.Name)
		delete(s.set, key.Name)
		return nil
	}
	s.vals[key.Name] = value
	s.set[key.Name] = true
	return nil
}

// Audit records an admin configuration change together with the role the actor
// held, so the log shows which role made each change.
func (s *Store) Audit(ctx context.Context, action, email, role, keyName string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_audit_log (admin_email, role, action, setting_key) VALUES ($1, $2, $3, $4)`,
		email, role, action, keyName); err != nil {
		return fmt.Errorf("record audit: %w", err)
	}
	return nil
}

// AuditEntry describes one admin configuration change.
type AuditEntry struct {
	AdminEmail string    `json:"admin_email"`
	Role       string    `json:"role"`
	Action     string    `json:"action"`
	SettingKey string    `json:"setting_key"`
	CreatedAt  time.Time `json:"created_at"`
}

// RecentAudit returns the most recent audit entries, newest first.
func (s *Store) RecentAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT admin_email, role, action, setting_key, created_at
		 FROM admin_audit_log ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("load audit log: %w", err)
	}
	defer rows.Close()

	out := make([]AuditEntry, 0, limit)
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.AdminEmail, &e.Role, &e.Action, &e.SettingKey, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Status describes the effective value of a settable key. Callers must mask
// secret values before exposing them to clients.
type Status struct {
	Key   Key
	Value string
	Set   bool
}

// Statuses snapshots every runtime-settable key.
func (s *Store) Statuses() []Status {
	out := make([]Status, 0, len(All))
	for _, k := range All {
		v, ok := s.Get(k)
		out = append(out, Status{Key: k, Value: v, Set: ok})
	}
	return out
}

// Admin emails are managed in the database (table admin_emails), not in the
// environment. The superadmin above them is configured only through the
// SUPERADMIN_EMAIL environment variable.

// Admins returns the current admin emails.
func (s *Store) Admins(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT email FROM admin_emails ORDER BY email`)
	if err != nil {
		return nil, fmt.Errorf("load admins: %w", err)
	}
	defer rows.Close()

	// Never a nil slice: a JSON nil renders as null, and clients that render
	// .length on this field would crash when no admin is stored yet.
	out := make([]string, 0)
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, fmt.Errorf("scan admin: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddAdmin grants the admin role. Adding an existing email is a no-op.
func (s *Store) AddAdmin(ctx context.Context, email, updatedBy, role string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_emails (email) VALUES ($1) ON CONFLICT (email) DO NOTHING`,
		email); err != nil {
		return fmt.Errorf("add admin: %w", err)
	}
	return s.Audit(ctx, ActionSet, updatedBy, role, "ADMIN_EMAILS")
}

// RemoveAdmin revokes the admin role.
func (s *Store) RemoveAdmin(ctx context.Context, email, updatedBy, role string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM admin_emails WHERE email = $1`, email); err != nil {
		return fmt.Errorf("remove admin: %w", err)
	}
	return s.Audit(ctx, ActionClear, updatedBy, role, "ADMIN_EMAILS")
}
