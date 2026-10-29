package providerconfig

// Saved STT/LLM combos: named snapshots of a user's provider configuration
// that can be switched without retyping providers, models, and keys.
//
// A combo stores the same ciphertexts as user_provider_settings (AES-256-GCM
// with the field label as authenticated data, not the user or row), so
// applying a combo copies them verbatim into the settings row server-side.
// Ciphertexts never leave the server: Combo reports only whether a key exists.
//
// There is deliberately no "active" pointer column. The active combo is
// derived by comparing the current settings row against the saved combos, so
// a manual edit, a key retype, or a delete automatically shows the right
// state with no second write that could drift out of sync.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ComboList.ActiveKind values. They are sent as a kind + name pair rather
// than one string so a combo can never be confused with a sentinel by its
// name.
const (
	ActiveKindSystem = "system" // riding the platform defaults
	ActiveKindCustom = "custom" // own keys that match no saved combo
	ActiveKindCombo  = "combo"  // ActiveName matches a saved combo
)

// Combo name validation and lookup failures, mapped by the transport to
// 400 / 404 / 400 (or 503 for a missing encryption box) respectively.
var (
	ErrComboName     = errors.New("providerconfig: invalid combo name")
	ErrComboNotFound = errors.New("providerconfig: combo not found")
	ErrComboNoSource = errors.New("providerconfig: no stored configuration to save as a combo")
)

// Combo is one saved setup. Ciphertexts are json:"-" so the list response
// carries key presence only.
type Combo struct {
	Name         string `json:"name"`
	STTProvider  string `json:"stt_provider"`
	STTAPIKeyEnc string `json:"-"`
	LLMProvider  string `json:"llm_provider"`
	LLMModel     string `json:"llm_model"`
	LLMAPIKeyEnc string `json:"-"`
	STTKeySet    bool   `json:"stt_key_set"`
	LLMKeySet    bool   `json:"llm_key_set"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// ComboList is the combo browser payload: the user's saved combos plus which
// one (if any) their current configuration matches.
type ComboList struct {
	ActiveKind string  `json:"active_kind"`
	ActiveName string  `json:"active_name,omitempty"`
	Combos     []Combo `json:"combos"`
}

// ValidateComboName checks a combo name as it will be stored: trimmed,
// non-empty, and at most 100 characters (the column width).
func ValidateComboName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrComboName)
	}
	if utf8.RuneCountInString(name) > 100 {
		return fmt.Errorf("%w: name must be at most 100 characters", ErrComboName)
	}
	return nil
}

// deriveActiveCombo reports which saved combo matches the current
// configuration: system while own keys are off, the first (alphabetically)
// identical combo when one matches, custom otherwise. A manual key retype
// produces a different ciphertext and therefore correctly reads as custom.
func deriveActiveCombo(us UserSettings, combos []Combo) (kind, name string) {
	if !us.UseOwnKeys {
		return ActiveKindSystem, ""
	}
	match := ""
	for _, c := range combos {
		if c.STTProvider == us.STTProvider && c.STTAPIKeyEnc == us.STTAPIKeyEnc &&
			c.LLMProvider == us.LLMProvider && c.LLMModel == us.LLMModel &&
			c.LLMAPIKeyEnc == us.LLMAPIKeyEnc {
			if match == "" || c.Name < match {
				match = c.Name
			}
		}
	}
	if match == "" {
		return ActiveKindCustom, ""
	}
	return ActiveKindCombo, match
}

// ListCombos returns the user's saved combos, ordered by name, plus which one
// their current configuration matches.
func (s *Store) ListCombos(ctx context.Context, email string) (ComboList, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return ComboList{}, errors.New("providerconfig: email is required")
	}

	// No stored row is not an error: it means the platform defaults, which is
	// an active state like any other.
	us, err := s.load(ctx, normalized)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ComboList{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT name, stt_provider, stt_api_key_enc, llm_provider, llm_model,
		       llm_api_key_enc, updated_at
		FROM user_provider_combos
		WHERE user_email = $1
		ORDER BY name`, normalized)
	if err != nil {
		return ComboList{}, fmt.Errorf("providerconfig: list combos: %w", err)
	}
	defer rows.Close()

	combos := []Combo{}
	for rows.Next() {
		var c Combo
		var updatedAt sql.NullTime
		if err := rows.Scan(&c.Name, &c.STTProvider, &c.STTAPIKeyEnc,
			&c.LLMProvider, &c.LLMModel, &c.LLMAPIKeyEnc, &updatedAt); err != nil {
			return ComboList{}, fmt.Errorf("providerconfig: scan combo: %w", err)
		}
		c.STTKeySet = c.STTAPIKeyEnc != ""
		c.LLMKeySet = c.LLMAPIKeyEnc != ""
		if updatedAt.Valid {
			c.UpdatedAt = updatedAt.Time.UTC().Format(time.RFC3339)
		}
		combos = append(combos, c)
	}
	if err := rows.Err(); err != nil {
		return ComboList{}, fmt.Errorf("providerconfig: list combos: %w", err)
	}

	kind, name := deriveActiveCombo(us, combos)
	return ComboList{ActiveKind: kind, ActiveName: name, Combos: combos}, nil
}

