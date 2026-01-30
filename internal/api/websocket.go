package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// WebSocketManager manages WebSocket connections and broadcasts updates
type WebSocketManager struct {
	clients    map[*WebSocketClient]bool
	broadcast  chan WebSocketMessage
	register   chan *WebSocketClient
	unregister chan *WebSocketClient
	mu         sync.RWMutex
	upgrader   websocket.Upgrader
	ctx        context.Context
	cancel     context.CancelFunc
}

// WebSocketClient represents a single WebSocket connection
type WebSocketClient struct {
	manager *WebSocketManager
	conn    *websocket.Conn
	send    chan WebSocketMessage
	userID  string
}

// WebSocketMessage represents a message sent over WebSocket
type WebSocketMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
	Time    time.Time   `json:"time"`
}

// NewWebSocketManager creates a new WebSocket manager
func NewWebSocketManager() *WebSocketManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &WebSocketManager{
		clients:    make(map[*WebSocketClient]bool),
		broadcast:  make(chan WebSocketMessage, 256),
		register:   make(chan *WebSocketClient),
		unregister: make(chan *WebSocketClient),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins in development
				// In production, this should be restricted
				return true
			},
		},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins the WebSocket manager event loop
func (wsm *WebSocketManager) Start() {
	go wsm.run()
}

// Stop shuts down the WebSocket manager
func (wsm *WebSocketManager) Stop() {
	wsm.cancel()
	wsm.mu.Lock()
	for client := range wsm.clients {
		close(client.send)
		delete(wsm.clients, client)
	}
	wsm.mu.Unlock()
}

// run is the main event loop for the WebSocket manager
func (wsm *WebSocketManager) run() {
	for {
		select {
		case client := <-wsm.register:
			wsm.mu.Lock()
			wsm.clients[client] = true
			wsm.mu.Unlock()
			log.Info().Str("user_id", client.userID).Msg("WebSocket client connected")

		case client := <-wsm.unregister:
			wsm.mu.Lock()
			if _, ok := wsm.clients[client]; ok {
				delete(wsm.clients, client)
				close(client.send)
			}
			wsm.mu.Unlock()
			log.Info().Str("user_id", client.userID).Msg("WebSocket client disconnected")

		case message := <-wsm.broadcast:
			wsm.mu.RLock()
			clients := make([]*WebSocketClient, 0, len(wsm.clients))
			for client := range wsm.clients {
				clients = append(clients, client)
			}
			wsm.mu.RUnlock()

			for _, client := range clients {
				select {
				case client.send <- message:
				default:
					// Client's send channel is full, close it
					wsm.mu.Lock()
					delete(wsm.clients, client)
					close(client.send)
					wsm.mu.Unlock()
				}
			}

		case <-wsm.ctx.Done():
			return
		}
	}
}

// HandleWebSocket upgrades HTTP connection to WebSocket
func (wsm *WebSocketManager) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract user from context if authenticated
	userID := "anonymous"
	if user, ok := GetUser(r.Context()); ok {
		userID = user.ID
	}

	// Check if response writer supports hijacking
	if _, ok := w.(http.Hijacker); !ok {
		log.Error().Msg("WebSocket: response does not implement http.Hijacker")
		http.Error(w, "WebSocket not supported", http.StatusInternalServerError)
		return
	}

	conn, err := wsm.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	client := &WebSocketClient{
		manager: wsm,
		conn:    conn,
		send:    make(chan WebSocketMessage, 256),
		userID:  userID,
	}

	wsm.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

// BroadcastTunnelUpdate sends tunnel status update to all clients
func (wsm *WebSocketManager) BroadcastTunnelUpdate(tunnelID string, status interface{}) {
	msg := WebSocketMessage{
		Type: "tunnel_update",
		Payload: map[string]interface{}{
			"tunnelId": tunnelID,
			"status":   status,
		},
		Time: time.Now(),
	}

	select {
	case wsm.broadcast <- msg:
	case <-time.After(100 * time.Millisecond):
		log.Warn().Msg("WebSocket broadcast channel full, dropping message")
	}
}

// BroadcastSystemMetrics sends system metrics to all clients
func (wsm *WebSocketManager) BroadcastSystemMetrics(metrics interface{}) {
	msg := WebSocketMessage{
		Type:    "system_metrics",
		Payload: metrics,
		Time:    time.Now(),
	}

	select {
	case wsm.broadcast <- msg:
	case <-time.After(100 * time.Millisecond):
		log.Warn().Msg("WebSocket broadcast channel full, dropping message")
	}
}

// readPump handles incoming messages from the client
func (c *WebSocketClient) readPump() {
	defer func() {
		c.manager.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512) // Max message size
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error().Err(err).Str("user_id", c.userID).Msg("WebSocket error")
			}
			break
		}
		// We don't process incoming messages for now (one-way updates)
		// But this loop is needed to detect disconnections
	}
}

// writePump handles outgoing messages to the client
func (c *WebSocketClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Channel closed
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			data, err := json.Marshal(message)
			if err != nil {
				log.Error().Err(err).Msg("Failed to marshal WebSocket message")
				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Error().Err(err).Str("user_id", c.userID).Msg("WebSocket write error")
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.manager.ctx.Done():
			return
		}
	}
}

// GetClientCount returns the number of connected clients
func (wsm *WebSocketManager) GetClientCount() int {
	wsm.mu.RLock()
	defer wsm.mu.RUnlock()
	return len(wsm.clients)
}
