package transcription

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type DeepgramTranscriber struct {
	apiKey      string
	conn        *websocket.Conn
	events      chan TranscriptEvent
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	connected   bool
	config      Config
	audioChunks int
}

func NewDeepgramTranscriber(apiKey string) (*DeepgramTranscriber, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Deepgram API key is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &DeepgramTranscriber{
		apiKey: apiKey,
		events: make(chan TranscriptEvent, 100),
		ctx:    ctx,
		cancel: cancel,
		config: Config{
			Language:       "en-US",
			SampleRate:     16000,
			Channels:       1,
			Encoding:       "linear16",
			Model:          "nova-2",
			InterimResults: true,
		},
	}, nil
}

func (d *DeepgramTranscriber) Start(ctx context.Context, config Config) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if config.Language != "" {
		d.config.Language = config.Language
	}
	if config.SampleRate > 0 {
		d.config.SampleRate = config.SampleRate
	}
	if config.Model != "" {
		d.config.Model = config.Model
	}

	d.ctx = ctx
	d.connected = false

	log.Println("Deepgram transcriber initialized (lazy connect)")
	return nil
}

func (d *DeepgramTranscriber) ensureConnected() error {
	if d.connected {
		return nil
	}

	log.Println("Deepgram: connecting...")

	u, err := url.Parse("wss://api.deepgram.com/v1/listen")
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("model", d.config.Model)
	q.Set("language", d.config.Language)
	q.Set("sample_rate", fmt.Sprintf("%d", d.config.SampleRate))
	q.Set("channels", fmt.Sprintf("%d", d.config.Channels))
	q.Set("encoding", d.config.Encoding)
	if d.config.InterimResults {
		q.Set("interim_results", "true")
		q.Set("endpointing", "300")
		q.Set("utterance_end_ms", "1000")
	}
	u.RawQuery = q.Encode()

	header := http.Header{}
	header.Set("Authorization", "Token "+d.apiKey)

	conn, _, err := websocket.DefaultDialer.DialContext(d.ctx, u.String(), header)
	if err != nil {
		return fmt.Errorf("failed to connect to Deepgram: %w", err)
	}

	d.conn = conn
	d.connected = true

	go d.readResponses()

	log.Println("Connected to Deepgram WebSocket")
	return nil
}

func (d *DeepgramTranscriber) WriteAudio(chunk []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.ensureConnected(); err != nil {
		return err
	}

	if d.audioChunks == 0 {
		log.Printf("Deepgram: first audio chunk received (%d bytes)", len(chunk))
	}
	d.audioChunks++

	return d.conn.WriteMessage(websocket.BinaryMessage, chunk)
}

func (d *DeepgramTranscriber) Events() <-chan TranscriptEvent {
	return d.events
}

func (d *DeepgramTranscriber) Close() error {
	d.cancel()

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.conn != nil {
		d.connected = false
		return d.conn.Close()
	}
	return nil
}

func (d *DeepgramTranscriber) readResponses() {
	defer close(d.events)

	for {
		select {
		case <-d.ctx.Done():
			return
		default:
			_, message, err := d.conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Println("Deepgram WebSocket closed normally")
					return
				}
				log.Printf("Error reading from Deepgram: %v", err)
				return
			}

			var response DeepgramResponse
			if err := json.Unmarshal(message, &response); err != nil {
				log.Printf("Error parsing Deepgram response: %v", err)
				continue
			}

			d.processResponse(response)
		}
	}
}

type DeepgramResponse struct {
	Channel struct {
		Alternatives []struct {
			Transcript string `json:"transcript"`
			Confidence float64 `json:"confidence"`
			Words      []struct {
				Word     string  `json:"word"`
				Start    float64 `json:"start"`
				End      float64 `json:"end"`
				Confidence float64 `json:"confidence"`
			} `json:"words"`
		} `json:"alternatives"`
	} `json:"channel"`
	IsFinal    bool    `json:"is_final"`
	SpeechFinal bool   `json:"speech_final"`
	Duration   float64 `json:"duration"`
	StartTime  float64 `json:"start"`
}

func (d *DeepgramTranscriber) processResponse(response DeepgramResponse) {
	if len(response.Channel.Alternatives) == 0 {
		return
	}

	alternative := response.Channel.Alternatives[0]
	if alternative.Transcript == "" {
		return
	}

	segmentID := fmt.Sprintf("seg_%d_%d", time.Now().UnixNano(), len(d.events))

	event := TranscriptEvent{
		SegmentID:  segmentID,
		Text:       alternative.Transcript,
		StartTime:  response.StartTime,
		EndTime:    response.StartTime + response.Duration,
		Confidence: &alternative.Confidence,
		Final:      response.IsFinal,
	}

	select {
	case d.events <- event:
	default:
		log.Println("Event channel full, dropping event")
	}
}
