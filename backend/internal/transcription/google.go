package transcription

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	speech "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/speech/apiv2/speechpb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	googleEndpoint = "us-speech.googleapis.com:443"
	googleRegion   = "us"
	googleModel    = "chirp_3"
	// maxChunkBytes is the per-message audio limit for Chirp 3 streaming.
	maxChunkBytes = 25600
)

// googleTranscriber implements the Transcriber interface using Google's V2
// Speech-to-Text streaming with the Chirp 3 model and interim results.
//
// Language handling uses multiple language codes (en-US, vi-VN), so a single
// stream can transcribe code-switched English/Vietnamese speech, tagging each
// result with its detected language.
type googleTranscriber struct {
	apiKey    string
	vocab     Vocabulary
	events    chan TranscriptEvent
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	connected bool
	config    Config

	client       *speech.Client
	stream       speechpb.Speech_StreamingRecognizeClient
	audioCh      chan []byte
	writeDone    chan struct{}
	streamCancel context.CancelFunc
	segCounter   int
}

func NewGoogleTranscriber(apiKey string, vocab Vocabulary) (Transcriber, error) {
	if apiKey != "" {
		log.Println("Google STT: using STT_API_KEY as Google API key")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &googleTranscriber{
		apiKey: apiKey,
		vocab:  vocab,
		events: make(chan TranscriptEvent, 100),
		ctx:    ctx,
		cancel: cancel,
		config: Config{
			Language:       "en-US",
			SampleRate:     16000,
			Channels:       1,
			Encoding:       "linear16",
			InterimResults: true,
		},
		audioCh:   make(chan []byte, 256),
		writeDone: make(chan struct{}),
	}, nil
}

func (g *googleTranscriber) Start(ctx context.Context, config Config) error {
	if config.Language != "" {
		g.config.Language = config.Language
	}
	if config.SampleRate > 0 {
		g.config.SampleRate = config.SampleRate
	}
	g.ctx = ctx

	// Deliberately do NOT connect here. The streaming recognition session is
	// opened lazily by writeLoop on the first audio chunk. Connecting eagerly
	// would start a billed Google STT session for every WebSocket listener,
	// even when no microphone is active.
	go g.writeLoop()
	go g.readLoop()
	return nil
}

// WriteAudio sends raw PCM to the streaming recognizer.
func (g *googleTranscriber) WriteAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}

	select {
	case g.audioCh <- chunk:
		return nil
	case <-g.ctx.Done():
		return g.ctx.Err()
	default:
		log.Println("Google STT: audio channel full, dropping chunk")
		return nil
	}
}

func (g *googleTranscriber) Events() <-chan TranscriptEvent {
	return g.events
}

func (g *googleTranscriber) Close() error {
	g.cancel()

	g.mu.Lock()
	g.connected = false
	g.mu.Unlock()

	close(g.audioCh)
	<-g.writeDone
	if g.streamCancel != nil {
		g.streamCancel()
	}
	if g.client != nil {
		g.client.Close()
	}
	return nil
}

