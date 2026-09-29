package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
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

	if cfg.STTAPIKey == "" && cfg.STTProvider != string(transcription.ProviderWhisper) && cfg.STTProvider != string(transcription.ProviderGoogle) {
		log.Printf("Warning: no API key set for STT provider %q. Transcription will not work.", cfg.STTProvider)
	}

	hub := realtime.NewHub()
	go hub.Run()

	meetingService := meetings.NewService(meetingRepo, hub, intelProvider, glossary)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/meetings", handleMeetings(meetingService))
	mux.HandleFunc("/api/meetings/", handleMeetingByID(meetingService))
	mux.HandleFunc("/api/transcript/", handleTranscript(meetingService))

	mux.HandleFunc("/ws/meeting/", handleWebSocket(hub, meetingService, cfg.STTProvider, cfg.STTAPIKey, buildSTTVocabulary(glossary)))

	handler := corsMiddleware(mux)

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
