package youtube

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/database"
	"VLX_Robot/internal/twitch"
	"VLX_Robot/internal/websocket"

	"go.uber.org/zap"
	"golang.org/x/time/rate" // Rate Limiting Package
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

const (
	MinPollingInterval     = 5
	MaxPollingInterval     = 60
	DefaultPollingInterval = 5
)

type Client struct {
	service         *youtube.Service
	channelID       string
	apiKey          string
	pollingInterval time.Duration
	hub             *websocket.Hub
	db              *database.DB
	commands        twitch.AudioCommandsMap
	logger          *zap.Logger
	limiter         *rate.Limiter // Rate Limiter
}

func NewClient(cfg config.YouTubeConfig, hub *websocket.Hub, db *database.DB, commands twitch.AudioCommandsMap, logger *zap.Logger) (*Client, error) {
	if cfg.APIKey == "" {
		logger.Info("YouTube module disabled (No API Key provided)")
		return nil, nil
	}

	if cfg.ChannelID == "" {
		logger.Warn("YouTube Channel ID is missing in config. Polling will fail.")
	}

	interval := cfg.PollingInterval
	if interval < MinPollingInterval || interval > MaxPollingInterval {
		logger.Warn("Invalid polling interval, using default",
			zap.Int("provided", interval),
			zap.Int("default", DefaultPollingInterval),
		)
		interval = DefaultPollingInterval
	}

	ctx := context.Background()
	service, err := youtube.NewService(ctx, option.WithAPIKey(cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube service: %w", err)
	}

	// Rate Limiter: Allow 1 request every second, with a burst of 2.
	// This protects against loop malfunctions.
	limiter := rate.NewLimiter(rate.Every(1*time.Second), 2)

	return &Client{
		service:         service,
		apiKey:          cfg.APIKey,
		channelID:       cfg.ChannelID,
		pollingInterval: time.Duration(interval) * time.Second,
		hub:             hub,
		db:              db,
		commands:        commands,
		logger:          logger,
		limiter:         limiter,
	}, nil
}

func (c *Client) Start() {
	if c == nil {
		return
	}

	go func() {
		c.logger.Info("Starting YouTube module initialization...")

		if err := c.ensureLiveChatID(); err != nil {
			c.logger.Error("YouTube Initialization failed. Polling halted.", zap.Error(err))
			return
		}

		c.logger.Info("YouTube Live Chat ID initialized. Starting Polling Engine.")
		c.startPolling()
	}()
}

func (c *Client) ensureLiveChatID() error {
	// Rate Limit Check
	if err := c.limiter.Wait(context.Background()); err != nil {
		return err
	}

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
	c.logger.Info("Found active live stream", zap.String("videoID", videoID))

	// Rate Limit Check before next call
	if err := c.limiter.Wait(context.Background()); err != nil {
		return err
	}

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
		return fmt.Errorf("live stream exists but has no active chat")
	}

	liveChatID := details.ActiveLiveChatId
	c.logger.Info("Found LiveChatID", zap.String("liveChatID", liveChatID))

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

func (c *Client) startPolling() {
	ticker := time.NewTicker(c.pollingInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := c.pollChat(); err != nil {
			c.logger.Error("YouTube polling cycle failed", zap.Error(err))
		}
	}
}

func (c *Client) pollChat() error {
	// Rate Limit Check
	if err := c.limiter.Wait(context.Background()); err != nil {
		return fmt.Errorf("rate limiter error: %w", err)
	}

	state, err := c.db.GetYouTubeState(c.channelID)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if !state.LiveChatID.Valid {
		return fmt.Errorf("live_chat_id is missing in DB")
	}
	liveChatID := state.LiveChatID.String

	call := c.service.LiveChatMessages.List(liveChatID, []string{"snippet", "authorDetails"}).MaxResults(200)

	if state.NextPageToken.Valid && state.NextPageToken.String != "" {
		call.PageToken(state.NextPageToken.String)
	}

	response, err := call.Do()
	if err != nil {
		return fmt.Errorf("API call failed: %w", err)
	}

	newState := &database.YouTubeState{
		ChannelID:     c.channelID,
		LiveChatID:    state.LiveChatID,
		NextPageToken: sql.NullString{String: response.NextPageToken, Valid: true},
		UpdatedAt:     time.Now(),
	}
	if err := c.db.UpsertYouTubeState(newState); err != nil {
		c.logger.Warn("Failed to save NextPageToken", zap.Error(err))
	}

	if len(response.Items) > 0 {
		c.processMessages(response.Items)
	}

	return nil
}

func (c *Client) processMessages(items []*youtube.LiveChatMessage) {
	for _, item := range items {
		snippet := item.Snippet
		author := item.AuthorDetails

		// Handle Super Chats
		if snippet.SuperChatDetails != nil {
			payload := map[string]interface{}{
				"type":          "youtube_super_chat",
				"user_name":     author.DisplayName,
				"amount_string": snippet.SuperChatDetails.AmountDisplayString,
				"message":       snippet.SuperChatDetails.UserComment,
				"tier":          snippet.SuperChatDetails.Tier,
			}
			c.broadcast(payload)
			c.logger.Info("Super Chat detected",
				zap.String("user", author.DisplayName),
				zap.String("amount", snippet.SuperChatDetails.AmountDisplayString),
			)
			continue
		}

		// Handle Super Stickers
		if snippet.SuperStickerDetails != nil {
			payload := map[string]interface{}{
				"type":          "youtube_super_sticker",
				"user_name":     author.DisplayName,
				"amount_string": snippet.SuperStickerDetails.AmountDisplayString,
				"sticker_alt":   snippet.SuperStickerDetails.SuperStickerMetadata.AltText,
			}
			c.broadcast(payload)
			c.logger.Info("Super Sticker detected", zap.String("user", author.DisplayName))
			continue
		}

		// Handle Text Commands
		if snippet.DisplayMessage != "" && strings.HasPrefix(snippet.DisplayMessage, "!") {
			c.handleCommand(snippet.DisplayMessage, author)
		}
	}
}

func (c *Client) handleCommand(message string, author *youtube.LiveChatMessageAuthorDetails) {
	rawCommand := strings.Fields(message)[0]
	commandName := strings.ToLower(strings.TrimPrefix(rawCommand, "!"))

	cmdData, exists := c.commands[commandName]
	if !exists {
		return
	}

	hasPerm := false
	switch cmdData.Permission {
	case twitch.PermissionEveryone:
		hasPerm = true
	case twitch.PermissionVIP:
		hasPerm = author.IsChatModerator || author.IsChatOwner
	case twitch.PermissionSubscriber:
		hasPerm = author.IsChatSponsor || author.IsChatModerator || author.IsChatOwner
	}

	if !hasPerm {
		return
	}

	c.logger.Info("YouTube Command Triggered", zap.String("command", commandName), zap.String("user", author.DisplayName))

	payload := twitch.ChatAlertPayload{
		Type:      "sound_command",
		Filename:  cmdData.Filename,
		MediaType: cmdData.MediaType,
	}

	data, _ := json.Marshal(payload)
	c.hub.Broadcast <- data
}

func (c *Client) broadcast(payload map[string]interface{}) {
	data, err := json.Marshal(payload)
	if err == nil {
		c.hub.Broadcast <- data
	}
}
