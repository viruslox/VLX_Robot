package youtube

import (
	"log"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/database"
	"VLX_Robot/internal/websocket"
)

type Client struct {
	apiKey string
	hub    *websocket.Hub
	db     *database.DB
}

// NewClient initializes the YouTube client structure
func NewClient(cfg config.YouTubeConfig, hub *websocket.Hub, db *database.DB) *Client {
	// Check if API Key is provided. If not, disable the module gracefully.
	if cfg.APIKey == "" {
		log.Println("[INFO] YouTube module disabled (No API Key provided)")
		return nil
	}

	return &Client{
		apiKey: cfg.APIKey,
		hub:    hub,
		db:     db,
	}
}