// SaveCombo snapshots the user's current stored configuration under a name
// (upserting an existing name). The source must contain at least one stored
// key: a combo with no key could never be applied as own-keys configuration.
func (s *Store) SaveCombo(ctx context.Context, email, name string) error {
	if err := ValidateComboName(name); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	normalized := strings.ToLower(strings.TrimSpace(email))

	// Snapshot the source through the loader seam (s.load in production),
	// like For(), so the rules above are testable without a database.
	us, err := s.loader.load(ctx, normalized)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrComboNoSource
	}
	if err != nil {
		return err
	}
	if us.STTAPIKeyEnc == "" && us.LLMAPIKeyEnc == "" {
		return ErrComboNoSource
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO user_provider_combos
			(user_email, name, stt_provider, stt_api_key_enc,
			 llm_provider, llm_model, llm_api_key_enc, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (user_email, name) DO UPDATE SET
			stt_provider = EXCLUDED.stt_provider,
			stt_api_key_enc = EXCLUDED.stt_api_key_enc,
			llm_provider = EXCLUDED.llm_provider,
			llm_model = EXCLUDED.llm_model,
			llm_api_key_enc = EXCLUDED.llm_api_key_enc,
			updated_at = EXCLUDED.updated_at`,
		normalized, name, us.STTProvider, us.STTAPIKeyEnc,
		us.LLMProvider, us.LLMModel, us.LLMAPIKeyEnc)
	if err != nil {
		return fmt.Errorf("providerconfig: save combo: %w", err)
	}
	return nil
}

// ApplyCombo switches the user's stored configuration to a saved combo by
// copying its fields (ciphertexts included) verbatim and enabling own keys.
// updatedBy is stamped on the settings row like any other configuration write.
func (s *Store) ApplyCombo(ctx context.Context, email, name, updatedBy string) error {
	if err := ValidateComboName(name); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	normalized := strings.ToLower(strings.TrimSpace(email))

	var c Combo
	err := s.db.QueryRowContext(ctx, `
		SELECT stt_provider, stt_api_key_enc, llm_provider, llm_model, llm_api_key_enc
		FROM user_provider_combos
		WHERE user_email = $1 AND name = $2`, normalized, name,
	).Scan(&c.STTProvider, &c.STTAPIKeyEnc, &c.LLMProvider, &c.LLMModel, &c.LLMAPIKeyEnc)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrComboNotFound
	}
	if err != nil {
		return fmt.Errorf("providerconfig: load combo: %w", err)
	}
	if c.STTAPIKeyEnc == "" && c.LLMAPIKeyEnc == "" {
		// Cannot happen via SaveCombo; refuses enabling own keys with nothing
		// to bill against instead of silently using the platform account.
		return ErrOwnKeysNeedKey
	}
	// Without a box the copied ciphertexts could never be decrypted, which
	// would break the user's next session — refuse instead, like Save does.
	if s.box == nil {
		return ErrNoEncryptionKey
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO user_provider_settings
			(user_email, use_own_keys, stt_provider, stt_api_key_enc,
			 llm_provider, llm_model, llm_api_key_enc, updated_by, updated_at)
		VALUES ($1, TRUE, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (user_email) DO UPDATE SET
			use_own_keys = TRUE,
			stt_provider = EXCLUDED.stt_provider,
			stt_api_key_enc = EXCLUDED.stt_api_key_enc,
			llm_provider = EXCLUDED.llm_provider,
			llm_model = EXCLUDED.llm_model,
			llm_api_key_enc = EXCLUDED.llm_api_key_enc,
			updated_by = EXCLUDED.updated_by,
			updated_at = EXCLUDED.updated_at`,
		normalized, c.STTProvider, c.STTAPIKeyEnc,
		c.LLMProvider, c.LLMModel, c.LLMAPIKeyEnc, updatedBy)
	if err != nil {
		return fmt.Errorf("providerconfig: apply combo: %w", err)
	}
	return nil
}

// DeleteCombo removes one saved combo. Deleting the combo that currently
// matches the settings row needs no extra write: the active state is derived,
// so the row simply reads as custom afterwards.
func (s *Store) DeleteCombo(ctx context.Context, email, name string) error {
	if err := ValidateComboName(name); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	normalized := strings.ToLower(strings.TrimSpace(email))

	res, err := s.db.ExecContext(ctx,
		`DELETE FROM user_provider_combos WHERE user_email = $1 AND name = $2`,
		normalized, name)
	if err != nil {
		return fmt.Errorf("providerconfig: delete combo: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrComboNotFound
	}
	return nil
}
