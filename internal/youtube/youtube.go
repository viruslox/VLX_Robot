package youtube

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/database"
	"VLX_Robot/internal/websocket"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

type Client struct {
	service   *youtube.Service
	channelID string
	apiKey    string
	hub       *websocket.Hub
	db        *database.DB
}

// NewClient initializes the YouTube client structure and API service
func NewClient(cfg config.YouTubeConfig, hub *websocket.Hub, db *database.DB) (*Client, error) {
	// Check if API Key is provided. If not, disable the module gracefully.
	if cfg.APIKey == "" {
		log.Println("[INFO] YouTube module disabled (No API Key provided)")
		return nil, nil
	}

	if cfg.ChannelID == "" {
		log.Println("[WARN] YouTube Channel ID is missing in config. Polling will fail.")
	}

	ctx := context.Background()
	service, err := youtube.NewService(ctx, option.WithAPIKey(cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube service: %w", err)
	}

	return &Client{
		service:   service,
		apiKey:    cfg.APIKey,
		channelID: cfg.ChannelID,
		hub:       hub,
		db:        db,
	}, nil
}

// Start initiates the polling logic (Phase A: Initialization)
func (c *Client) Start() {
	if c == nil {
		return
	}

	go func() {
		log.Println("[INFO] [YouTube] Starting initialization...")

		// Phase A: Retrieve and Persist LiveChatID
		if err := c.ensureLiveChatID(); err != nil {
			log.Printf("[ERROR] [YouTube] Initialization failed: %v. Polling will NOT start.", err)
			return
		}

		log.Println("[INFO] [YouTube] Live Chat ID successfully initialized.")

		// TODO: Phase B (StartPolling) will be called here
	}()
}

// ensureLiveChatID finds the current live stream and saves its Chat ID to DB
func (c *Client) ensureLiveChatID() error {
	// 1. Search for an active live stream on the channel
	call := c.service.Search.List([]string{"id"}).
		ChannelId(c.channelID).
		EventType("live").
		Type("video").
		MaxResults(1)

	response, err := call.Do()
	if err != nil {
		return fmt.Errorf("search API failed: %w", err)
	}

	if len(response.Items) == 0 {
		return fmt.Errorf("no active live stream found for channel %s", c.channelID)
	}

	videoID := response.Items[0].Id.VideoId
	log.Printf("[INFO] [YouTube] Found active live stream: %s", videoID)

	// 2. Get the video details to find the activeLiveChatId
	videoCall := c.service.Videos.List([]string{"liveStreamingDetails"}).Id(videoID)
	videoResponse, err := videoCall.Do()
	if err != nil {
		return fmt.Errorf("videos API failed: %w", err)
	}

	if len(videoResponse.Items) == 0 {
		return fmt.Errorf("video details not found for ID %s", videoID)
	}

	details := videoResponse.Items[0].LiveStreamingDetails
	if details == nil || details.ActiveLiveChatId == "" {
		return fmt.Errorf("live stream exists but has no active chat (or chat is disabled)")
	}

	liveChatID := details.ActiveLiveChatId
	log.Printf("[INFO] [YouTube] Found LiveChatID: %s", liveChatID)

	// 3. Persist to Database (Phase A Complete)
	state := &database.YouTubeState{
		ChannelID:  c.channelID,
		LiveChatID: sql.NullString{String: liveChatID, Valid: true},
		UpdatedAt:  time.Now(),
	}

	if err := c.db.UpsertYouTubeState(state); err != nil {
		return fmt.Errorf("failed to save state to DB: %w", err)
	}

	return nil
}
