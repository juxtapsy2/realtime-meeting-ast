package transcription

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"sync"
	"time"
)

// whisperTranscriber implements Transcriber against a self-hosted Speaches
// (faster-whisper-server) instance, which exposes an OpenAI-compatible
// /v1/audio/transcriptions endpoint.
//
// The language parameter is intentionally omitted so Whisper auto-detects the
// spoken language per audio chunk (handles Vietnamese / English mixed speech).
type whisperTranscriber struct {
	apiKey    string
	events    chan TranscriptEvent
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	connected bool
	config    Config
	buffer    []byte
	baseURL   string
	model     string
}

const (
	// 16000 Hz * 2 bytes * 5 seconds = 160000 bytes
	whisperBufferSize = 160000
	whisperDefaultURL = "http://localhost:8000"
	// Must match the model loaded by the Speaches server
	whisperModel = "Systran/faster-whisper-large-v3"
)

func NewWhisperTranscriber(apiKey string) (Transcriber, error) {
	ctx, cancel := context.WithCancel(context.Background())

	baseURL := os.Getenv("WHISPER_URL")
	if baseURL == "" {
		baseURL = whisperDefaultURL
	}
	model := os.Getenv("WHISPER_MODEL")
	if model == "" {
		model = whisperModel
	}

	return &whisperTranscriber{
		apiKey:  apiKey,
		events:  make(chan TranscriptEvent, 100),
		ctx:     ctx,
		cancel:  cancel,
		config:  Config{SampleRate: 16000, Channels: 1, Encoding: "linear16"},
		buffer:  make([]byte, 0),
		baseURL: baseURL,
		model:   model,
	}, nil
}

func (w *whisperTranscriber) Start(ctx context.Context, config Config) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.connected = true
	log.Printf("Whisper transcriber initialized (lazy connect to %s)", w.baseURL)
	return nil
}

func (w *whisperTranscriber) WriteAudio(chunk []byte) error {
	w.mu.Lock()
	if !w.connected {
		w.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	w.buffer = append(w.buffer, chunk...)
	full := len(w.buffer) >= whisperBufferSize
	w.mu.Unlock()

	if full {
		go w.transcribeBuffer()
	}
	return nil
}

func (w *whisperTranscriber) Events() <-chan TranscriptEvent {
	return w.events
}

func (w *whisperTranscriber) Close() error {
	w.cancel()

	w.mu.Lock()
	if !w.connected {
		w.mu.Unlock()
		return nil
	}
	w.connected = false
	remaining := len(w.buffer)
	w.mu.Unlock()

	if remaining > 0 {
		w.transcribeBuffer()
	}
	return nil
}

func (w *whisperTranscriber) transcribeBuffer() {
	w.mu.Lock()
	audioData := make([]byte, len(w.buffer))
	copy(audioData, w.buffer)
	w.buffer = w.buffer[:0]
	w.mu.Unlock()

	if len(audioData) == 0 {
		return
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return
	}
	part.Write(createWAVHeader(len(audioData), w.config.SampleRate, w.config.Channels, 16))
	part.Write(audioData)

	// Language intentionally omitted: Whisper auto-detects per chunk.
	writer.WriteField("model", w.model)
	writer.WriteField("response_format", "verbose_json")
	writer.Close()

	url := fmt.Sprintf("%s/v1/audio/transcriptions", w.baseURL)
	req, err := http.NewRequestWithContext(w.ctx, http.MethodPost, url, body)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if w.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+w.apiKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Whisper request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("Whisper API error (status %d): %s", resp.StatusCode, truncate(respBody, 300))
		return
	}

	var result whisperResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Error decoding Whisper response: %v", err)
		return
	}
	w.emitSegments(result)
}

type whisperResponse struct {
	Text     string       `json:"text"`
	Language string       `json:"language"`
	Segments []whisperSeg `json:"segments"`
}

type whisperSeg struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func (w *whisperTranscriber) emitSegments(result whisperResponse) {
	segments := result.Segments
	if len(segments) == 0 && result.Text != "" {
		segments = []whisperSeg{{Start: 0, End: 0, Text: result.Text}}
	}

	for _, seg := range segments {
		text := trimSpace(seg.Text)
		if text == "" {
			continue
		}
		event := TranscriptEvent{
			SegmentID: fmt.Sprintf("seg_%d_%d", time.Now().UnixNano(), len(w.events)),
			Text:      text,
			StartTime: seg.Start,
			EndTime:   seg.End,
			Final:     true,
		}
		select {
		case w.events <- event:
		default:
			log.Println("Event channel full, dropping event")
		}
	}
}
