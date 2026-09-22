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
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		meetings:   make(map[int]map[*Client]bool),
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
func (h *Hub) BroadcastToMeeting(meetingID int, eventType string, data interface{}) {
	event := Event{
		Type: eventType,
		Data: data,
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshaling event: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	if clients, ok := h.meetings[meetingID]; ok {
		for client := range clients {
			select {
			case client.Send <- data.([]byte):
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
