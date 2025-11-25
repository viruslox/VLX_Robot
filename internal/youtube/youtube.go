package youtube

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/database"
	"VLX_Robot/internal/twitch" // Import necessario per usare CommandData
	"VLX_Robot/internal/websocket"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// Constants for Polling Limits
const (
	MinPollingInterval     = 5  // seconds
	MaxPollingInterval     = 60 // seconds
	DefaultPollingInterval = 5
)

type Client struct {
	service         *youtube.Service
	channelID       string
	apiKey          string
	pollingInterval time.Duration
	hub             *websocket.Hub
	db              *database.DB
	commands        twitch.AudioCommandsMap // Mappa dei comandi condivisa
}

// NewClient initializes the YouTube client structure and API service
func NewClient(cfg config.YouTubeConfig, hub *websocket.Hub, db *database.DB, commands twitch.AudioCommandsMap) (*Client, error) {
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
		commands:        commands,
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

	// 3. Persist to Database
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

	// 4. Update State (NextPageToken)
	newState := &database.YouTubeState{
		ChannelID:     c.channelID,
		LiveChatID:    state.LiveChatID,
		NextPageToken: sql.NullString{String: response.NextPageToken, Valid: true},
		UpdatedAt:     time.Now(),
	}
	if err := c.db.UpsertYouTubeState(newState); err != nil {
		log.Printf("[WARN] [YouTube] Failed to save NextPageToken: %v", err)
	}

	// 5. Process Messages
	if len(response.Items) > 0 {
		c.processMessages(response.Items)
	}

	return nil
}

// processMessages iterates through chat items and triggers events
func (c *Client) processMessages(items []*youtube.LiveChatMessage) {
	for _, item := range items {
		snippet := item.Snippet
		author := item.AuthorDetails

		// --- A. Handle Super Chats (Monetization) ---
		if snippet.SuperChatDetails != nil {
			payload := map[string]interface{}{
				"type":          "youtube_super_chat",
				"user_name":     author.DisplayName,
				"amount_string": snippet.SuperChatDetails.AmountDisplayString,
				"message":       snippet.SuperChatDetails.UserComment,
				"tier":          snippet.SuperChatDetails.Tier,
			}
			c.broadcast(payload)
			log.Printf("[INFO] [YouTube] Super Chat: %s sent %s", author.DisplayName, snippet.SuperChatDetails.AmountDisplayString)
			continue
		}

		// --- B. Handle Super Stickers (Monetization) ---
		if snippet.SuperStickerDetails != nil {
			payload := map[string]interface{}{
				"type":          "youtube_super_sticker",
				"user_name":     author.DisplayName,
				"amount_string": snippet.SuperStickerDetails.AmountDisplayString,
				"sticker_alt":   snippet.SuperStickerDetails.SuperStickerMetadata.AltText,
			}
			c.broadcast(payload)
			log.Printf("[INFO] [YouTube] Super Sticker from %s", author.DisplayName)
			continue
		}

		// --- C. Handle Text Commands (Interaction) ---
		if snippet.DisplayMessage != "" && strings.HasPrefix(snippet.DisplayMessage, "!") {
			c.handleCommand(snippet.DisplayMessage, author)
		}
	}
}

// handleCommand checks text against the audio command map
func (c *Client) handleCommand(message string, author *youtube.LiveChatMessageAuthorDetails) {
	rawCommand := strings.Fields(message)[0]
	commandName := strings.ToLower(strings.TrimPrefix(rawCommand, "!"))

	cmdData, exists := c.commands[commandName]
	if !exists {
		return
	}

	// Permission Check (Simplified Mapping for YouTube)
	// YouTube doesn't have "Subscriber" in the same API field easily accessible without extra calls.
	// We map: Moderator -> VIP/Mod, Member -> Subscriber.
	hasPerm := false
	
	switch cmdData.Permission {
	case twitch.PermissionEveryone:
		hasPerm = true
	case twitch.PermissionVIP:
		hasPerm = author.IsChatModerator || author.IsChatOwner
	case twitch.PermissionSubscriber:
		// Note: 'IsChatSponsor' is the old field for 'Member'
		hasPerm = author.IsChatSponsor || author.IsChatModerator || author.IsChatOwner
	}

	if !hasPerm {
		return
	}

	log.Printf("[INFO] [YouTube] Command !%s triggered by %s", commandName, author.DisplayName)

	payload := twitch.ChatAlertPayload{
		Type:      "sound_command",
		Filename:  cmdData.Filename,
		MediaType: cmdData.MediaType,
	}
	
	// Serialize manually to match the hub input type
	data, _ := json.Marshal(payload)
	c.hub.Broadcast <- data
}

// broadcast sends a map payload to the WebSocket hub
func (c *Client) broadcast(payload map[string]interface{}) {
	data, err := json.Marshal(payload)
	if err == nil {
		c.hub.Broadcast <- data
	}
}
