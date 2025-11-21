package server

import (
	"encoding/json"
	"log"
	"net/http"

	"VLX_Robot/internal/websocket"
)

// TestServer manages the private HTTP server for local testing.
type TestServer struct {
	httpServer *http.Server
	hub        *websocket.Hub
}

// NewTestServer initializes the test server on a specific port.
func NewTestServer(port string, hub *websocket.Hub) *TestServer {
	mux := http.NewServeMux()
	ts := &TestServer{
		hub: hub,
	}

	// Register the test alert endpoint
	mux.HandleFunc("/test/alert", ts.handleTestAlert)

	ts.httpServer = &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}
	return ts
}

// handleTestAlert processes manual alert triggers via JSON payloads.
func (ts *TestServer) handleTestAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Marshal payload back to JSON bytes for broadcasting
	msgBytes, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "JSON Marshal error", http.StatusInternalServerError)
		return
	}

	// Broadcast directly to the WebSocket hub
	ts.hub.Broadcast <- msgBytes

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Test alert sent via Private Test Server"))
}

// ListenAndServe starts the test HTTP server.
func (ts *TestServer) ListenAndServe() error {
	log.Printf("[INFO] Test Server listening on %s (Local Only)", ts.httpServer.Addr)
	return ts.httpServer.ListenAndServe()
}
