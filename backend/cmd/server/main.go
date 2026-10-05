package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/user/realtime-meeting-ast/backend/internal/auth"
	"github.com/user/realtime-meeting-ast/backend/internal/intelligence"
	"github.com/user/realtime-meeting-ast/backend/internal/meetings"
	"github.com/user/realtime-meeting-ast/backend/internal/providerconfig"
	"github.com/user/realtime-meeting-ast/backend/internal/realtime"
	"github.com/user/realtime-meeting-ast/backend/internal/sealedbox"
	"github.com/user/realtime-meeting-ast/backend/internal/secretbox"
	"github.com/user/realtime-meeting-ast/backend/internal/settings"
	"github.com/user/realtime-meeting-ast/backend/internal/storage"
	"github.com/user/realtime-meeting-ast/backend/internal/transcription"
)

func main() {
	// Load .env file if present. Resolution is CWD-relative AND anchored to common
	// project locations so it works regardless of where the server is launched from.
	exeDir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	cwd, _ := os.Getwd()
	candidateDirs := []string{cwd, exeDir, filepath.Dir(exeDir)}
	seen := map[string]bool{}
	loaded := false
	for i := 0; i < len(candidateDirs) && !loaded; i++ {
		dir := candidateDirs[i]
		for _, rel := range []string{".env", "../.env", "backend/.env"} {
			path := filepath.Join(dir, rel)
			if seen[path] {
				continue
			}
			seen[path] = true
			if err := godotenv.Load(path); err == nil {
				loaded = true
				log.Printf("Loaded .env from %s", path)
				break
			}
		}
	}
	if !loaded {
		log.Println("No .env file found, using environment variables")
	}

	cfg := loadConfig()

	db, err := storage.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	settingsStore := settings.New(db.DB())
	if err := settingsStore.Load(context.Background()); err != nil {
		log.Printf("Warning: failed to load runtime settings: %v (using environment defaults)", err)
	}

	meetingRepo := meetings.NewRepository(db.DB())

	glossary, err := intelligence.LoadGlossary(os.Getenv("BUSINESS_GLOSSARY_PATH"))
	if err != nil {
		log.Printf("Warning: failed to load business glossary: %v (using empty glossary)", err)
	}

	// Provider keys: platform defaults come from deployment secrets only. They
	// are never written to app_settings, so a database dump cannot leak them.
	authSecret := getEnv("AUTH_HMAC_SECRET", "")
	keyBox, err := secretbox.New(authSecret)
	if err != nil {
		log.Printf("Warning: AUTH_HMAC_SECRET is not set (%v); per-user API keys cannot be stored", err)
		keyBox = nil
	}

	platform := providerconfig.Selection{
		STTProvider: settingsStore.GetDefault(settings.KeySTTProvider, cfg.STTProvider),
		STTAPIKey:   cfg.STTAPIKey,
		LLMProvider: settingsStore.GetDefault(settings.KeyLLMProvider, cfg.LLMProvider),
		LLMAPIKey:   cfg.LLMAPIKey,
		LLMModel:    settingsStore.GetDefault(settings.KeyLLMModel, cfg.LLMModel),
	}
	providerStore := providerconfig.NewStore(db.DB(), keyBox, platform)

	// platformSelection re-reads the non-secret platform settings, so a change
	// made by the superadmin applies to the next meeting or connection without
	// a pod restart. API keys never change here: platform keys come from
	// deployment secrets, user keys from the admin users API.
	platformSelection := func() providerconfig.Selection {
		return providerconfig.Selection{
			STTProvider: settingsStore.GetDefault(settings.KeySTTProvider, cfg.STTProvider),
			STTAPIKey:   cfg.STTAPIKey,
			LLMProvider: settingsStore.GetDefault(settings.KeyLLMProvider, cfg.LLMProvider),
			LLMAPIKey:   cfg.LLMAPIKey,
			LLMModel:    settingsStore.GetDefault(settings.KeyLLMModel, cfg.LLMModel),
		}
	}

	hub := realtime.NewHub()
	go hub.Run()

	// Each meeting resolves its own LLM provider from its owner's settings, so a
	// user with their own key is billed on that key and everyone else keeps
	// using the platform provider.
	resolveProvider := func(meetingID int, ownerEmail string) (intelligence.IntelligenceProvider, error) {
		sel, err := providerStore.For(context.Background(), ownerEmail)
		if err != nil {
			return nil, err
		}
		return buildIntelProvider(sel, glossary)
	}
	meetingService := meetings.NewService(meetingRepo, hub, resolveProvider, glossary)

	if platform.STTAPIKey == "" && platform.STTProvider != string(transcription.ProviderGoogle) {
		log.Printf("Warning: no API key set for STT provider %q. Transcription will not work.", platform.STTProvider)
	}

	allowedEmails := auth.ParseAllowed(settingsStore.GetDefault(settings.KeyAllowedEmails, ""))
	authenticator := auth.New([]byte(authSecret), allowedEmails, sessionTTL)
	if admins, err := settingsStore.Admins(context.Background()); err != nil {
		log.Printf("Warning: failed to load admin emails from database: %v", err)
	} else {
		authenticator.SetAdmins(admins)
	}
	// The superadmin is configured only through the environment, so it cannot
	// be changed from the database or the admin API. It is implicitly allowed,
	// so it can still sign in when ALLOWED_EMAILS is non-empty.
	authenticator.SetSuperAdmin(getEnv("SUPERADMIN_EMAIL", ""))
	if super := authenticator.SuperAdmin(); super != "" {
		log.Printf("AUTH: superadmin role configured for %s (implicitly allowlisted)", super)
	} else {
		log.Println("AUTH: SUPERADMIN_EMAIL is empty; no one can change settings, user keys, or admins")
	}
	if !authenticator.Enabled() {
		log.Println("AUTH: ALLOWED_EMAILS is empty; access control is DISABLED")
	}
	if authSecret == "" {
		log.Println("AUTH: AUTH_HMAC_SECRET is empty; using an ephemeral secret (sessions reset on restart, user keys cannot be stored)")
	}

	googleClient := auth.NewGoogleClient(
		os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		os.Getenv("GOOGLE_OAUTH_REDIRECT_URL"),
	)
	if authenticator.Enabled() && !googleClient.Configured() {
		log.Println("AUTH: access control enabled but Google OAuth credentials are missing; login is unavailable")
	}

	// Seal box key pair for admin bodies that carry provider API keys. It is
	// generated per process: the public half is published for the browser to
	// seal with, the private half only decrypts here.
	sealBox, err := sealedbox.NewKeypair()
	if err != nil {
		log.Fatalf("Failed to initialise seal box: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/sealedbox/public-key", handleSealedBoxPublicKey(sealBox))
	mux.HandleFunc("/api/auth/google/start", handleGoogleAuthStart(authenticator, googleClient))
	mux.HandleFunc("/api/auth/google/callback", handleGoogleAuthCallback(authenticator, googleClient))
	mux.HandleFunc("/api/auth/logout", handleAuthLogout)
	mux.HandleFunc("/api/auth/me", handleAuthMe(authenticator))

	mux.HandleFunc("/api/meetings", handleMeetings(meetingService))
	mux.HandleFunc("/api/meetings/", handleMeetingByID(meetingService))
	mux.HandleFunc("/api/transcript/", handleTranscript(meetingService))

	// Admin surface. Admins may MONITOR (read-only, no secrets returned);
	// only the superadmin may change settings, admins, or user API keys.
	mux.HandleFunc("/api/admin/monitor", adminAuth(authenticator, handleAdminMonitor(db.DB(), settingsStore, providerStore, authenticator)))
	mux.HandleFunc("/api/admin/settings", adminAuth(authenticator, superAdminOnly(authenticator, handleAdminSettings(settingsStore, authenticator, func() {
		authenticator.SetAllowed(auth.ParseAllowed(settingsStore.GetDefault(settings.KeyAllowedEmails, "")))
		// Provider and model overrides take effect on the next connection or
		// meeting; the platform API keys are unchanged (deployment secrets).
		providerStore.SetPlatform(platformSelection())
	}))))
	mux.HandleFunc("/api/admin/admins", adminAuth(authenticator, superAdminOnly(authenticator, handleAdminUsers(settingsStore, authenticator))))
	// /api/admin/users serves the list on GET and per-user updates on
	// PUT/POST/DELETE, so the exact path and the sub-path share one handler.
	userProviders := handleAdminUserProvider(providerStore, settingsStore, authenticator, sealBox)
	mux.HandleFunc("/api/admin/users", adminAuth(authenticator, superAdminOnly(authenticator, handleAdminUserProviders(providerStore, authenticator, userProviders))))
	mux.HandleFunc("/api/admin/users/", adminAuth(authenticator, superAdminOnly(authenticator, userProviders)))

	mux.HandleFunc("/ws/meeting/", handleWebSocket(hub, meetingService, providerStore, buildSTTVocabulary(glossary)))

	handler := corsMiddleware(authMiddleware(authenticator, withSessionEmail(authenticator, mux)))

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Server starting on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Server shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

type Config struct {
	Port        string
	DatabaseURL string
	STTProvider string
	STTAPIKey   string
	LLMProvider string
	LLMAPIKey   string
	LLMModel    string
}

const sessionTTL = 7 * 24 * time.Hour

func loadConfig() Config {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@127.0.0.1:5432/meeting_ast?sslmode=disable"
	} else if _, inContainer := os.LookupEnv("KUBERNETES_SERVICE_HOST"); !inContainer && os.Getenv("CONTAINER") == "" {
		// Running on the host: remap Docker-only service hostnames to localhost.
		dbURL = strings.Replace(dbURL, "@postgres:", "@127.0.0.1:", 1)
	}

	return Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: dbURL,
		STTProvider: getEnv("STT_PROVIDER", "deepgram"),
		STTAPIKey:   os.Getenv("STT_API_KEY"),
		LLMProvider: getEnv("LLM_PROVIDER", "openai"),
		LLMAPIKey:   os.Getenv("LLM_API_KEY"),
		LLMModel:    os.Getenv("LLM_MODEL"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// buildIntelProvider constructs an intelligence provider for one resolved
// selection. A missing API key or unknown provider returns nil, which the
// meeting service treats as "no summary" rather than an error, so transcription
// and the meeting itself are unaffected.
func buildIntelProvider(sel providerconfig.Selection, glossary intelligence.Glossary) (intelligence.IntelligenceProvider, error) {
	if sel.LLMAPIKey == "" {
		return nil, fmt.Errorf("no API key configured for LLM provider %q", sel.LLMProvider)
	}
	p, err := intelligence.NewProvider(intelligence.Provider(sel.LLMProvider), sel.LLMAPIKey, sel.LLMModel, glossary)
	if err != nil {
		return nil, fmt.Errorf("initialize provider: %w", err)
	}
	return p, nil
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// authMiddleware gates every request except the auth endpoints themselves.
// When the allowlist is empty the gate is disabled entirely so local
// development stays friction-free, with one exception: /api/admin/* always
// requires a signed session. Expired/missing/invalid cookies receive a
// generic 401 (no information about the allowlist is leaked).
func authMiddleware(a *auth.Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		// The gate is open when no allowlist is configured (local development),
		// but the admin surface always requires a real signed session, which
		// can only be obtained through the Google OAuth flow.
		if !a.Enabled() && !strings.HasPrefix(r.URL.Path, "/api/admin/") {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(auth.CookieName)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if _, err := a.Verify(c.Value); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withSessionEmail re-verifies the session cookie and places the authenticated
// email in the request context, so handlers (meeting creation) can attribute
// work to a user without trusting anything client-supplied.
func withSessionEmail(a *auth.Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(auth.CookieName); err == nil {
			if email, err := a.Verify(c.Value); err == nil {
				next.ServeHTTP(w, r.WithContext(meetings.WithOwnerEmail(r.Context(), email)))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

const oauthStateCookie = "oauth_state"

// handleGoogleAuthStart kicks off the authorization-code flow. The state
// value doubles as a signed CSRF token: it travels both in the callback
// query parameter and in a short-lived HttpOnly cookie, and the callback
// requires both to match a cryptographically valid signature.
func handleGoogleAuthStart(a *auth.Authenticator, gc *auth.GoogleClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !gc.Configured() {
			http.Error(w, "Login unavailable: Google OAuth is not configured", http.StatusServiceUnavailable)
			return
		}
		state, err := a.IssueState(randomState(), 10*time.Minute)
		if err != nil {
			log.Printf("AUTH: failed to issue OAuth state: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		authURL, err := gc.Start(state)
		if err != nil {
			log.Printf("AUTH: failed to build Google authorize URL: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     oauthStateCookie,
			Value:    state,
			Path:     "/api/auth/google/callback",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecureRequest(r),
			MaxAge:   600,
		})
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

func handleGoogleAuthCallback(a *auth.Authenticator, gc *auth.GoogleClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		deny := func() {
			http.SetCookie(w, sessionCookie(r, "", -1))
			clearOAuthState(w, r)
			http.Redirect(w, r, "/", http.StatusFound)
		}

		if r.URL.Query().Get("error") != "" {
			log.Printf("AUTH: Google returned an OAuth error: %s", r.URL.Query().Get("error"))
			deny()
			return
		}
		state := r.URL.Query().Get("state")
		stateCookie, err := r.Cookie(oauthStateCookie)
		if err != nil || stateCookie.Value != state {
			log.Println("AUTH: OAuth callback state mismatch")
			deny()
			return
		}
		if _, err := a.VerifyState(state); err != nil {
			log.Printf("AUTH: OAuth callback state invalid: %v", err)
			deny()
			return
		}

		user, err := gc.Exchange(r.Context(), r.URL.Query().Get("code"))
		if err != nil {
			log.Printf("AUTH: Google token exchange failed: %v", err)
			deny()
			return
		}
		if !a.Allowed(user.Email) {
			log.Printf("AUTH: denied access for non-allowlisted Google account %s", user.Email)
			http.SetCookie(w, sessionCookie(r, "", -1))
			clearOAuthState(w, r)
			http.Redirect(w, r, "/?error=denied", http.StatusFound)
			return
		}

		token, err := a.Issue(user.Email)
		if err != nil {
			log.Printf("AUTH: failed to issue session token for %s: %v", user.Email, err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, sessionCookie(r, token, sessionTTL))
		clearOAuthState(w, r)
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func clearOAuthState(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    "",
		Path:     "/api/auth/google/callback",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		MaxAge:   -1,
	})
}

func randomState() string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie(r, "", -1))
	w.WriteHeader(http.StatusNoContent)
}

// adminEmailCtxKey carries the verified admin email into admin handlers.
type adminEmailCtxKey struct{}

// adminAuth verifies the session cookie and requires the admin role (or
// superadmin, which is strictly above admin).
func adminAuth(a *auth.Authenticator, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(auth.CookieName)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		email, err := a.Verify(c.Value)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if !a.IsAdmin(email) {
			log.Printf("ADMIN: denied non-admin access for %s to %s", email, r.URL.Path)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), adminEmailCtxKey{}, email)))
	}
}

// superAdminOnly restricts a handler to the env-configured superadmin. Regular
// admins can monitor, but only the superadmin can change settings, admins, or
// user API keys.
func superAdminOnly(a *auth.Authenticator, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email, _ := r.Context().Value(adminEmailCtxKey{}).(string)
		if !a.IsSuperAdmin(email) {
			log.Printf("ADMIN: denied %s (admin) attempt to %s; superadmin required", email, r.URL.Path)
			http.Error(w, "Superadmin role required", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

type adminSettingStatus struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret"`
	Set    bool   `json:"set"`
	Value  string `json:"value"` // masked for secret keys
}

// knownUsers returns every email that can use the service, so the admin user
// list includes the superadmin (env-only, not part of the allowlist) alongside
// the allowlisted users.
func knownUsers(a *auth.Authenticator) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(a.Allowlist())+1)
	for _, email := range append([]string{a.SuperAdmin()}, a.Allowlist()...) {
		normalized := auth.NormalizeEmail(email)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

// handleAdminMonitor reports runtime health and configuration to admins.
// No API key value is ever returned, not even masked: the response only states
// whether a key is configured and which source it comes from.
func handleAdminMonitor(db *sql.DB, store *settings.Store, providers *providerconfig.Store, a *auth.Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()

		statuses := make([]adminSettingStatus, 0, len(settings.All))
		for _, st := range store.Statuses() {
			statuses = append(statuses, adminSettingStatus{
				Key:    st.Key.Name,
				Secret: st.Key.Secret,
				Set:    st.Set,
				Value:  settings.MaskedValue(st.Key, st.Value),
			})
		}

		audit, err := store.RecentAudit(ctx, 25)
		if err != nil {
			log.Printf("ADMIN: failed to load audit log: %v", err)
			audit = []settings.AuditEntry{}
		}

		dbStatus := "ok"
		if err := db.PingContext(ctx); err != nil {
			dbStatus = "error"
		}
		var meetingCount int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meetings`).Scan(&meetingCount); err != nil {
			log.Printf("ADMIN: failed to count meetings: %v", err)
			meetingCount = -1
		}

		authGate := "disabled"
		if a.Enabled() {
			authGate = "enabled"
		}

		admins, err := store.Admins(ctx)
		if err != nil {
			log.Printf("ADMIN: failed to load admins: %v", err)
			admins = []string{}
		}

		platform := providers.Platform()
		users, err := providers.List(ctx, knownUsers(a))
		if err != nil {
			log.Printf("ADMIN: failed to load user provider settings: %v", err)
			users = []providerconfig.UserSummary{}
		}

		viewer, _ := r.Context().Value(adminEmailCtxKey{}).(string)
		writeJSON(w, http.StatusOK, map[string]any{
			"settings":  statuses,
			"database":  dbStatus,
			"meetings":  meetingCount,
			"auth_gate": authGate,
			// provider_store.Platform() is read at request time, so a provider or
			// model changed by the superadmin is reflected immediately.
			"platform": map[string]any{
				"stt_provider": platform.STTProvider,
				"stt_key_set":  platform.STTAPIKey != "",
				"llm_provider": platform.LLMProvider,
				"llm_model":    platform.LLMModel,
				"llm_key_set":  platform.LLMAPIKey != "",
				"google_stt":   platform.STTProvider == string(transcription.ProviderGoogle),
				"own_keys_ok":  providers.OwnKeysAvailable(),
			},
			"admins":      admins,
			"superadmin":  a.SuperAdmin(),
			"viewer":      viewer,
			"viewer_role": a.Role(viewer),
			"users":       users,
			"audit":       audit,
		})
	}
}

// handleAdminUsers manages the admin role. Superadmin only. Admin emails are
// stored in the database; granting or revoking applies immediately to the live
// authenticator. The superadmin cannot be demoted (their role is env-configured
// and not part of this table) and the last admin cannot be removed.
func handleAdminUsers(store *settings.Store, a *auth.Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acting, _ := r.Context().Value(adminEmailCtxKey{}).(string)
		role := a.Role(acting)
		ctx := r.Context()
		refresh := func() {
			if admins, err := store.Admins(ctx); err == nil {
				a.SetAdmins(admins)
			}
		}

		switch r.Method {
		case http.MethodGet:
			admins, err := store.Admins(ctx)
			if err != nil {
				log.Printf("ADMIN: failed to load admins: %v", err)
				http.Error(w, "Failed to load admins", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"admins":     admins,
				"superadmin": a.SuperAdmin(),
			})

		case http.MethodPost:
			var body struct {
				Email string `json:"email"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
				http.Error(w, "Invalid request body", http.StatusBadRequest)
				return
			}
			email := auth.NormalizeEmail(body.Email)
			if !strings.Contains(email, "@") {
				http.Error(w, "A valid email is required", http.StatusBadRequest)
				return
			}
			if a.IsSuperAdmin(email) {
				http.Error(w, "The superadmin already holds the highest role and does not need an admin row", http.StatusBadRequest)
				return
			}
			if err := store.AddAdmin(ctx, email, acting, role); err != nil {
				log.Printf("ADMIN %s: failed to add admin %s: %v", acting, email, err)
				http.Error(w, "Failed to add admin", http.StatusInternalServerError)
				return
			}
			log.Printf("ADMIN %s (%s): set %s as admin", acting, role, email)
			refresh()
			writeJSON(w, http.StatusOK, map[string]any{"added": email})

		case http.MethodDelete:
			email := auth.NormalizeEmail(r.URL.Query().Get("email"))
			if email == "" {
				http.Error(w, "email query parameter is required", http.StatusBadRequest)
				return
			}
			if a.IsSuperAdmin(email) {
				http.Error(w, "The superadmin is configured via SUPERADMIN_EMAIL and cannot be removed", http.StatusBadRequest)
				return
			}
			admins, err := store.Admins(ctx)
			if err != nil {
				log.Printf("ADMIN %s: failed to load admins: %v", acting, err)
				http.Error(w, "Failed to load admins", http.StatusInternalServerError)
				return
			}
			if len(admins) <= 1 {
				http.Error(w, "Cannot remove the last admin", http.StatusBadRequest)
				return
			}
			if err := store.RemoveAdmin(ctx, email, acting, role); err != nil {
				log.Printf("ADMIN %s: failed to remove admin %s: %v", acting, email, err)
				http.Error(w, "Failed to remove admin", http.StatusInternalServerError)
				return
			}
			log.Printf("ADMIN %s (%s): removed admin %s", acting, role, email)
			refresh()
			writeJSON(w, http.StatusOK, map[string]any{"removed": email})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// handleAdminUserProviders lists every user and which providers they use.
// Superadmin only; no key material is included. Other methods are handled by
// handleAdminUserProvider, which this delegates to.
func handleAdminUserProviders(store *providerconfig.Store, a *auth.Authenticator, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next(w, r)
			return
		}
		users, err := store.List(r.Context(), knownUsers(a))
		if err != nil {
			log.Printf("ADMIN: failed to list user providers: %v", err)
			http.Error(w, "Failed to load users", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"users":       users,
			"own_keys_ok": store.OwnKeysAvailable(),
			"platform":    store.Platform(),
		})
	}
}

// adminUserProviderRequest is the write-only update payload for one user. API
// keys are only ever written: omitting a key leaves the stored key untouched,
// and clear_* removes it.
type adminUserProviderRequest struct {
	Email       string `json:"email"`
	UseOwnKeys  *bool  `json:"use_own_keys"`
	STTProvider string `json:"stt_provider"`
	STTAPIKey   string `json:"stt_api_key"`
	ClearSTTKey bool   `json:"clear_stt_key"`
	LLMProvider string `json:"llm_provider"`
	LLMModel    string `json:"llm_model"`
	LLMAPIKey   string `json:"llm_api_key"`
	ClearLLMKey bool   `json:"clear_llm_key"`
}

// handleAdminUserProvider creates, updates, or resets one user's provider
// configuration. Superadmin only. The body may arrive sealed (see
// internal/sealedbox) because it can carry API keys; keys are encrypted before
// storage and are never echoed back.
func handleAdminUserProvider(store *providerconfig.Store, settingsStore *settings.Store, a *auth.Authenticator, sealBox *sealedbox.Keypair) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acting, _ := r.Context().Value(adminEmailCtxKey{}).(string)
		ctx := r.Context()

		// DELETE /api/admin/users/{email} resets a user to the platform defaults.
		if r.Method == http.MethodDelete {
			email := auth.NormalizeEmail(r.URL.Query().Get("email"))
			if email == "" {
				email = auth.NormalizeEmail(strings.TrimPrefix(r.URL.Path, "/api/admin/users/"))
			}
			if !strings.Contains(email, "@") {
				http.Error(w, "A valid email is required", http.StatusBadRequest)
				return
			}
			if err := store.Delete(ctx, email); err != nil {
				log.Printf("ADMIN %s: failed to reset providers for %s: %v", acting, email, err)
				http.Error(w, "Failed to reset user configuration", http.StatusInternalServerError)
				return
			}
			log.Printf("ADMIN %s: reset %s to platform provider defaults", acting, email)
			// Removing stored keys is a configuration change, so it is audited
			// like any other. The audit stores the target email, never a key.
			if err := settingsStore.Audit(ctx, settings.ActionClear, acting, a.Role(acting), "USER_PROVIDERS:"+email); err != nil {
				log.Printf("ADMIN %s: failed to audit reset of %s: %v", acting, email, err)
			}
			writeJSON(w, http.StatusOK, map[string]any{"reset": email})
			return
		}

		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// The body is size limited before decoding because a sealed envelope
		// still arrives as one JSON object, and an oversized one is bad input
		// whether it is encrypted or not.
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		var body adminUserProviderRequest
		// Every failure here is bad client input: a body that is not the
		// expected JSON, or an envelope that does not decrypt for this key.
		if err := sealBox.OpenInto(raw, &body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		email := auth.NormalizeEmail(body.Email)
		if !strings.Contains(email, "@") {
			http.Error(w, "A valid email is required", http.StatusBadRequest)
			return
		}
		if !store.OwnKeysAvailable() && (body.STTAPIKey != "" || body.LLMAPIKey != "") {
			http.Error(w, "Cannot store user API keys: AUTH_HMAC_SECRET is not configured", http.StatusServiceUnavailable)
			return
		}

		// Preserve the current toggle when the caller does not send one.
		useOwn := false
		current, err := store.Get(ctx, email)
		switch {
		case body.UseOwnKeys != nil:
			useOwn = *body.UseOwnKeys
		case err == nil:
			useOwn = current.UseOwnKeys
		case err != sql.ErrNoRows:
			log.Printf("ADMIN %s: failed to load providers for %s: %v", acting, email, err)
			http.Error(w, "Failed to load user configuration", http.StatusInternalServerError)
			return
		}

		if err := store.Save(ctx, providerconfig.SaveInput{
			Email:       email,
			UseOwnKeys:  useOwn,
			STTProvider: body.STTProvider,
			STTAPIKey:   body.STTAPIKey,
			ClearSTTKey: body.ClearSTTKey,
			LLMProvider: body.LLMProvider,
			LLMModel:    body.LLMModel,
			LLMAPIKey:   body.LLMAPIKey,
			ClearLLMKey: body.ClearLLMKey,
			UpdatedBy:   acting,
		}); err != nil {
			log.Printf("ADMIN %s: failed to save providers for %s: %v", acting, email, err)
			switch {
			case errors.Is(err, providerconfig.ErrNoEncryptionKey):
				http.Error(w, "Cannot store user API keys: encryption is not configured", http.StatusServiceUnavailable)
				return
			case errors.Is(err, providerconfig.ErrOwnKeysNeedKey):
				http.Error(w, "Enabling own keys requires at least one STT or LLM API key", http.StatusBadRequest)
				return
			}
			// Other failures (constraint violations, connectivity) are server-side
			// problems; the cause stays in the log, not in the response.
			http.Error(w, "Failed to save user configuration", http.StatusInternalServerError)
			return
		}

		action := settings.ActionSet
		if !useOwn {
			action = settings.ActionClear
		}
		log.Printf("ADMIN %s (%s): updated provider settings for %s (own_keys=%v)", acting, a.Role(acting), email, useOwn)
		if err := settingsStore.Audit(ctx, action, acting, a.Role(acting), "USER_PROVIDERS:"+email); err != nil {
			log.Printf("ADMIN: failed to record audit for user providers %s: %v", email, err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"email": email, "use_own_keys": useOwn})
	}
}

// canonicalAllowlist rewrites an allowlist value into the exact form it is
// enforced in: trimmed, lower-cased, no duplicates, comma separated. The admin
// UI sends a de-duplicated list already, but any other client could otherwise
// leave the stored string disagreeing with the effective access list. An empty
// value is returned unchanged because it means "clear the override".
func canonicalAllowlist(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return value
	}
	return strings.Join(auth.ParseAllowed(trimmed), ",")
}

// handleAdminSettings applies setting updates. Superadmin only (enforced by
// superAdminOnly). No API key is a settable setting: platform keys live in
// deployment secrets and per-user keys are stored encrypted via
// /api/admin/users. An empty value clears an override, reverting to the env
// baseline.
func handleAdminSettings(store *settings.Store, a *auth.Authenticator, apply func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		email, _ := r.Context().Value(adminEmailCtxKey{}).(string)
		ctx := r.Context()

		var payload map[string]string
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if len(payload) == 0 {
			http.Error(w, "No settings provided", http.StatusBadRequest)
			return
		}
		for name := range payload {
			if _, ok := settings.Find(name); !ok {
				http.Error(w, fmt.Sprintf("Unknown setting %q", name), http.StatusBadRequest)
				return
			}
		}

		for name, value := range payload {
			key, _ := settings.Find(name)
			if key == settings.KeyAllowedEmails {
				value = canonicalAllowlist(value)
			}
			if err := store.Set(ctx, key, value, email); err != nil {
				log.Printf("ADMIN %s: failed to set %s: %v", email, name, err)
				http.Error(w, "Failed to save settings", http.StatusInternalServerError)
				return
			}
			action := settings.ActionSet
			if strings.TrimSpace(value) == "" {
				action = settings.ActionClear
			}
			role := a.Role(email)
			if err := store.Audit(ctx, action, email, role, name); err != nil {
				log.Printf("ADMIN: failed to record audit for %s: %v", name, err)
			}
			log.Printf("ADMIN %s: %s %s", email, action, name)
		}

		apply()
		writeJSON(w, http.StatusOK, map[string]any{"updated": true})
	}
}

// handleAuthMe reports the session identity and role so the frontend can show
// the admin entry point and hide superadmin-only controls.
func handleAuthMe(a *auth.Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := auth.RoleUser
		email := ""
		// The gate is open when ALLOWED_EMAILS is empty, so a missing cookie is
		// not an error. A session is still verified when present, otherwise the
		// superadmin and admins configured in the database would be invisible in
		// the UI exactly when the gate is disabled.
		if c, err := r.Cookie(auth.CookieName); err == nil {
			if sessionEmail, err := a.Verify(c.Value); err == nil {
				email = sessionEmail
				role = a.Role(email)
			} else if a.Enabled() {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		} else if a.Enabled() {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"email":         email,
			"role":          role,
			"is_admin":      role != auth.RoleUser,
			"is_superadmin": role == auth.RoleSuperAdmin,
		})
	}
}

func sessionCookie(r *http.Request, token string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		MaxAge:   int(ttl.Seconds()),
	}
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func handleMeetings(svc *meetings.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			svc.ListMeetings(w, r)
		case http.MethodPost:
			svc.CreateMeeting(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func handleMeetingByID(svc *meetings.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			svc.GetMeeting(w, r)
		case http.MethodPut:
			svc.UpdateMeeting(w, r)
		case http.MethodDelete:
			svc.DeleteMeeting(w, r)
		case http.MethodPost:
			if strings.HasSuffix(r.URL.Path, "/summary") {
				svc.RegenerateSummary(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func handleTranscript(svc *meetings.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		svc.GetTranscript(w, r)
	}
}

// handleWebSocket bridges a live meeting room to the STT provider. The provider
// and API key are resolved per connection from the meeting owner's settings, so
// a user on their own key transcribes with that key and everyone else uses the
// platform defaults.
func handleWebSocket(hub *realtime.Hub, svc *meetings.Service, providerStore *providerconfig.Store, vocab transcription.Vocabulary) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resolveSTT := func(meetingID int, ownerEmail string) (string, string, error) {
			sel, err := providerStore.For(r.Context(), ownerEmail)
			if err != nil {
				return "", "", err
			}
			return sel.STTProvider, sel.STTAPIKey, nil
		}
		realtime.HandleWebSocket(hub, svc, w, r, resolveSTT, vocab)
	}
}

// buildSTTVocabulary converts the business glossary into STT phrase biasing and
// transcript normalization inputs. Hint and normalization choices per term come
// from the glossary's stt_hints/stt_normalize fields; term + expansion are
// always biased.
func buildSTTVocabulary(glossary intelligence.Glossary) transcription.Vocabulary {
	terms := make([]transcription.VocabularyTerm, 0, len(glossary.Terms))
	for _, t := range glossary.Terms {
		terms = append(terms, transcription.VocabularyTerm{
			Term:         t.Term,
			Expansion:    t.Expansion,
			STTHints:     t.STTHints,
			STTNormalize: t.STTNormalize,
		})
	}
	return transcription.NewVocabulary(terms)
}
