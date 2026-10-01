// Package providerconfig resolves which STT/LLM providers and API keys apply
// to a given user.
//
// A user either rides the platform defaults (Google STT + Groq LLM, supplied
// by deployment secrets) or brings their own providers and keys, stored
// encrypted in the database. Resolution is deliberately pure and small: a
// user's own values win per field, and any field they left empty falls back to
// the platform default.
package providerconfig

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/secretbox"
)

// Source describes where an effective selection came from.
const (
	SourcePlatform = "platform"
	SourceUser     = "own_keys"
)

// Field labels used as authenticated data when sealing keys, so a ciphertext
// cannot be replayed from one field into another.
const (
	fieldSTTAPIKey = "stt_api_key"
	fieldLLMAPIKey = "llm_api_key"
)

// Selection is the effective provider configuration for one user.
type Selection struct {
	Source      string
	STTProvider string
	STTAPIKey   string
	LLMProvider string
	LLMAPIKey   string
	LLMModel    string
}

// UserSettings is a stored per-user configuration. API keys are encrypted in
// the database; the plaintext is only ever held in memory.
type UserSettings struct {
	Email        string
	UseOwnKeys   bool
	STTProvider  string
	STTAPIKeyEnc string
	LLMProvider  string
	LLMModel     string
	LLMAPIKeyEnc string
	UpdatedBy    string
	UpdatedAt    time.Time
}

// UserSummary describes a user for the admin UI: stored configuration plus
// activity, with no secret material.
type UserSummary struct {
	Email       string `json:"email"`
	UseOwnKeys  bool   `json:"use_own_keys"`
	STTProvider string `json:"stt_provider"`
	STTKeySet   bool   `json:"stt_key_set"`
	LLMProvider string `json:"llm_provider"`
	LLMModel    string `json:"llm_model"`
	LLMKeySet   bool   `json:"llm_key_set"`
	UpdatedBy   string `json:"updated_by,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Meetings    int    `json:"meetings"`
	Effective   string `json:"effective_source"`
}

// ErrNoEncryptionKey is returned when a user's own key must be stored but no
// encryption secret is configured.
var ErrNoEncryptionKey = secretbox.ErrNoKey

// ErrOwnKeysNeedKey is a validation error: own keys cannot be enabled without
// supplying at least one key, otherwise the user would silently keep using the
// platform account.
var ErrOwnKeysNeedKey = errors.New("enabling own keys requires at least one API key")

// Store reads and writes per-user provider settings.
type Store struct {
	db  *sql.DB
	box *secretbox.Box
	// platform is swapped when the superadmin changes platform settings, so a
	// new value applies to the next meeting or connection without a restart.
	platform atomic.Pointer[Selection]
	// loader is the seam For() reads through; it is s.load in production and a
	// fake in tests.
	loader settingsLoader
}

// NewStore wires the store. platform holds the deployment defaults used when a
// user has no own-key configuration. box may be nil when no encryption secret
// is configured; storing own keys is then rejected.
func NewStore(db *sql.DB, box *secretbox.Box, platform Selection) *Store {
	s := &Store{db: db, box: box}
	s.SetPlatform(platform)
	s.loader = s
	return s
}

// SetPlatform replaces the platform defaults (for example after the superadmin
// changes a provider or model).
func (s *Store) SetPlatform(platform Selection) {
	platform.Source = SourcePlatform
	s.platform.Store(&platform)
}

// Platform returns the current platform defaults.
func (s *Store) Platform() Selection {
	if p := s.platform.Load(); p != nil {
		return *p
	}
	return Selection{Source: SourcePlatform}
}

// OwnKeysAvailable reports whether own keys can be stored at all.
func (s *Store) OwnKeysAvailable() bool {
	return s.box != nil
}

// settingsLoader is the seam For() reads through, so the platform-fallback
// rules can be tested without a database.
type settingsLoader interface {
	load(ctx context.Context, email string) (UserSettings, error)
}

// For resolves the effective selection for a user's email. An unknown email
// (or one without own keys) gets the platform defaults. When own keys are
// enabled but cannot be decrypted, resolution fails rather than silently
// billing the platform account.
func (s *Store) For(ctx context.Context, email string) (Selection, error) {
	platform := s.Platform()
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return platform, nil
	}

	stored, err := s.loader.load(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		// No stored configuration: the user is on the platform defaults.
		return platform, nil
	}
	if err != nil {
		return Selection{}, err
	}
	if !stored.UseOwnKeys {
		return platform, nil
	}

	sttKey, err := s.open(fieldSTTAPIKey, stored.STTAPIKeyEnc)
	if err != nil {
		return Selection{}, fmt.Errorf("providerconfig: stt key for %s: %w", email, err)
	}
	llmKey, err := s.open(fieldLLMAPIKey, stored.LLMAPIKeyEnc)
	if err != nil {
		return Selection{}, fmt.Errorf("providerconfig: llm key for %s: %w", email, err)
	}

	return resolve(platform, stored.UseOwnKeys, stored.STTProvider, sttKey, stored.LLMProvider, stored.LLMModel, llmKey), nil
}

// resolve merges a user's own values over the platform defaults. Own values
// win per field; empty own fields fall back to the platform. When the user
// has not enabled own keys, the platform is used unchanged.
func resolve(platform Selection, useOwn bool, sttProvider, sttKey, llmProvider, llmModel, llmKey string) Selection {
	if !useOwn {
		return platform
	}
	out := platform
	out.Source = SourceUser
	if sttProvider != "" {
		out.STTProvider = sttProvider
	}
	if sttKey != "" {
		out.STTAPIKey = sttKey
	}
	if llmProvider != "" {
		out.LLMProvider = llmProvider
	}
	if llmModel != "" {
		out.LLMModel = llmModel
	}
	if llmKey != "" {
		out.LLMAPIKey = llmKey
	}
	return out
}

// Get reads a user's stored configuration.
func (s *Store) Get(ctx context.Context, email string) (UserSettings, error) {
	return s.load(ctx, email)
}

func (s *Store) load(ctx context.Context, email string) (UserSettings, error) {
	var us UserSettings
	var updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT user_email, use_own_keys, stt_provider, stt_api_key_enc,
		       llm_provider, llm_model, llm_api_key_enc, updated_by, updated_at
		FROM user_provider_settings
		WHERE user_email = $1`, strings.ToLower(strings.TrimSpace(email)),
	).Scan(&us.Email, &us.UseOwnKeys, &us.STTProvider, &us.STTAPIKeyEnc,
		&us.LLMProvider, &us.LLMModel, &us.LLMAPIKeyEnc, &us.UpdatedBy, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return UserSettings{}, err
	}
	if err != nil {
		return UserSettings{}, fmt.Errorf("providerconfig: load settings: %w", err)
	}
	if updatedAt.Valid {
		us.UpdatedAt = updatedAt.Time
	}
	return us, nil
}

