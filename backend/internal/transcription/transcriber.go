package transcription

import (
	"context"

	"github.com/user/realtime-meeting-ast/backend/internal/types"
)

// TranscriptEvent is an alias for types.TranscriptEvent
type TranscriptEvent = types.TranscriptEvent

// Transcriber is the interface for speech-to-text providers
type Transcriber interface {
	// Start initializes the transcriber with the given configuration
	Start(ctx context.Context, config Config) error

	// WriteAudio sends audio data to the transcriber
	WriteAudio(chunk []byte) error

	// Events returns a channel of transcript events
	Events() <-chan TranscriptEvent

	// Close stops the transcriber and cleans up resources
	Close() error
}

// ConnectionDropper is implemented by transcribers whose upstream connection
// can be actively released. Dropping tears down the current session
// immediately (useful when a meeting pauses or ends) without preventing a
// future reconnect on the next audio chunk.
type ConnectionDropper interface {
	DropConnection()
}

// Config contains configuration for the transcriber
type Config struct {
	Language       string `json:"language"`
	SampleRate     int    `json:"sample_rate"`
	Channels       int    `json:"channels"`
	Encoding       string `json:"encoding"`
	Model          string `json:"model"`
	InterimResults bool   `json:"interim_results"`
}

// Provider represents the type of STT provider
type Provider string

const (
	ProviderDeepgram Provider = "deepgram"
	ProviderOpenAI   Provider = "openai"
	ProviderGroq     Provider = "groq"
	ProviderWhisper  Provider = "whisper"
	ProviderGoogle   Provider = "google"
)

// NewProvider creates a new transcriber based on the provider type.
// vocab carries domain terminology for phrase biasing and normalization; it is
// currently consumed by the Google provider only.
func NewProvider(providerType Provider, apiKey string, vocab Vocabulary) (Transcriber, error) {
	switch providerType {
	case ProviderDeepgram:
		return NewDeepgramTranscriber(apiKey)
	case ProviderOpenAI:
		return NewOpenAITranscriber(apiKey)
	case ProviderGroq:
		return NewGroqTranscriber(apiKey)
	case ProviderWhisper:
		return NewWhisperTranscriber(apiKey)
	case ProviderGoogle:
		return NewGoogleTranscriber(apiKey, vocab)
	default:
		return NewDeepgramTranscriber(apiKey)
	}
}
