package websocket

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the client.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the client.
	// Must be greater than pingPeriod.
	pongWait = 60 * time.Second

	// Send pings to client with this period.
	// Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from client.
	maxMessageSize = 512
)

// upgrader handles upgrading the HTTP connection to a WebSocket connection.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// NOTE: In production, you should check the origin!
	// For now, we allow any origin (necessary for OBS).
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Client is a wrapper for a WebSocket connection and the Hub.
type Client struct {
	hub *Hub

	// The WebSocket connection itself.
	conn *websocket.Conn

	// Buffered channel for outbound messages.
	// The Hub sends messages to this channel.
	send chan []byte
}

// readPump pumps messages from the client (OBS) to the hub.
// In this case, we don't expect messages, but we use it
// to detect when the client closes the connection.
func (c *Client) readPump() {
	defer func() {
		// When this function exits (due to an error),
		// we unregister from the hub and close the connection.
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	// Set the deadline for receiving a "pong"
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	// When we receive a "pong", update the deadline.
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Infinite loop to read from the connection
	for {
		// ReadMessage blocks until it receives a message or an error.
		// Since we don't expect messages, this will only trigger
		// for pongs (handled above) or errors/disconnections.
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ERROR] : WebSocket read error: %v", err)
			}
			break // Exit loop on error
		}
	}
}

// writePump pumps messages from the hub to the client (OBS).
func (c *Client) writePump() {
	// A "ticker" sends a 'ping' to the client periodically
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		// When this function exits, stop the ticker
		// and close the connection.
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		// Case 1: There's a message to send (from the Hub)
		case message, ok := <-c.send:
			// Set a write deadline
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The 'send' channel was closed (by the Hub)
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Get the "writer" for the next message
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return // Error, exit
			}
			// Write the message
			w.Write(message)

			// If there are more messages in the queue, write them all
			// in the same frame for efficiency.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(<-c.send)
			}

			// Close the writer to send the message
			if err := w.Close(); err != nil {
				return // Error, exit
			}

		// Case 2: It's time to send a "ping" to keep the connection alive
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return // Error, exit (client is likely dead)
			}
		}
	}
}

// ServeWs handles WebSocket requests from the client (OBS).
// This is the function our HTTP server will call.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	// Upgrade the connection from HTTP to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("[ERROR] : Failed to upgrade to WebSocket:", err)
		return
	}

	// Create our Client struct
	client := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 256), // Buffer of 256 messages
	}

	// Register the new client with the Hub
	client.hub.register <- client

	// Start the "pumps" (read and write)
	// Each in its own goroutine, to run in parallel.
	go client.writePump()
	go client.readPump()

	log.Println("[INFO] : New WebSocket connection established.")
}
