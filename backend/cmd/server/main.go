package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	// Load .env file if present (check multiple locations for monorepo layout)
	loaded := false
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if err := godotenv.Load(path); err == nil {
			loaded = true
			log.Printf("Loaded .env from %s", path)
			break
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

	sttProvider, err := transcription.NewProvider(transcription.Provider(cfg.STTProvider), cfg.STTAPIKey)
	if err != nil {
		log.Fatalf("Failed to initialize STT provider: %v", err)
	}

	var intelProvider intelligence.IntelligenceProvider
	if cfg.LLMAPIKey != "" {
		intelProvider, err = intelligence.NewProvider(intelligence.Provider(cfg.LLMProvider), cfg.LLMAPIKey)
		if err != nil {
			log.Printf("Warning: Failed to initialize intelligence provider: %v", err)
			log.Println("Intelligence features will be disabled")
		}
	}

	hub := realtime.NewHub()
	go hub.Run()

	meetingService := meetings.NewService(meetingRepo, hub, sttProvider, intelProvider)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/meetings", handleMeetings(meetingService))
	mux.HandleFunc("/api/meetings/", handleMeetingByID(meetingService))
	mux.HandleFunc("/api/transcript/", handleTranscript(meetingService))

	mux.HandleFunc("/ws/meeting/", handleWebSocket(hub, meetingService))

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
	// Fallback: if DATABASE_URL still empty, try reading .env manually
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@127.0.0.1:5432/meeting_ast?sslmode=disable"
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

func handleWebSocket(hub *realtime.Hub, svc *meetings.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		realtime.HandleWebSocket(hub, svc, w, r)
	}
}
