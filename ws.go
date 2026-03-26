package main

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Hub manages all connected WebSocket clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

var hub = &Hub{
	clients: make(map[*Client]struct{}),
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		c.mu.Lock()
		c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := c.conn.WriteMessage(websocket.TextMessage, msg)
		c.mu.Unlock()
		if err != nil {
			go func(c *Client) {
				h.Unregister(c)
				c.conn.Close()
			}(c)
		}
	}
}

// WSHandler upgrades HTTP to WebSocket connection.
func WSHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &Client{conn: conn}
	hub.Register(client)

	// Send initial game state immediately upon connection.
	if data, err := buildGameStateJSON(); err == nil {
		client.mu.Lock()
		conn.WriteMessage(websocket.TextMessage, data)
		client.mu.Unlock()
	}

	// Read pump: keep connection alive, handle close.
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Ping ticker to keep connection alive.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			client.mu.Lock()
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := conn.WriteMessage(websocket.PingMessage, nil)
			client.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		// Application-level ping: client sends {"type":"ping"}, server replies {"type":"pong"}.
		if string(msg) == `{"type":"ping"}` {
			client.mu.Lock()
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
			client.mu.Unlock()
		}
	}

	hub.Unregister(client)
	conn.Close()
}

// BroadcastGameState builds the current game state and sends it to all clients.
func BroadcastGameState() {
	data, err := buildGameStateJSON()
	if err != nil {
		return
	}
	hub.Broadcast(data)
}
