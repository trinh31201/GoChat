package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/websocket"
	
	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
	"github.com/yourusername/chat-app/internal/service"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for development
		// In production, check the origin properly
		return true
	},
}

// Client represents a connected WebSocket client
type Client struct {
	ID       int64
	Username string
	Conn     *websocket.Conn
	Send     chan []byte
	Hub      *Hub
	RoomID   int64
}

// safeSend safely sends a message to a client's channel with panic recovery
func (c *Client) safeSend(message []byte) bool {
	defer func() {
		if r := recover(); r != nil {
			c.Hub.log.Warnf("Recovered from panic sending to client %s (user_id=%d): %v", c.Username, c.ID, r)
		}
	}()

	select {
	case c.Send <- message:
		return true
	default:
		// Channel is full
		return false
	}
}

// Hub maintains active WebSocket connections
type Hub struct {
	// Registered clients by room
	rooms map[int64]map[*Client]bool
	
	// Register requests from clients
	register chan *Client
	
	// Unregister requests from clients
	unregister chan *Client
	
	// Mutex for concurrent access
	mu sync.RWMutex
	
	// Services
	chatService *service.ChatService
	roomService *service.RoomService
	
	// Logger
	log *log.Helper
}

// WebSocketMessage represents messages between client and server
type WebSocketMessage struct {
	Type    string          `json:"type"`
	RoomID  int64          `json:"room_id,omitempty"`
	Content string          `json:"content,omitempty"`
	Token   string          `json:"token,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// NewHub creates a new WebSocket hub
func NewHub(chatService *service.ChatService, roomService *service.RoomService, logger log.Logger) *Hub {
	return &Hub{
		rooms:       make(map[int64]map[*Client]bool),
		register:    make(chan *Client, 100),
		unregister:  make(chan *Client, 100),
		chatService: chatService,
		roomService: roomService,
		log:         log.NewHelper(logger),
	}
}

// Run starts the hub's event loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.log.Infof("Hub received register request: user_id=%d, username=%s, room_id=%d", client.ID, client.Username, client.RoomID)
			h.mu.Lock()
			if h.rooms[client.RoomID] == nil {
				h.rooms[client.RoomID] = make(map[*Client]bool)
				h.log.Infof("Created new room %d in hub", client.RoomID)
			}
			h.rooms[client.RoomID][client] = true
			clientCount := len(h.rooms[client.RoomID])
			h.mu.Unlock()
			
			h.log.Infof("Client %s joined room %d (total clients in room: %d)", client.Username, client.RoomID, clientCount)
			
			// Send join notification to room (direct broadcast)
			joinMsg := map[string]interface{}{
				"type":     "user_joined",
				"username": client.Username,
				"user_id":  client.ID,
				"room_id":  client.RoomID,
			}
			msgBytes, _ := json.Marshal(joinMsg)
			
			// Broadcast directly to all clients in room
			go func(roomClients map[*Client]bool, message []byte) {
				h.log.Infof("Broadcasting user_joined to %d clients", len(roomClients))
				for roomClient := range roomClients {
					go func(cl *Client) {
						if cl.safeSend(message) {
							h.log.Infof("Join notification sent to %s", cl.Username)
						} else {
							h.log.Warnf("Failed to send join notification to %s", cl.Username)
						}
					}(roomClient)
				}
			}(h.rooms[client.RoomID], msgBytes)
			
		case client := <-h.unregister:
			h.mu.Lock()
			var remainingClients map[*Client]bool
			if clients, ok := h.rooms[client.RoomID]; ok {
				if _, ok := clients[client]; ok {
					delete(clients, client)
					close(client.Send)
					if len(clients) == 0 {
						delete(h.rooms, client.RoomID)
					} else {
						// Copy remaining clients for broadcasting
						remainingClients = make(map[*Client]bool)
						for cl := range clients {
							remainingClients[cl] = true
						}
					}
				}
			}
			h.mu.Unlock()
			
			h.log.Infof("Client %s left room %d", client.Username, client.RoomID)
			
			// Send leave notification to remaining clients (direct broadcast)
			if len(remainingClients) > 0 {
				leaveMsg := map[string]interface{}{
					"type":     "user_left",
					"username": client.Username,
					"user_id":  client.ID,
					"room_id":  client.RoomID,
				}
				msgBytes, _ := json.Marshal(leaveMsg)
				
				// Broadcast directly to remaining clients
				go func(clients map[*Client]bool, message []byte) {
					h.log.Infof("Broadcasting user_left to %d remaining clients", len(clients))
					for remainingClient := range clients {
						go func(cl *Client) {
							if cl.safeSend(message) {
								h.log.Infof("Leave notification sent to %s", cl.Username)
							} else {
								h.log.Warnf("Failed to send leave notification to %s", cl.Username)
							}
						}(remainingClient)
					}
				}(remainingClients, msgBytes)
			}
		}
	}
}

// HandleWebSocket handles WebSocket connections
func HandleWebSocket(hub *Hub, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Upgrade HTTP connection to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			hub.log.Errorf("WebSocket upgrade failed: %v", err)
			return
		}
		
		// Create client
		client := &Client{
			Conn: conn,
			Send: make(chan []byte, 256),
			Hub:  hub,
		}
		
		// Start client goroutines
		go client.writePump()
		go client.readPump(jwtSecret)
	}
}

// readPump handles incoming messages from the client
func (c *Client) readPump(jwtSecret string) {
	defer func() {
		if c.ID != 0 && c.RoomID != 0 {
			c.Hub.unregister <- c
		}
		c.Conn.Close()
	}()
	
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	
	for {
		var msg WebSocketMessage
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Hub.log.Errorf("WebSocket error: %v", err)
			}
			break
		}
		
		switch msg.Type {
		case "auth":
			// Authenticate the client
			if err := c.authenticate(msg.Token, jwtSecret); err != nil {
				c.sendError("Authentication failed")
				return
			}
			c.sendSuccess("Authenticated successfully")
			
		case "join_room":
			// Join a room
			if c.ID == 0 {
				c.sendError("Please authenticate first")
				continue
			}
			
			if err := c.joinRoom(msg.RoomID); err != nil {
				c.sendError(fmt.Sprintf("Failed to join room: %v", err))
				continue
			}
			
		case "send_message":
			// Send a message to the room
			if c.ID == 0 || c.RoomID == 0 {
				c.sendError("Please authenticate and join a room first")
				continue
			}
			
			if err := c.sendMessage(msg.Content); err != nil {
				c.sendError(fmt.Sprintf("Failed to send message: %v", err))
				continue
			}
			
		case "leave_room":
			// Leave the current room
			if c.RoomID != 0 {
				c.Hub.unregister <- c
				c.RoomID = 0
				c.sendSuccess("Left room")
			}
			
		case "ping":
			// Respond to ping
			c.safeSend([]byte(`{"type":"pong"}`))
		}
	}
}

// writePump handles outgoing messages to the client
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()
	
	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			
			c.Conn.WriteMessage(websocket.TextMessage, message)
			
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// authenticate validates the JWT token and sets client info
func (c *Client) authenticate(tokenString string, jwtSecret string) error {
	if tokenString == "" {
		return fmt.Errorf("missing token")
	}
	
	// Parse JWT token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(jwtSecret), nil
	})
	
	if err != nil || !token.Valid {
		return fmt.Errorf("invalid token")
	}
	
	// Extract user info from token
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if userID, ok := claims["user_id"].(float64); ok {
			c.ID = int64(userID)
		}
		if username, ok := claims["username"].(string); ok {
			c.Username = username
		}
	}
	
	if c.ID == 0 || c.Username == "" {
		return fmt.Errorf("invalid token claims")
	}
	
	return nil
}

// joinRoom adds the client to a room
func (c *Client) joinRoom(roomID int64) error {
	c.Hub.log.Infof("ENTER joinRoom: user_id=%d, username=%s, room_id=%d", c.ID, c.Username, roomID)
	
	if roomID <= 0 {
		c.Hub.log.Errorf("joinRoom FAILED: invalid room ID %d for user_id=%d", roomID, c.ID)
		return fmt.Errorf("invalid room ID")
	}
	
	// Debug: Log the user ID being used
	c.Hub.log.Infof("WebSocket joinRoom: user_id=%d trying to join room_id=%d", c.ID, roomID)
	
	// Check if user is a member of the room using the service
	ctx := context.Background()
	ctx = context.WithValue(ctx, "user_id", c.ID)
	
	// Verify room access (the service will check membership)
	room, err := c.Hub.roomService.GetRoom(ctx, &chatV1.GetRoomRequest{
		Id: roomID,
	})
	if err != nil {
		c.Hub.log.Errorf("WebSocket joinRoom failed: user_id=%d, room_id=%d, error=%v", c.ID, roomID, err)
		return fmt.Errorf("cannot access room: %v", err)
	}
	
	// Leave current room if in one
	if c.RoomID != 0 {
		c.Hub.unregister <- c
	}
	
	// Join new room
	c.RoomID = roomID
	c.Hub.log.Infof("Sending client to register channel: user_id=%d, room_id=%d", c.ID, roomID)
	c.Hub.register <- c
	
	// Send room info to client
	roomInfo := map[string]interface{}{
		"type":    "room_joined",
		"room_id": room.Id,
		"room":    room,
	}
	msgBytes, _ := json.Marshal(roomInfo)
	c.safeSend(msgBytes)
	
	c.Hub.log.Infof("EXIT joinRoom: user_id=%d successfully joined room_id=%d", c.ID, roomID)
	return nil
}

// GetRoomClients returns a copy of all clients in a room (thread-safe)
func (h *Hub) GetRoomClients(roomID int64) map[*Client]bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	// Return copy to avoid race conditions
	clients := make(map[*Client]bool)
	if roomClients, exists := h.rooms[roomID]; exists {
		for client := range roomClients {
			clients[client] = true
		}
	}
	
	h.log.Infof("GetRoomClients: room %d has %d clients", roomID, len(clients))
	return clients
}

// sendMessage sends a message to the room
func (c *Client) sendMessage(content string) error {
	if content == "" {
		return fmt.Errorf("empty message")
	}
	
	// Use the chat service to send the message
	ctx := context.Background()
	ctx = context.WithValue(ctx, "user_id", c.ID)
	ctx = context.WithValue(ctx, "username", c.Username)
	
	msg, err := c.Hub.chatService.SendMessage(ctx, &chatV1.SendMessageRequest{
		RoomId:  c.RoomID,
		Content: content,
		Type:    "text",
	})
	if err != nil {
		c.Hub.log.Errorf("ChatService.SendMessage failed: %v", err)
		return err
	}
	
	// Debug: Log what we received from chat service
	c.Hub.log.Infof("Message created: id=%d, room_id=%d, user_id=%d, username=%s", 
		msg.Id, msg.RoomId, msg.UserId, msg.Username)
	
	// Broadcast message to all clients in the room
	msgData := map[string]interface{}{
		"type":       "new_message",
		"message_id": msg.Id,
		"room_id":    msg.RoomId,
		"user_id":    msg.UserId,
		"username":   msg.Username,
		"content":    msg.Content,
		"msg_type":   msg.Type,
		"created_at": msg.CreatedAt,
	}
	msgBytes, _ := json.Marshal(msgData)
	
	// Debug: Log broadcasting details
	c.Hub.log.Infof("Broadcasting message to room %d: sender=%s, content=%s", c.RoomID, c.Username, content)
	
	// Get room clients for direct broadcasting
	roomClients := c.Hub.GetRoomClients(c.RoomID)
	c.Hub.log.Infof("Room %d has %d connected clients", c.RoomID, len(roomClients))
	
	// Broadcast directly to all clients in parallel
	go func(clients map[*Client]bool, message []byte) {
		sentCount := 0
		for client := range clients {
			go func(cl *Client) {
				if cl.safeSend(message) {
					c.Hub.log.Infof("Message sent to client %s (user_id=%d)", cl.Username, cl.ID)
					sentCount++
				} else {
					// Client's send channel is full, remove it
					c.Hub.log.Warnf("Client %s (user_id=%d) send channel full, removing client", cl.Username, cl.ID)
					c.Hub.unregister <- cl
				}
			}(client)
		}
		c.Hub.log.Infof("Direct broadcast complete: attempted to send to %d clients in room %d", len(clients), c.RoomID)
	}(roomClients, msgBytes)
	
	return nil
}

// sendError sends an error message to the client
func (c *Client) sendError(message string) {
	errMsg := map[string]interface{}{
		"type":    "error",
		"message": message,
	}
	msgBytes, _ := json.Marshal(errMsg)
	c.safeSend(msgBytes)
}

// sendSuccess sends a success message to the client
func (c *Client) sendSuccess(message string) {
	successMsg := map[string]interface{}{
		"type":    "success",
		"message": message,
	}
	msgBytes, _ := json.Marshal(successMsg)
	c.safeSend(msgBytes)
}