package websocket

import "log"

// Hub manages the set of active clients and broadcasts
// messages to all connected clients.
type Hub struct {
	// Registered clients. The map is used as a "set" for
	// quick registration checks.
	clients map[*Client]bool

	// Inbound messages (from Twitch/YouTube) to be sent to clients.
	// This is the channel you use to SEND alerts.
	Broadcast chan []byte

	// Register requests from new clients.
	register chan *Client

	// Unregister requests from disconnected clients.
	unregister chan *Client
}

// NewHub creates a new Hub instance, ready to use.
func NewHub() *Hub {
	return &Hub{
		Broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

// Run starts the hub. It must be run as a goroutine.
// It listens on its channels and manages connections.
func (h *Hub) Run() {
	for {
		// The 'select' statement waits for activity on one of the channels.
		select {
		// Case 1: A new client has connected
		case client := <-h.register:
			h.clients[client] = true
			log.Println("[INFO] : New WebSocket client registered.")

		// Case 2: A client has disconnected
		case client := <-h.unregister:
			// Check if the client is still registered (for safety)
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client) // Remove it from the map
				close(client.send)         // Close its 'send' channel
				log.Println("[INFO] : WebSocket client unregistered.")
			}

		// Case 3: There is a new message to send to everyone
		case message := <-h.Broadcast:
			// Iterate over all registered clients
			for client := range h.clients {
				// Send the message to the client's 'send' channel.
				// We use a 'select' with 'default' to prevent
				// a slow or blocked client from stalling the entire hub.
				select {
				case client.send <- message:
					// Message sent successfully
				default:
					// The client's buffer is full. It's likely
					// disconnected or blocked. We remove it.
					log.Printf("[WARN] : Client buffer full, forcing unregister.")
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}