// SaveInput carries an admin's write-only update for one user. Empty key
// fields leave the stored key untouched; pass ClearSTTKey/ClearLLMKey to drop
// them.
type SaveInput struct {
	Email       string
	UseOwnKeys  bool
	STTProvider string
	STTAPIKey   string
	ClearSTTKey bool
	LLMProvider string
	LLMModel    string
	LLMAPIKey   string
	ClearLLMKey bool
	UpdatedBy   string
}

// Save validates and persists a user's configuration, encrypting any new keys.
func (s *Store) Save(ctx context.Context, in SaveInput) error {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" {
		return fmt.Errorf("providerconfig: email is required")
	}

	current, err := s.Get(ctx, email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	us := current
	us.Email = email
	us.UseOwnKeys = in.UseOwnKeys
	if in.STTProvider != "" {
		us.STTProvider = in.STTProvider
	}
	if in.LLMProvider != "" {
		us.LLMProvider = in.LLMProvider
	}
	if in.LLMModel != "" {
		us.LLMModel = in.LLMModel
	}

	if in.ClearSTTKey {
		us.STTAPIKeyEnc = ""
	} else if in.STTAPIKey != "" {
		if s.box == nil {
			return ErrNoEncryptionKey
		}
		sealed, err := s.box.Seal(fieldSTTAPIKey, in.STTAPIKey)
		if err != nil {
			return err
		}
		us.STTAPIKeyEnc = sealed
	}

	if in.ClearLLMKey {
		us.LLMAPIKeyEnc = ""
	} else if in.LLMAPIKey != "" {
		if s.box == nil {
			return ErrNoEncryptionKey
		}
		sealed, err := s.box.Seal(fieldLLMAPIKey, in.LLMAPIKey)
		if err != nil {
			return err
		}
		us.LLMAPIKeyEnc = sealed
	}

	// Enabling own keys without any usable key would silently fall back to the
	// platform account while claiming to be on the user's own key.
	if us.UseOwnKeys && us.STTAPIKeyEnc == "" && us.LLMAPIKeyEnc == "" {
		return ErrOwnKeysNeedKey
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO user_provider_settings
			(user_email, use_own_keys, stt_provider, stt_api_key_enc,
			 llm_provider, llm_model, llm_api_key_enc, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_email) DO UPDATE SET
			use_own_keys = EXCLUDED.use_own_keys,
			stt_provider = EXCLUDED.stt_provider,
			stt_api_key_enc = EXCLUDED.stt_api_key_enc,
			llm_provider = EXCLUDED.llm_provider,
			llm_model = EXCLUDED.llm_model,
			llm_api_key_enc = EXCLUDED.llm_api_key_enc,
			updated_by = EXCLUDED.updated_by,
			updated_at = EXCLUDED.updated_at`,
		us.Email, us.UseOwnKeys, us.STTProvider, us.STTAPIKeyEnc,
		us.LLMProvider, us.LLMModel, us.LLMAPIKeyEnc, in.UpdatedBy, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("providerconfig: save settings: %w", err)
	}
	return nil
}

// Delete removes a user's configuration so they fall back to the platform.
func (s *Store) Delete(ctx context.Context, email string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM user_provider_settings WHERE user_email = $1`,
		strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return fmt.Errorf("providerconfig: delete settings: %w", err)
	}
	return nil
}

