package server

import (
	"encoding/json"
	"net/http"

	"VLX_Robot/internal/websocket"

	"go.uber.org/zap"
)

// TestServer manages the private HTTP server for local testing.
type TestServer struct {
	httpServer *http.Server
	hub        *websocket.Hub
	logger     *zap.Logger
}

// NewTestServer initializes the test server on a specific port.
func NewTestServer(port string, hub *websocket.Hub, logger *zap.Logger) *TestServer {
	mux := http.NewServeMux()
	ts := &TestServer{
		hub:    hub,
		logger: logger,
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
		ts.logger.Warn("Test server received invalid JSON", zap.Error(err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Marshal payload back to JSON bytes for broadcasting
	msgBytes, err := json.Marshal(payload)
	if err != nil {
		ts.logger.Error("Test server JSON marshal error", zap.Error(err))
		http.Error(w, "JSON Marshal error", http.StatusInternalServerError)
		return
	}

	// Broadcast directly to the WebSocket hub
	ts.hub.Broadcast <- msgBytes

	ts.logger.Info("Test alert broadcasted", zap.Any("payload", payload))

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Test alert sent via Private Test Server"))
}

// ListenAndServe starts the test HTTP server.
func (ts *TestServer) ListenAndServe() error {
	ts.logger.Info("Test Server listening", zap.String("address", ts.httpServer.Addr), zap.String("mode", "Local Only"))
	return ts.httpServer.ListenAndServe()
}
