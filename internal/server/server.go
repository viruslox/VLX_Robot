package server

import (
	"html/template"
	"log"
	"net/http"
	"path"
	"path/filepath"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/twitch"
	"VLX_Robot/internal/websocket"
)

// Server manages the public-facing HTTP server dependencies and routes.
type Server struct {
	httpServer   *http.Server
	hub          *websocket.Hub
	twitchClient *twitch.Client
	cfg          *config.Config
}

// NewServer initializes the main HTTP server using the provided configuration.
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

// registerRoutes sets up the HTTP endpoints for the main server.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// 1. Serve the dynamic visual overlay
	mux.HandleFunc("/static/alerts_overlay.html", func(w http.ResponseWriter, r *http.Request) {
		s.serveTemplate(w, "alerts_overlay.html")
	})

	// 2. Serve the dynamic audio/chat overlay
	mux.HandleFunc("/static/chat_overlay.html", func(w http.ResponseWriter, r *http.Request) {
		s.serveTemplate(w, "chat_overlay.html")
	})

	// 3. Serve the emotes wall overlay
	mux.HandleFunc("/static/emotes_overlay.html", func(w http.ResponseWriter, r *http.Request) {
		s.serveTemplate(w, "emotes_overlay.html")
	})

	// 4. Serve static assets (CSS, JS, Images)
	fileServer := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	// 5. WebSocket endpoint
	mux.HandleFunc(s.cfg.Server.WebsocketPath, func(w http.ResponseWriter, r *http.Request) {
		websocket.ServeWs(s.hub, w, r)
	})

	// 6. Twitch EventSub Webhook receiver
	mux.HandleFunc("/webhooks/twitch", s.twitchClient.HandleEventSubCallback)

	// 7. Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Println("[INFO] Main HTTP server routes registered.")
}

// serveTemplate serves the requested HTML file, injecting the security configuration paths.
func (s *Server) serveTemplate(w http.ResponseWriter, filename string) {
	// Construct the PUBLIC paths for the browser to use (e.g. "/vlxrobot/ws")
	publicWsPath := path.Join(s.cfg.Server.PathPrefix, s.cfg.Server.WebsocketPath)
		publicAssetPrefix := s.cfg.Server.PathPrefix

			data := struct {
				WebsocketPath string
				AssetPrefix   string
			}{
				WebsocketPath: publicWsPath,
				AssetPrefix:   publicAssetPrefix,
			}

			fp := filepath.Join("static", filename)
			tmpl, err := template.ParseFiles(fp)
			if err != nil {
				log.Printf("[ERROR] Failed to parse template %s: %v", filename, err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := tmpl.Execute(w, data); err != nil {
				log.Printf("[ERROR] Failed to execute template %s: %v", filename, err)
			}
}

// ListenAndServe starts the main HTTP server.
func (s *Server) ListenAndServe() error {
	log.Printf("[INFO] Main HTTP server listening on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}
