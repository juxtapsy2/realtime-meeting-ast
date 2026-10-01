package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	"github.com/user/realtime-meeting-ast/backend/internal/realtime"
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

	meetingRepo := meetings.NewRepository(db.DB())

	glossary, err := intelligence.LoadGlossary(os.Getenv("BUSINESS_GLOSSARY_PATH"))
	if err != nil {
		log.Printf("Warning: failed to load business glossary: %v (using empty glossary)", err)
	}

	var intelProvider intelligence.IntelligenceProvider
	if cfg.LLMAPIKey != "" {
		var err error
		intelProvider, err = intelligence.NewProvider(intelligence.Provider(cfg.LLMProvider), cfg.LLMAPIKey, glossary)
		if err != nil {
			log.Printf("Warning: Failed to initialize intelligence provider: %v", err)
			log.Println("Intelligence features will be disabled")
		}
	}

	if cfg.STTAPIKey == "" && cfg.STTProvider != string(transcription.ProviderGoogle) {
		log.Printf("Warning: no API key set for STT provider %q. Transcription will not work.", cfg.STTProvider)
	}

	hub := realtime.NewHub()
	go hub.Run()

	meetingService := meetings.NewService(meetingRepo, hub, intelProvider, glossary)

	allowedEmails := auth.ParseAllowed(os.Getenv("ALLOWED_EMAILS"))
	authSecret := getEnv("AUTH_HMAC_SECRET", "")
	authenticator := auth.New([]byte(authSecret), allowedEmails, sessionTTL)
	if !authenticator.Enabled() {
		log.Println("AUTH: ALLOWED_EMAILS is empty; access control is DISABLED")
	}
	if authSecret == "" {
		log.Println("AUTH: AUTH_HMAC_SECRET is empty; using an ephemeral secret (sessions reset on restart)")
	}

	googleClient := auth.NewGoogleClient(
		os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		os.Getenv("GOOGLE_OAUTH_REDIRECT_URL"),
	)
	if authenticator.Enabled() && !googleClient.Configured() {
		log.Println("AUTH: access control enabled but Google OAuth credentials are missing; login is unavailable")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/auth/google/start", handleGoogleAuthStart(authenticator, googleClient))
	mux.HandleFunc("/api/auth/google/callback", handleGoogleAuthCallback(authenticator, googleClient))
	mux.HandleFunc("/api/auth/logout", handleAuthLogout)
	mux.HandleFunc("/api/auth/me", handleAuthMe(authenticator))

	mux.HandleFunc("/api/meetings", handleMeetings(meetingService))
	mux.HandleFunc("/api/meetings/", handleMeetingByID(meetingService))
	mux.HandleFunc("/api/transcript/", handleTranscript(meetingService))

	mux.HandleFunc("/ws/meeting/", handleWebSocket(hub, meetingService, cfg.STTProvider, cfg.STTAPIKey, buildSTTVocabulary(glossary)))

	handler := corsMiddleware(authMiddleware(authenticator, mux))

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
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
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
// development stays friction-free. Expired/missing/invalid cookies receive a
// generic 401 (no information about the allowlist is leaked).
func authMiddleware(a *auth.Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Enabled() || strings.HasPrefix(r.URL.Path, "/api/auth/") {
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

func handleAuthMe(a *auth.Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Enabled() {
			writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "email": ""})
			return
		}
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
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "email": email})
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

func handleWebSocket(hub *realtime.Hub, svc *meetings.Service, sttProvider string, sttAPIKey string, vocab transcription.Vocabulary) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		realtime.HandleWebSocket(hub, svc, w, r, sttProvider, sttAPIKey, vocab)
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