// List returns a per-user view for the admin UI: the union of allowlisted
// emails, users with stored configuration, and meeting owners, so admins can
// see who rides the platform defaults and who brings their own keys.
func (s *Store) List(ctx context.Context, allowlist []string) ([]UserSummary, error) {
	platform := s.Platform()
	rows, err := s.db.QueryContext(ctx, `
		WITH users AS (
			SELECT user_email AS email FROM user_provider_settings
			UNION
			SELECT owner_email FROM meetings WHERE owner_email IS NOT NULL AND owner_email <> ''
		)
		SELECT u.email,
		       COALESCE(s.use_own_keys, FALSE),
		       COALESCE(s.stt_provider, ''),
		       COALESCE(s.stt_api_key_enc, '') <> '',
		       COALESCE(s.llm_provider, ''),
		       COALESCE(s.llm_model, ''),
		       COALESCE(s.llm_api_key_enc, '') <> '',
		       COALESCE(s.updated_by, ''),
		       s.updated_at,
		       (SELECT COUNT(*) FROM meetings m WHERE m.owner_email = u.email)
		FROM users u
		LEFT JOIN user_provider_settings s ON s.user_email = u.email`)
	if err != nil {
		return nil, fmt.Errorf("providerconfig: list users: %w", err)
	}
	defer rows.Close()

	byEmail := map[string]UserSummary{}
	for rows.Next() {
		var u UserSummary
		var updatedAt sql.NullTime
		if err := rows.Scan(&u.Email, &u.UseOwnKeys, &u.STTProvider, &u.STTKeySet,
			&u.LLMProvider, &u.LLMModel, &u.LLMKeySet, &u.UpdatedBy, &updatedAt, &u.Meetings); err != nil {
			return nil, fmt.Errorf("providerconfig: scan user: %w", err)
		}
		u.Effective = platform.Source
		if u.UseOwnKeys {
			u.Effective = SourceUser
		}
		if updatedAt.Valid {
			u.UpdatedAt = updatedAt.Time.UTC().Format(time.RFC3339)
		}
		byEmail[u.Email] = u
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Allowlisted users without any activity or configuration still matter:
	// they are the people who can currently use the platform keys.
	for _, email := range allowlist {
		normalized := strings.ToLower(strings.TrimSpace(email))
		if normalized == "" {
			continue
		}
		if _, ok := byEmail[normalized]; !ok {
			byEmail[normalized] = UserSummary{Email: normalized, Effective: platform.Source}
		}
	}

	out := make([]UserSummary, 0, len(byEmail))
	for _, u := range byEmail {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

// open decrypts a stored key. Empty ciphertext means "not provided".
func (s *Store) open(field, blob string) (string, error) {
	if blob == "" {
		return "", nil
	}
	if s.box == nil {
		return "", ErrNoEncryptionKey
	}
	return s.box.Open(field, blob)
}
