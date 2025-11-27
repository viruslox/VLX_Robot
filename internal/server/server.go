package server

import (
	"html/template"
	"net/http"
	"path"
	"path/filepath"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/twitch"
	"VLX_Robot/internal/websocket"

	"go.uber.org/zap"
)

type Server struct {
	httpServer   *http.Server
	hub          *websocket.Hub
	twitchClient *twitch.Client
	cfg          *config.Config
	logger       *zap.Logger
}

func NewServer(cfg *config.Config, hub *websocket.Hub, twitchClient *twitch.Client, logger *zap.Logger) *Server {
	mux := http.NewServeMux()
	s := &Server{
		hub:          hub,
		twitchClient: twitchClient,
		cfg:          cfg,
		logger:       logger,
	}
	s.registerRoutes(mux)
	s.httpServer = &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: mux,
	}
	return s
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/static/alerts_overlay.html", func(w http.ResponseWriter, r *http.Request) {
		s.serveTemplate(w, "alerts_overlay.html")
	})
	mux.HandleFunc("/static/chat_overlay.html", func(w http.ResponseWriter, r *http.Request) {
		s.serveTemplate(w, "chat_overlay.html")
	})
	mux.HandleFunc("/static/emotes_overlay.html", func(w http.ResponseWriter, r *http.Request) {
		s.serveTemplate(w, "emotes_overlay.html")
	})

	fileServer := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	// Pass Logger to WebSocket handler
	mux.HandleFunc(s.cfg.Server.WebsocketPath, func(w http.ResponseWriter, r *http.Request) {
		websocket.ServeWs(s.hub, s.logger, w, r)
	})

	mux.HandleFunc("/webhooks/twitch", s.twitchClient.HandleEventSubCallback)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	s.logger.Info("Main HTTP server routes registered")
}

func (s *Server) serveTemplate(w http.ResponseWriter, filename string) {
	publicWsPath := path.Join(s.cfg.Server.PathPrefix, s.cfg.Server.WebsocketPath)
	publicAssetPrefix := s.cfg.Server.PathPrefix

	// Determine volume, default to 100 if not set or invalid
	vol := s.cfg.Server.OverlayVolume
	if vol < 0 {
		vol = 100
	}

	data := struct {
		WebsocketPath string
		AssetPrefix   string
		Volume        int // Injected volume
	}{
		WebsocketPath: publicWsPath,
		AssetPrefix:   publicAssetPrefix,
		Volume:        vol,
	}

	fp := filepath.Join("static", filename)
	tmpl, err := template.ParseFiles(fp)
	if err != nil {
		s.logger.Error("Failed to parse template", zap.String("file", filename), zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		s.logger.Error("Failed to execute template", zap.String("file", filename), zap.Error(err))
	}
}

func (s *Server) ListenAndServe() error {
	s.logger.Info("Main HTTP server listening", zap.String("address", s.httpServer.Addr))
	return s.httpServer.ListenAndServe()
}
