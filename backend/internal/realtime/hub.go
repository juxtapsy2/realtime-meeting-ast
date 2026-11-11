package realtime

import (
	"encoding/json"
	"log"
	"sync"
)

// Event represents a WebSocket event
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Client represents a WebSocket client
type Client struct {
	Hub       *Hub
	Conn      *Connection
	MeetingID int
	Send      chan []byte
}

// Hub manages WebSocket connections
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	meetings   map[int]map[*Client]bool
	paused     map[int]bool
	// audioOwner is the one connection per meeting allowed to feed audio.
	// Nil means nobody may record until a client claims the token, so a
	// second tab on the same meeting can never transcribe duplicate speech.
	audioOwner map[int]*Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		meetings:   make(map[int]map[*Client]bool),
		paused:     make(map[int]bool),
		audioOwner: make(map[int]*Client),
	}
}

// SetMeetingPaused records whether a meeting is paused. While paused, audio
// sent by clients is dropped so no transcription is produced.
func (h *Hub) SetMeetingPaused(id int, paused bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if paused {
		h.paused[id] = true
	} else {
		delete(h.paused, id)
	}
}

// IsMeetingPaused reports whether a meeting is currently paused.
func (h *Hub) IsMeetingPaused(id int) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.paused[id]
}

// ClaimAudio makes c the meeting's sole audio source. A previous holder (if
// any, and if it is a different connection) is told recording.locked and c is
// told recording.unlocked, so exactly one connection records at a time.
// Ownership is claimed explicitly when a client starts capturing: merely
// opening a second connection to watch a meeting never steals the recording
// from the tab that is live.
func (h *Hub) ClaimAudio(meetingID int, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if prev, ok := h.audioOwner[meetingID]; ok && prev != nil && prev != c {
		h.sendEventLocked(prev, "recording.locked")
	}
	h.audioOwner[meetingID] = c
	h.sendEventLocked(c, "recording.unlocked")
}

// IsAudioOwner reports whether c holds the meeting's recording token. It
// fails closed: with no owner, nobody may feed audio.
func (h *Hub) IsAudioOwner(meetingID int, c *Client) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.audioOwner[meetingID] == c
}

// sendEventLocked queues an event for one client without blocking. The
// caller must hold h.mu, which keeps it mutually exclusive with the
// unregister path that closes Send.
func (h *Hub) sendEventLocked(c *Client, eventType string) {
	data, err := json.Marshal(Event{Type: eventType, Data: nil})
	if err != nil {
		log.Printf("Error marshaling event %s: %v", eventType, err)
		return
	}
	select {
	case c.Send <- data:
	default:
		log.Printf("Failed to send %s to client in meeting %d", eventType, c.MeetingID)
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			if h.meetings[client.MeetingID] == nil {
				h.meetings[client.MeetingID] = make(map[*Client]bool)
			}
			h.meetings[client.MeetingID][client] = true
			h.mu.Unlock()
			log.Printf("Client connected to meeting %d", client.MeetingID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
				if h.meetings[client.MeetingID] != nil {
					delete(h.meetings[client.MeetingID], client)
				}
				if h.audioOwner[client.MeetingID] == client {
					delete(h.audioOwner, client.MeetingID)
				}
			}
			h.mu.Unlock()
			log.Printf("Client disconnected from meeting %d", client.MeetingID)

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// BroadcastToMeeting sends an event to all clients in a meeting (implements meetings.Broadcaster)
func (h *Hub) BroadcastToMeeting(meetingID int, eventType string, payload interface{}) {
	event := Event{
		Type: eventType,
		Data: payload,
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshaling event: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	if clients, ok := h.meetings[meetingID]; ok {
		for client := range clients {
			select {
			case client.Send <- eventData:
			default:
				log.Printf("Failed to send to client in meeting %d", meetingID)
			}
		}
	}
}

func (h *Hub) BroadcastToAll(eventType string, data interface{}) {
	event := Event{
		Type: eventType,
		Data: data,
	}

	dataBytes, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshaling event: %v", err)
		return
	}

	h.broadcast <- dataBytes
}
