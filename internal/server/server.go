package server

import (
	"bytes"
	"html/template"
	"log"
	"net/http"
	"path/filepath"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/twitch"
	"VLX_Robot/internal/websocket"
)

// Server holds all dependencies for the main public-facing HTTP server.
type Server struct {
	httpServer   *http.Server
	hub          *websocket.Hub
	twitchClient *twitch.Client
	cfg          *config.Config
}

// NewServer creates and initializes a new main HTTP server instance.
func NewServer(cfg *config.Config, hub *websocket.Hub, twitchClient *twitch.Client) *Server {
	mux := http.NewServeMux()
	s := &Server{
		hub:          hub,
		twitchClient: twitchClient,
		cfg:          cfg,
	}
	s.registerRoutes(mux)
	s.httpServer = &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: mux,
	}
	return s
}

// registerRoutes sets up all HTTP endpoints for the main server.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// 1. Dynamic handler for overlay.html (to inject config)
	mux.HandleFunc("/static/overlay.html", s.handleOverlayHTML)

	// 2. Standard file server for all other static assets (CSS, JS, images)
	fileServer := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	// 3. WebSocket endpoint for the overlay client
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		websocket.ServeWs(s.hub, w, r)
	})

	// 4. Twitch EventSub webhook receiver
	mux.HandleFunc("/webhooks/twitch", s.twitchClient.HandleEventSubCallback)

	// 5. Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Note: The /test/alert endpoint is now served by the separate test server.

	log.Println("Enabled public routes:")
	log.Println("  /static/overlay.html -> (Dynamic HTML)")
	log.Println("  /static/* -> (Static file server: CSS, JS, Images)")
	log.Println("  /ws                  -> (WebSocket)")
	log.Println("  /webhooks/twitch     -> (Twitch EventSub)")
	log.Println("  /health              -> (Health Check)")
}

// handleOverlayHTML serves the overlay.html file as a template,
// injecting the configured WebSocket path into it before sending.
func (s *Server) handleOverlayHTML(w http.ResponseWriter, r *http.Request) {
	// Data structure for template injection
	data := struct {
		WebsocketPath string
	}{
		WebsocketPath: s.cfg.Server.WebsocketPath,
	}

	// Define file path
	fp := filepath.Join("static", "overlay.html")

	// Parse the template file
	tmpl, err := template.ParseFiles(fp)
	if err != nil {
		log.Printf("[ERROR] Failed to parse overlay.html template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Execute the template into a buffer to catch errors
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("[ERROR] Failed to execute overlay.html template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Set headers and write the response
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

// ListenAndServe starts the main HTTP server.
func (s *Server) ListenAndServe() error {
	log.Printf("Main HTTP server listening on http://localhost%s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}
