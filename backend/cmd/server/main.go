package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/meetings"
	"github.com/user/realtime-meeting-ast/backend/internal/realtime"
	"github.com/user/realtime-meeting-ast/backend/internal/storage"
	"github.com/user/realtime-meeting-ast/backend/internal/transcription"
)

func main() {
	// Load configuration from environment
	cfg := loadConfig()

	// Initialize storage
	db, err := storage.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := db.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize repositories
	meetingRepo := meetings.NewRepository(db)

	// Initialize STT provider
	sttProvider, err := transcription.NewProvider(cfg.STTProvider, cfg.STTAPIKey)
	if err != nil {
		log.Fatalf("Failed to initialize STT provider: %v", err)
	}

	// Initialize realtime hub
	hub := realtime.NewHub()
	go hub.Run()

	// Initialize meeting service
	meetingService := meetings.NewService(meetingRepo, hub, sttProvider)

	// Setup HTTP routes
	mux := http.NewServeMux()

	// REST API
	mux.HandleFunc("/api/meetings", handleMeetings(meetingService))
	mux.HandleFunc("/api/meetings/", handleMeetingByID(meetingService))

	// WebSocket endpoint
	mux.HandleFunc("/ws/meeting/", handleWebSocket(hub, meetingService))

	// CORS middleware
	handler := corsMiddleware(mux)

	// Create server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	go func() {
		log.Printf("Server starting on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Graceful shutdown
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
}

func loadConfig() Config {
	return Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://localhost:5432/meeting_ast?sslmode=disable"),
		STTProvider: getEnv("STT_PROVIDER", "deepgram"),
		STTAPIKey:   os.Getenv("STT_API_KEY"),
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

func handleWebSocket(hub *realtime.Hub, svc *meetings.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		realtime.HandleWebSocket(hub, svc, w, r)
	}
}
