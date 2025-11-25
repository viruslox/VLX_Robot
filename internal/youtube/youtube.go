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

// Constants for Polling Limits
const (
	MinPollingInterval = 5  // seconds
	MaxPollingInterval = 60 // seconds
	DefaultPollingInterval = 5
)

type Client struct {
	service   *youtube.Service
	channelID string
	apiKey    string
	pollingInterval time.Duration
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

	// Validate Polling Interval
	interval := cfg.PollingInterval
	if interval < MinPollingInterval || interval > MaxPollingInterval {
		log.Printf("[WARN] Invalid polling interval (%d). Using default: %ds", interval, DefaultPollingInterval)
		interval = DefaultPollingInterval
	}

	ctx := context.Background()
	service, err := youtube.NewService(ctx, option.WithAPIKey(cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube service: %w", err)
	}

	return &Client{
		service:         service,
		apiKey:          cfg.APIKey,
		channelID:       cfg.ChannelID,
		pollingInterval: time.Duration(interval) * time.Second,
		hub:             hub,
		db:              db,
	}, nil
}

// Start initiates the initialization and then the polling loop
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
		
		log.Println("[INFO] [YouTube] Live Chat ID initialized. Starting Polling Engine...")
		
		// Phase B: Start Polling
		c.startPolling()
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

// startPolling runs the main ticker loop
func (c *Client) startPolling() {
	ticker := time.NewTicker(c.pollingInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := c.pollChat(); err != nil {
			log.Printf("[ERROR] [YouTube] Polling cycle failed: %v", err)
            // Optional: Implement backoff or stop on critical errors
		}
	}
}

// pollChat performs a single API check
func (c *Client) pollChat() error {
	// 1. Get current state (ChatID + NextPageToken)
	state, err := c.db.GetYouTubeState(c.channelID)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if !state.LiveChatID.Valid {
		return fmt.Errorf("live_chat_id is missing in DB")
	}
	liveChatID := state.LiveChatID.String

	// 2. Prepare API Call
	call := c.service.LiveChatMessages.List(liveChatID, []string{"snippet", "authorDetails"}).MaxResults(200)

	// Important: Use page token if available
	if state.NextPageToken.Valid && state.NextPageToken.String != "" {
		call.PageToken(state.NextPageToken.String)
	}

	// 3. Execute API Call
	response, err := call.Do()
	if err != nil {
		return fmt.Errorf("API call failed: %w", err)
	}

	// 4. Update State (Save new NextPageToken)
    // We update immediately to avoid processing duplicates if the next steps fail
	newState := &database.YouTubeState{
		ChannelID:     c.channelID,
		LiveChatID:    state.LiveChatID,
		NextPageToken: sql.NullString{String: response.NextPageToken, Valid: true},
		UpdatedAt:     time.Now(),
	}
	if err := c.db.UpsertYouTubeState(newState); err != nil {
		log.Printf("[WARN] [YouTube] Failed to save NextPageToken: %v", err)
	}

    // 5. Process Messages (Phase C stub)
	if len(response.Items) > 0 {
		log.Printf("[INFO] [YouTube] Received %d new messages. (Processing Logic: TODO)", len(response.Items))
        
        // Loop through items and handle them (Phase C)
        // for _, item := range response.Items { ... }
	}

	return nil
}