func (g *googleTranscriber) connect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.connected {
		return nil
	}

	log.Printf("Google STT: connecting (lang=%s, model=%s, interim=%v)", g.config.Language, googleModel, g.config.InterimResults)

	opts := []option.ClientOption{
		option.WithEndpoint(googleEndpoint),
		option.WithGRPCDialOption(grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", addr)
		})),
	}
	if g.apiKey != "" {
		opts = append(opts, option.WithAPIKey(g.apiKey))
	}
	if creds := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); creds != "" {
		opts = append(opts, option.WithCredentialsFile(creds))
	}

	client, err := speech.NewClient(g.ctx, opts...)
	if err != nil {
		return err
	}

	streamCtx, streamCancel := context.WithCancel(g.ctx)
	stream, err := client.StreamingRecognize(streamCtx)
	if err != nil {
		streamCancel()
		client.Close()
		return err
	}

	config := &speechpb.RecognitionConfig{
		DecodingConfig: &speechpb.RecognitionConfig_ExplicitDecodingConfig{
			ExplicitDecodingConfig: &speechpb.ExplicitDecodingConfig{
				Encoding:          speechpb.ExplicitDecodingConfig_LINEAR16,
				SampleRateHertz:   int32(g.config.SampleRate),
				AudioChannelCount: int32(g.config.Channels),
			},
		},
		LanguageCodes: []string{g.config.Language, "vi-VN"},
		Model:         googleModel,
		Features: &speechpb.RecognitionFeatures{
			EnableAutomaticPunctuation: true,
		},
	}
	if g.config.Language == "vi-VN" || g.config.Language == "vi" {
		config.LanguageCodes = []string{"vi-VN", "en-US"}
	}

	config.Adaptation = g.buildAdaptation()
	config.TranscriptNormalization = g.buildNormalization()

	streamingConfig := &speechpb.StreamingRecognitionConfig{
		Config: config,
		StreamingFeatures: &speechpb.StreamingRecognitionFeatures{
			InterimResults: g.config.InterimResults,
		},
	}

	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		projectID = projectIDFromCredentials(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	}
	if projectID == "" {
		streamCancel()
		client.Close()
		return fmt.Errorf("Google STT: project ID not configured for Chirp 3 recognizer")
	}

	req := &speechpb.StreamingRecognizeRequest{
		Recognizer: fmt.Sprintf("projects/%s/locations/%s/recognizers/_", projectID, googleRegion),
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: streamingConfig,
		},
	}
	if err := stream.Send(req); err != nil {
		streamCancel()
		client.Close()
		return err
	}

	g.client = client
	g.stream = stream
	g.streamCancel = streamCancel
	g.connected = true

	log.Printf("Google STT: streaming enabled (project=%s, region=%s, langs=%v)", projectID, googleRegion, config.LanguageCodes)
	return nil
}

// buildAdaptation converts the vocabulary hints into SpeechAdaptation phrase
// biasing. Returns nil when there is nothing to bias.
func (g *googleTranscriber) buildAdaptation() *speechpb.SpeechAdaptation {
	if len(g.vocab.Hints) == 0 {
		return nil
	}

	phrases := make([]*speechpb.PhraseSet_Phrase, 0, len(g.vocab.Hints))
	for _, h := range g.vocab.Hints {
		if strings.TrimSpace(h.Phrase) == "" {
			continue
		}
		p := &speechpb.PhraseSet_Phrase{Value: h.Phrase}
		if h.Boost > 0 && h.Boost <= 20 {
			p.Boost = h.Boost
		}
		phrases = append(phrases, p)
	}
	if len(phrases) == 0 {
		return nil
	}

	log.Printf("Google STT: applying %d adaptation phrases", len(phrases))
	return &speechpb.SpeechAdaptation{
		PhraseSets: []*speechpb.SpeechAdaptation_AdaptationPhraseSet{{
			Value: &speechpb.SpeechAdaptation_AdaptationPhraseSet_InlinePhraseSet{
				InlinePhraseSet: &speechpb.PhraseSet{Phrases: phrases},
			},
		}},
	}
}

// buildNormalization converts curated mishearing rewrites into
// TranscriptNormalization entries. Returns nil when there is nothing to fix.
func (g *googleTranscriber) buildNormalization() *speechpb.TranscriptNormalization {
	if len(g.vocab.Normalizations) == 0 {
		return nil
	}

	entries := make([]*speechpb.TranscriptNormalization_Entry, 0, len(g.vocab.Normalizations))
	for _, r := range g.vocab.Normalizations {
		if strings.TrimSpace(r.Search) == "" || strings.TrimSpace(r.Replace) == "" {
			continue
		}
		entries = append(entries, &speechpb.TranscriptNormalization_Entry{
			Search:  r.Search,
			Replace: r.Replace,
		})
	}
	if len(entries) == 0 {
		return nil
	}

	log.Printf("Google STT: applying %d transcript normalization entries", len(entries))
	return &speechpb.TranscriptNormalization{Entries: entries}
}

// openStream (re)establishes the streaming recognizer if not already connected.
func (g *googleTranscriber) openStream() error {
	g.mu.Lock()
	if g.connected {
		g.mu.Unlock()
		return nil
	}
	g.mu.Unlock()

	if err := g.connect(); err != nil {
		return err
	}
	return nil
}

