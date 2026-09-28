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
	"strings"
	"sync"
	"time"
)

// groqTranscriber implements Transcriber using Groq's OpenAI-compatible
// Whisper endpoint. Groq has no true streaming API, so audio is buffered into
// ~5s chunks and each chunk is transcribed as a file upload. Groq's Whisper
// auto-detects the language per chunk, so mixed Vietnamese/English speech is
// handled without a fixed language parameter.
type groqTranscriber struct {
	apiKey      string
	events      chan TranscriptEvent
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	connected   bool
	config      Config
	buffer      []byte
	audioChunks int
}

const (
	// 16000 Hz * 2 bytes * 5 seconds = 160000 bytes
	groqBufferSize = 160000
	groqURL        = "https://api.groq.com/openai/v1/audio/transcriptions"
	// whisper-large-v3: 99+ languages, 10.3% WER; turbo is cheaper but lower quality
	groqDefaultModel = "whisper-large-v3"
)

func NewGroqTranscriber(apiKey string) (Transcriber, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Groq API key is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &groqTranscriber{
		apiKey: apiKey,
		events: make(chan TranscriptEvent, 100),
		ctx:    ctx,
		cancel: cancel,
		config: Config{
			Language:       "", // auto-detect: Groq Whisper picks VI/EN per chunk
			SampleRate:     16000,
			Channels:       1,
			Encoding:       "linear16",
			Model:          groqDefaultModel,
			InterimResults: true,
		},
		buffer: make([]byte, 0),
	}, nil
}

func (g *groqTranscriber) Start(ctx context.Context, config Config) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Only accept a model override that is actually a Whisper model; otherwise
	// the caller's provider-specific default (e.g. "nova-2") would be invalid.
	if config.Model == "whisper-large-v3" || config.Model == "whisper-large-v3-turbo" {
		g.config.Model = config.Model
	}
	g.config.SampleRate = 16000
	g.config.Channels = 1
	g.connected = true
	return nil
}

func (g *groqTranscriber) WriteAudio(chunk []byte) error {
	g.mu.Lock()
	if !g.connected {
		g.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	g.buffer = append(g.buffer, chunk...)
	full := len(g.buffer) >= groqBufferSize
	g.audioChunks++
	g.mu.Unlock()

	if full {
		go g.transcribeBuffer()
	}
	return nil
}

func (g *groqTranscriber) Events() <-chan TranscriptEvent {
	return g.events
}

func (g *groqTranscriber) Close() error {
	g.cancel()

	g.mu.Lock()
	if !g.connected {
		g.mu.Unlock()
		return nil
	}
	g.connected = false
	remaining := len(g.buffer)
	g.mu.Unlock()

	if remaining > 0 {
		g.transcribeBuffer()
	}
	return nil
}

func (g *groqTranscriber) transcribeBuffer() {
	g.mu.Lock()
	audioData := make([]byte, len(g.buffer))
	copy(audioData, g.buffer)
	g.buffer = g.buffer[:0]
	g.mu.Unlock()

	if len(audioData) == 0 {
		return
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return
	}
	part.Write(createWAVHeader(len(audioData), g.config.SampleRate, g.config.Channels, 16))
	part.Write(audioData)

	writer.WriteField("model", g.config.Model)
	// Language intentionally omitted: Whisper auto-detects Vietnamese vs English
	// per chunk, which is what we want for mixed-language meetings.
	writer.WriteField("response_format", "verbose_json")
	writer.Close()

	req, err := http.NewRequestWithContext(g.ctx, http.MethodPost, groqURL, body)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+g.apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("Groq API error (status %d): %s", resp.StatusCode, truncate(respBody, 300))
		return
	}

	var result groqResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}
	g.emitSegments(result)
}

type groqResponse struct {
	Text     string        `json:"text"`
	Language string        `json:"language"`
	Segments []groqSegment `json:"segments"`
}

type groqSegment struct {
	ID    int     `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func (g *groqTranscriber) emitSegments(result groqResponse) {
	segments := result.Segments
	if len(segments) == 0 && result.Text != "" {
		// Fallback: synthesize a single segment when verbose_json omits segments
		segments = []groqSegment{{Start: 0, End: 0, Text: result.Text}}
	}

	for _, seg := range segments {
		text := trimSpace(seg.Text)
		if text == "" {
			continue
		}
		segmentID := fmt.Sprintf("seg_%d_%d", time.Now().UnixNano(), len(g.events))
		event := TranscriptEvent{
			SegmentID: segmentID,
			Text:      text,
			StartTime: seg.Start,
			EndTime:   seg.End,
			Final:     true,
		}
		select {
		case g.events <- event:
		default:
		}
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}
