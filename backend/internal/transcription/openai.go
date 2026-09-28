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
	"sync"
	"time"
)

type OpenAITranscriber struct {
	apiKey    string
	events    chan TranscriptEvent
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	connected bool
	config    Config
	buffer    []byte
}

func NewOpenAITranscriber(apiKey string) (*OpenAITranscriber, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &OpenAITranscriber{
		apiKey: apiKey,
		events: make(chan TranscriptEvent, 100),
		ctx:    ctx,
		cancel: cancel,
		config: Config{
			Language: "en",
			Model:    "whisper-1",
		},
		buffer: make([]byte, 0),
	}, nil
}

func (o *OpenAITranscriber) Start(ctx context.Context, config Config) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if config.Language != "" {
		o.config.Language = config.Language
	}
	if config.Model != "" {
		o.config.Model = config.Model
	}

	o.connected = true
	log.Println("OpenAI transcriber initialized")
	return nil
}

func (o *OpenAITranscriber) WriteAudio(chunk []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.connected {
		return fmt.Errorf("not connected")
	}

	// Buffer audio and transcribe periodically
	o.buffer = append(o.buffer, chunk...)

	// Transcribe when buffer reaches ~5 seconds (assuming 16kHz, 16-bit, mono)
	// 16000 * 2 * 5 = 160000 bytes
	if len(o.buffer) >= 160000 {
		go o.transcribeBuffer()
	}

	return nil
}

func (o *OpenAITranscriber) Events() <-chan TranscriptEvent {
	return o.events
}

func (o *OpenAITranscriber) Close() error {
	o.cancel()

	o.mu.Lock()
	defer o.mu.Unlock()

	// Transcribe any remaining audio
	if len(o.buffer) > 0 {
		o.transcribeBuffer()
	}

	o.connected = false
	return nil
}

func (o *OpenAITranscriber) transcribeBuffer() {
	o.mu.Lock()
	audioData := make([]byte, len(o.buffer))
	copy(audioData, o.buffer)
	o.buffer = o.buffer[:0]
	o.mu.Unlock()

	if len(audioData) == 0 {
		return
	}

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add audio file
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		log.Printf("Error creating form file: %v", err)
		return
	}

	// Write WAV header + audio data
	wavHeader := o.createWAVHeader(len(audioData), int(o.config.SampleRate), 1, 16)
	part.Write(wavHeader)
	part.Write(audioData)

	// Add model field
	writer.WriteField("model", o.config.Model)
	writer.WriteField("language", o.config.Language)
	writer.WriteField("response_format", "verbose_json")

	writer.Close()

	// Make API request
	req, err := http.NewRequestWithContext(o.ctx, "POST", "https://api.openai.com/v1/audio/transcriptions", body)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error calling OpenAI API: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("OpenAI API error: %s", string(respBody))
		return
	}

	var result WhisperResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Error decoding response: %v", err)
		return
	}

	if result.Text != "" {
		segmentID := fmt.Sprintf("seg_%d_%d", time.Now().UnixNano(), len(o.events))
		confidence := 0.9

		event := TranscriptEvent{
			SegmentID:  segmentID,
			Text:       result.Text,
			StartTime:  result.Start,
			EndTime:    result.End,
			Confidence: &confidence,
			Final:      true,
		}

		select {
		case o.events <- event:
		default:
			log.Println("Event channel full, dropping event")
		}
	}
}

type WhisperResponse struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func (o *OpenAITranscriber) createWAVHeader(dataSize, sampleRate, channels, bitsPerSample int) []byte {
	return createWAVHeader(dataSize, sampleRate, channels, bitsPerSample)
}

func createWAVHeader(dataSize, sampleRate, channels, bitsPerSample int) []byte {
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	fileSize := 36 + dataSize

	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	header[4] = byte(fileSize)
	header[5] = byte(fileSize >> 8)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	copy(header[16:20], []byte{16, 0, 0, 0}) // chunk size
	copy(header[20:22], []byte{1, 0})        // PCM format
	header[22] = byte(channels)
	copy(header[24:28], []byte{byte(sampleRate), byte(sampleRate >> 8), 0, 0})
	copy(header[28:32], []byte{byte(byteRate), byte(byteRate >> 8), 0, 0})
	copy(header[32:34], []byte{byte(blockAlign), 0})
	copy(header[34:36], []byte{byte(bitsPerSample), 0})
	copy(header[36:40], "data")
	header[40] = byte(dataSize)
	header[41] = byte(dataSize >> 8)

	return header
}