func (g *googleTranscriber) writeLoop() {
	defer close(g.writeDone)

	var pending []byte
	for {
		if pending != nil {
			chunk := pending
			pending = nil

			if err := g.openStream(); err != nil {
				log.Printf("Google STT: connect failed: %v", err)
				pending = chunk
				time.Sleep(time.Second)
				continue
			}

			if err := g.stream.Send(&speechpb.StreamingRecognizeRequest{
				StreamingRequest: &speechpb.StreamingRecognizeRequest_Audio{Audio: chunk},
			}); err != nil {
				log.Printf("Google STT: stream write error: %v", err)
				pending = chunk
				g.dropConnection()
			}
			continue
		}

		select {
		case <-g.ctx.Done():
			return
		case chunk, ok := <-g.audioCh:
			if !ok {
				return
			}

			// Chirp 3 caps each audio message at maxChunkBytes; split oversized
			// chunks from the client.
			if len(chunk) > maxChunkBytes {
				pending = chunk[maxChunkBytes:]
				chunk = chunk[:maxChunkBytes]
			}

			if err := g.openStream(); err != nil {
				log.Printf("Google STT: connect failed: %v", err)
				pending = append(pending, chunk...)
				time.Sleep(time.Second)
				continue
			}

			if err := g.stream.Send(&speechpb.StreamingRecognizeRequest{
				StreamingRequest: &speechpb.StreamingRecognizeRequest_Audio{Audio: chunk},
			}); err != nil {
				log.Printf("Google STT: stream write error: %v", err)
				g.dropConnection()
			}
		}
	}
}

func (g *googleTranscriber) readLoop() {
	defer close(g.events)

	for {
		// The stream is created by writeLoop on the first audio chunk. Until
		// then there is nothing to receive; waitForReconnect returns only once
		// a stream exists, or when the context is done.
		if !g.waitForReconnect() {
			return
		}

		resp, err := g.stream.Recv()
		if err != nil {
			if err == io.EOF || g.ctx.Err() != nil {
				return
			}
			// Canceled is expected after an explicit DropConnection (pause or
			// end meeting) and after writeLoop cancels a failing stream. It is
			// transient: writeLoop re-establishes the session lazily on the
			// next audio chunk.
			if status.Code(err) == codes.Canceled {
				g.dropConnection()
				if !g.waitForReconnect() {
					return
				}
				continue
			}
			log.Printf("Google STT: receive error: %v", err)
			g.dropConnection()
			// writeLoop will reconnect on next audio chunk
			if !g.waitForReconnect() {
				return
			}
			continue
		}

		for _, result := range resp.Results {
			g.processResult(result)
		}
	}
}

func (g *googleTranscriber) waitForReconnect() bool {
	// Livelock-safe: wait until a new stream is set or context ends.
	for {
		select {
		case <-g.ctx.Done():
			return false
		default:
		}
		g.mu.Lock()
		ok := g.connected
		g.mu.Unlock()
		if ok {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (g *googleTranscriber) dropConnection() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.connected {
		g.connected = false
		if g.streamCancel != nil {
			g.streamCancel()
		}
	}
}

// DropConnection actively cancels the current streaming session so the billed
// connection is released immediately (billing is per-minute, so an idle stream
// that only dies on the server-side timeout still costs). Idempotent: a later
// audio chunk triggers a fresh lazy connect via writeLoop.
func (g *googleTranscriber) DropConnection() {
	log.Println("Google STT: dropping connection (pause/end meeting)")
	g.dropConnection()
}

func (g *googleTranscriber) processResult(result *speechpb.StreamingRecognitionResult) {
	if len(result.Alternatives) == 0 {
		return
	}

	alt := result.Alternatives[0]
	text := trimSpace(alt.Transcript)
	if text == "" {
		return
	}

	event := TranscriptEvent{
		SegmentID:  "", // assigned by persistence layer on final
		Text:       text,
		EndTime:    durationSeconds(result.ResultEndOffset),
		Confidence: nil,
		Final:      result.IsFinal,
	}
	if alt.Confidence > 0 {
		conf := float64(alt.Confidence)
		event.Confidence = &conf
	}

	if result.IsFinal {
		g.segCounter++
		event.SegmentID = fmt.Sprintf("seg_%d", g.segCounter)
	}

	select {
	case g.events <- event:
	default:
		log.Println("Google STT: event channel full, dropping event")
	}
}

func durationSeconds(d *durationpb.Duration) float64 {
	if d == nil {
		return 0
	}
	return float64(d.Seconds) + float64(d.Nanos)/1e9
}

// projectIDFromCredentials extracts the project ID from a GCP service-account
// JSON file, falling back to "" if the file can't be read.
func projectIDFromCredentials(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var creds struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return ""
	}
	return creds.ProjectID
}
