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
)

// NewProvider creates a new transcriber based on the provider type
func NewProvider(providerType Provider, apiKey string) (Transcriber, error) {
	switch providerType {
	case ProviderDeepgram:
		return NewDeepgramTranscriber(apiKey)
	case ProviderOpenAI:
		return NewOpenAITranscriber(apiKey)
	default:
		return NewDeepgramTranscriber(apiKey)
	}
}
