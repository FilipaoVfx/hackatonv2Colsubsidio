package main

import (
	"sync"

	"github.com/gofiber/contrib/websocket"
)

// Hub broadcasts every event to connected dashboards over WebSocket
// (ADR-013 / ADR-016 / RA-005). Dashboard is read-only (RN-004): the Hub never
// reads inbound messages back into the domain.
type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]bool)}
}

func (h *Hub) Add(c *websocket.Conn) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

func (h *Hub) Remove(c *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Broadcast is subscribed to "*": pushes each event to all dashboards.
func (h *Hub) Broadcast(ev Event) {
	h.mu.RLock()
	conns := make([]*websocket.Conn, 0, len(h.clients))
	for c := range h.clients {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	for _, c := range conns {
		if err := c.WriteJSON(ev); err != nil {
			h.Remove(c)
			_ = c.Close()
		}
	}
}
