package realtime

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/user/realtime-meeting-ast/backend/internal/meetings"
	"github.com/user/realtime-meeting-ast/backend/internal/transcription"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

type Connection struct {
	WS     *websocket.Conn
	Send   chan []byte
	Client *Client
}

type AudioMessage struct {
	Type    string `json:"type"`
	Payload []byte `json:"payload"`
}

type CommandMessage struct {
	Type    string `json:"type"`
	MeetingID int  `json:"meeting_id"`
}

func HandleWebSocket(hub *Hub, meetingSvc *meetings.Service, w http.ResponseWriter, r *http.Request) {
	// Extract meeting ID from URL
	meetingID, err := extractMeetingID(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}

	// Verify meeting exists
	_, err = meetingSvc.GetMeetingByID(meetingID)
	if err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	// Create client
	client := &Client{
		Hub:       hub,
		Conn:      &Connection{WS: conn, Send: make(chan []byte, 256)},
		MeetingID: meetingID,
		Send:      make(chan []byte, 256),
	}
	client.Conn.Client = client

	// Register client with hub
	hub.Register(client)

	// Start transcriber for this meeting
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	transcriber, err := transcription.NewProvider(transcription.ProviderDeepgram, "")
	if err != nil {
		log.Printf("Failed to create transcriber: %v", err)
		conn.Close()
		return
	}

	config := transcription.Config{
		Language:       "en-US",
		SampleRate:     16000,
		Channels:       1,
		Encoding:       "linear16",
		Model:          "nova-2",
		InterimResults: true,
	}

	if err := transcriber.Start(ctx, config); err != nil {
		log.Printf("Failed to start transcriber: %v", err)
		conn.Close()
		return
	}
	defer transcriber.Close()

	// Start goroutines for reading/writing
	go client.writePump()
	go client.readPump(transcriber, meetingSvc)
	go client.transcriptPump(transcriber, meetingSvc, hub, meetingID)
}

func (c *Client) readPump(transcriber transcription.Transcriber, meetingSvc *meetings.Service) {
	defer func() {
		c.Hub.Unregister(c)
		c.Conn.WS.Close()
	}()

	c.Conn.WS.SetReadLimit(512 * 1024) // 512KB
	c.Conn.WS.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.WS.SetPongHandler(func(string) error {
		c.Conn.WS.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.WS.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Try to parse as command first
		var cmd CommandMessage
		if err := json.Unmarshal(message, &cmd); err == nil && cmd.Type != "" {
			c.handleCommand(cmd, meetingSvc)
			continue
		}

		// Try to parse as audio message
		var audioMsg AudioMessage
		if err := json.Unmarshal(message, &audioMsg); err == nil && audioMsg.Type == "audio" {
			// Forward audio to transcriber
			if err := transcriber.WriteAudio(audioMsg.Payload); err != nil {
				log.Printf("Error writing audio to transcriber: %v", err)
			}
			continue
		}

		// If it's raw binary data, treat as audio
		if len(message) > 0 {
			if err := transcriber.WriteAudio(message); err != nil {
				log.Printf("Error writing audio to transcriber: %v", err)
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.WS.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.WS.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WS.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.WS.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.WS.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WS.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) transcriptPump(transcriber transcription.Transcriber, meetingSvc *meetings.Service, hub *Hub, meetingID int) {
	for event := range transcriber.Events() {
		// Set meeting ID
		event.MeetingID = strconv.Itoa(meetingID)

		// Broadcast to all clients in the meeting
		hub.BroadcastToMeeting(meetingID, Event{
			Type: func() string {
				if event.Final {
					return "transcript.final"
				}
				return "transcript.partial"
			}(),
			Data: event,
		})

		// Persist final transcript segments
		if event.Final {
			if err := meetingSvc.SaveTranscriptSegment(meetingID, &event); err != nil {
				log.Printf("Error saving transcript segment: %v", err)
			}
		}
	}
}

func (c *Client) handleCommand(cmd CommandMessage, meetingSvc *meetings.Service) {
	switch cmd.Type {
	case "start_meeting":
		if err := meetingSvc.StartMeeting(cmd.MeetingID); err != nil {
			log.Printf("Error starting meeting: %v", err)
		}
	case "end_meeting":
		if err := meetingSvc.EndMeeting(cmd.MeetingID); err != nil {
			log.Printf("Error ending meeting: %v", err)
		}
	default:
		log.Printf("Unknown command: %s", cmd.Type)
	}
}

func extractMeetingID(path string) (int, error) {
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if id, err := strconv.Atoi(part); err == nil {
			return id, nil
		}
	}
	return 0, strconv.ErrSyntax
}
