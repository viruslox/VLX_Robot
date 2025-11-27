package twitch

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/database"
	"VLX_Robot/internal/websocket"

	"github.com/nicklaw5/helix/v2"
	"go.uber.org/zap"
)

// EventSub constants
const (
	EventSubFollow     = "channel.follow"
	EventSubSubscribe  = "channel.subscribe"
	EventSubSubGift    = "channel.subscription.gift"
	EventSubSubMessage = "channel.subscription.message"
	EventSubCheer      = "channel.cheer"
	EventSubRaid       = "channel.raid"
)

// Client manages Twitch API interactions and EventSub webhooks.
type Client struct {
	config      config.TwitchConfig
	helix       *helix.Client
	hub         *websocket.Hub
	db          *database.DB
	selfBaseURL string
	logger      *zap.Logger
}

// NewClient initializes the Twitch client with database-backed token management.
func NewClient(cfg config.TwitchConfig, monitoringChannels []string, baseURL string, hub *websocket.Hub, db *database.DB, logger *zap.Logger) (*Client, error) {
	helixClient, err := helix.NewClient(&helix.Options{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create helix client: %w", err)
	}

	// 1. Generate and Set App Access Token (REQUIRED for EventSub Webhooks)
	appToken, err := helixClient.RequestAppAccessToken(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate app access token: %w", err)
	}
	helixClient.SetAppAccessToken(appToken.Data.AccessToken)

	c := &Client{
		helix:       helixClient,
		db:          db,
		hub:         hub,
		config:      cfg,
		selfBaseURL: baseURL,
		logger:      logger,
	}

	// 2. Verify User Permissions (using DB or Config)
	if len(monitoringChannels) == 0 {
		return nil, errors.New("monitoring channels list is empty")
	}
	primaryLogin := monitoringChannels[0]

	usersResp, err := helixClient.GetUsers(&helix.UsersParams{Logins: []string{primaryLogin}})
	if err != nil || usersResp.StatusCode != http.StatusOK || len(usersResp.Data.Users) == 0 {
		logger.Error("Could not resolve user ID", zap.String("login", primaryLogin))
	}
	var userID string
	if len(usersResp.Data.Users) > 0 {
		userID = usersResp.Data.Users[0].ID
	}

	// 3. Maintain User Token Lifecycle (Refresh if needed)
	if userID != "" {
		err := c.maintainUserToken(userID, cfg)
		if err != nil {
			logger.Warn("User token maintenance failed. EventSub might still work if App authorized.", zap.Error(err))
		}
	} else {
		// Fallback: Try to fix missing ID using config token if available
		if cfg.UserAccessToken != "" {
			logger.Info("Validating config token as fallback...")
			helixClient.SetUserAccessToken(cfg.UserAccessToken)
			isValid, _, _ := helixClient.ValidateToken(cfg.UserAccessToken)
			if isValid {
				logger.Info("Config token is valid")
			}
			helixClient.SetUserAccessToken("")
		}
	}

	// 4. FINAL STEP: Ensure Client uses App Token
	helixClient.SetUserAccessToken("")
	logger.Info("Twitch Client initialized (App Access Token active)")

	return c, nil
}

// maintainUserToken checks DB, validates/refreshes the user token to keep it alive.
func (c *Client) maintainUserToken(userID string, cfg config.TwitchConfig) error {
	creds, err := c.db.GetTwitchCredentials(userID)
	if err != nil {
		if err == sql.ErrNoRows {
			if cfg.UserAccessToken != "" {
				c.logger.Info("No DB record, but config token exists. Assuming initial setup.")
				return nil
			}
			return errors.New("no credentials in DB and no config token")
		}
		return err
	}

	if time.Now().UTC().After(creds.ExpiresAt) {
		c.logger.Info("User access token expired in DB. Refreshing...")
		_, err := c.refreshToken(creds)
		if err != nil {
			return fmt.Errorf("refresh failed: %w", err)
		}
		c.logger.Info("User token refreshed in DB")
	} else {
		c.logger.Info("User token in DB is valid")
	}
	return nil
}

// refreshToken refreshes the User Access Token and updates the database.
func (c *Client) refreshToken(creds *database.TwitchCredentials) (*database.TwitchCredentials, error) {
	if creds.RefreshToken == "" {
		return nil, errors.New("empty refresh token")
	}

	token, err := c.helix.RefreshUserAccessToken(creds.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("helix refresh failed: %w", err)
	}
	if token.StatusCode >= 400 {
		return nil, fmt.Errorf("api refresh error %d: %s", token.StatusCode, token.ErrorMessage)
	}

	newCreds := &database.TwitchCredentials{
		UserID:       creds.UserID,
		AccessToken:  token.Data.AccessToken,
		RefreshToken: token.Data.RefreshToken,
		ExpiresAt:    time.Now().UTC().Add(time.Second * time.Duration(token.Data.ExpiresIn)),
	}

	if err := c.db.UpsertTwitchCredentials(newCreds); err != nil {
		return nil, fmt.Errorf("db update failed: %w", err)
	}

	return newCreds, nil
}

// StartMonitoring sets up EventSub subscriptions for the configured channels.
func (c *Client) StartMonitoring(channelLogins []string) error {
	if c.selfBaseURL == "" {
		return errors.New("baseURL is empty")
	}

	usersResp, err := c.helix.GetUsers(&helix.UsersParams{Logins: channelLogins})
	if err != nil || usersResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to resolve users: %w", err)
	}

	if len(usersResp.Data.Users) == 0 {
		return nil
	}

	callbackURL := c.selfBaseURL + "/webhooks/twitch"

	for _, user := range usersResp.Data.Users {
		c.logger.Info("Subscribing to events", zap.String("user", user.Login), zap.String("id", user.ID))
		c.subscribeToEvent(user.ID, EventSubFollow, "2", callbackURL)
		c.subscribeToRaidEvent(user.ID, callbackURL)
		c.subscribeToEvent(user.ID, EventSubSubscribe, "1", callbackURL)
		c.subscribeToEvent(user.ID, EventSubSubGift, "1", callbackURL)
		c.subscribeToEvent(user.ID, EventSubSubMessage, "1", callbackURL)
		c.subscribeToEvent(user.ID, EventSubCheer, "1", callbackURL)
	}
	return nil
}

// subscribeToEvent creates a subscription if not already active in the DB.
func (c *Client) subscribeToEvent(userID, eventType, version, callbackURL string) {
	sub, err := c.db.GetSubscription(userID, eventType)
	if err == nil && sub.Status == "enabled" {
		return // Already active
	}

	newSub, err := c.createSubscription(userID, eventType, version, callbackURL)
	if err != nil {
		c.logger.Error("Subscription failed", zap.String("type", eventType), zap.Error(err))
		return
	}

	if err := c.saveSubscriptionToDB(userID, eventType, newSub); err != nil {
		c.logger.Info("Synced subscription to DB", zap.String("type", eventType))
	}
}

// subscribeToRaidEvent handles the specific requirements for raid subscriptions.
func (c *Client) subscribeToRaidEvent(userID, callbackURL string) {
	sub, err := c.db.GetSubscription(userID, EventSubRaid)
	if err == nil && sub.Status == "enabled" {
		return
	}

	newSub, err := c.createRaidSubscription(userID, callbackURL)
	if err != nil {
		c.logger.Error("Raid subscription failed", zap.Error(err))
		return
	}

	if err := c.saveSubscriptionToDB(userID, EventSubRaid, newSub); err != nil {
		c.logger.Info("Synced raid subscription to DB")
	}
}

// saveSubscriptionToDB persists subscription details.
func (c *Client) saveSubscriptionToDB(userID, eventType string, sub *helix.EventSubSubscription) error {
	return c.db.CreateSubscription(&database.TwitchSubscription{
		ID:        sub.ID,
		UserID:    userID,
		EventType: eventType,
		Status:    sub.Status,
		CreatedAt: sub.CreatedAt.Time,
	})
}

// createSubscription performs the API call for standard events with 409 auto-recovery.
func (c *Client) createSubscription(userID, eventType, version, callbackURL string) (*helix.EventSubSubscription, error) {
	condition := helix.EventSubCondition{BroadcasterUserID: userID}
	if eventType == EventSubFollow {
		condition.ModeratorUserID = userID
	}

	resp, err := c.helix.CreateEventSubSubscription(&helix.EventSubSubscription{
		Type:      eventType,
		Version:   version,
		Condition: condition,
		Transport: helix.EventSubTransport{
			Method:   "webhook",
			Callback: callbackURL,
			Secret:   c.config.WebhookSecret,
		},
	})

	// Handle 409 Conflict (Already Exists) by fetching the existing one
	if resp != nil && resp.StatusCode == 409 {
		return c.fetchExistingSubscription(userID, eventType)
	}

	return c.handleSubscriptionResponse(resp, err)
}

// createRaidSubscription performs the API call for raid events with 409 auto-recovery.
func (c *Client) createRaidSubscription(userID, callbackURL string) (*helix.EventSubSubscription, error) {
	resp, err := c.helix.CreateEventSubSubscription(&helix.EventSubSubscription{
		Type:    EventSubRaid,
		Version: "1",
		Condition: helix.EventSubCondition{
			ToBroadcasterUserID: userID,
		},
		Transport: helix.EventSubTransport{
			Method:   "webhook",
			Callback: callbackURL,
			Secret:   c.config.WebhookSecret,
		},
	})

	// Handle 409 Conflict (Already Exists)
	if resp != nil && resp.StatusCode == 409 {
		return c.fetchExistingSubscription(userID, EventSubRaid)
	}

	return c.handleSubscriptionResponse(resp, err)
}

// fetchExistingSubscription retrieves all subs of a type and filters client-side to ensure we find it.
func (c *Client) fetchExistingSubscription(userID, eventType string) (*helix.EventSubSubscription, error) {
	opts := &helix.EventSubSubscriptionsParams{
		Type: eventType,
	}

	resp, err := c.helix.GetEventSubSubscriptions(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch existing sub list: %w", err)
	}

	for _, sub := range resp.Data.EventSubSubscriptions {
		// Special handling for Raid: The "UserID" we track is the TARGET of the raid (ToBroadcasterUserID)
		if eventType == EventSubRaid {
			if sub.Condition.ToBroadcasterUserID == userID {
				c.logger.Info("Found existing Raid subscription", zap.String("id", sub.ID), zap.String("status", sub.Status))
				return &sub, nil
			}
			continue
		}

		// Standard handling: The "UserID" is the BroadcasterUserID
		if sub.Condition.BroadcasterUserID == userID {
			c.logger.Info("Found existing subscription", zap.String("type", eventType), zap.String("id", sub.ID), zap.String("status", sub.Status))
			return &sub, nil
		}
	}

	return nil, fmt.Errorf("got 409 from Twitch but could not find subscription for user %s in the list", userID)
}

// handleSubscriptionResponse processes the Helix response.
func (c *Client) handleSubscriptionResponse(resp *helix.EventSubSubscriptionsResponse, err error) (*helix.EventSubSubscription, error) {
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("api status %d: %s", resp.StatusCode, resp.ErrorMessage)
	}
	if len(resp.Data.EventSubSubscriptions) == 0 {
		return nil, errors.New("no subscription data returned")
	}
	return &resp.Data.EventSubSubscriptions[0], nil
}

// HandleEventSubCallback processes incoming webhooks.
func (c *Client) HandleEventSubCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	if !c.verifyEventSubSignature(r, body) {
		http.Error(w, "Invalid Signature", http.StatusUnauthorized)
		return
	}

	messageType := r.Header.Get("Twitch-Eventsub-Message-Type")

	switch messageType {
	case "webhook_callback_verification":
		var verification struct {
			Challenge string `json:"challenge"`
		}
		if err := json.Unmarshal(body, &verification); err == nil {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(verification.Challenge))
		} else {
			http.Error(w, "Bad Request", http.StatusBadRequest)
		}

	case "notification":
		var notification struct {
			Subscription helix.EventSubSubscription `json:"subscription"`
			Event        json.RawMessage            `json:"event"`
		}
		if err := json.Unmarshal(body, &notification); err == nil {
			c.handleNotification(notification.Subscription.Type, notification.Event)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			http.Error(w, "Bad Request", http.StatusBadRequest)
		}

	case "revocation":
		var revocation struct {
			Subscription helix.EventSubSubscription `json:"subscription"`
		}
		if err := json.Unmarshal(body, &revocation); err == nil {
			c.logger.Warn("Subscription revoked", zap.String("id", revocation.Subscription.ID))
			c.db.DeleteSubscription(revocation.Subscription.ID)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))

	default:
		w.WriteHeader(http.StatusOK)
	}
}

// verifyEventSubSignature validates the HMAC signature.
func (c *Client) verifyEventSubSignature(r *http.Request, body []byte) bool {
	id := r.Header.Get("Twitch-Eventsub-Message-Id")
	ts := r.Header.Get("Twitch-Eventsub-Message-Timestamp")
	sig := r.Header.Get("Twitch-Eventsub-Message-Signature")

	prefix := "sha256="
	if len(sig) < len(prefix) {
		return false
	}

	mac := hmac.New(sha256.New, []byte(c.config.WebhookSecret))
	mac.Write([]byte(id + ts))
	mac.Write(body)
	expected := prefix + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}

// handleNotification distributes events to the WebSocket hub.
func (c *Client) handleNotification(eventType string, eventData json.RawMessage) {
	var payload map[string]interface{}
	var err error

	switch eventType {
	case EventSubFollow:
		var e helix.EventSubChannelFollowEvent
		if err = json.Unmarshal(eventData, &e); err == nil {
			payload = map[string]interface{}{
				"type":      "twitch_follow",
				"user_name": e.UserName,
			}
		}
	case EventSubSubscribe:
		var e helix.EventSubChannelSubscribeEvent
		if err = json.Unmarshal(eventData, &e); err == nil {
			payload = map[string]interface{}{
				"type":      "twitch_subscribe",
				"user_name": e.UserName,
				"tier":      e.Tier,
				"is_gift":   e.IsGift,
			}
		}
	case EventSubSubMessage:
		var e helix.EventSubChannelSubscriptionMessageEvent
		if err = json.Unmarshal(eventData, &e); err == nil {
			payload = map[string]interface{}{
				"type":              "twitch_resubscribe",
				"user_name":         e.UserName,
				"tier":              e.Tier,
				"message":           e.Message.Text,
				"cumulative_months": e.CumulativeMonths,
			}
		}
	case EventSubSubGift:
		var e helix.EventSubChannelSubscriptionGiftEvent
		if err = json.Unmarshal(eventData, &e); err == nil {
			payload = map[string]interface{}{
				"type":        "twitch_gift_sub",
				"gifter_name": e.UserName,
				"total_gifts": e.Total,
				"tier":        e.Tier,
			}
		}
	case EventSubCheer:
		var e helix.EventSubChannelCheerEvent
		if err = json.Unmarshal(eventData, &e); err == nil {
			payload = map[string]interface{}{
				"type":      "twitch_cheer",
				"user_name": e.UserName,
				"bits":      e.Bits,
				"message":   e.Message,
			}
		}
	case EventSubRaid:
		var e helix.EventSubChannelRaidEvent
		if err = json.Unmarshal(eventData, &e); err == nil {
			payload = map[string]interface{}{
				"type":        "twitch_raid",
				"raider_name": e.FromBroadcasterUserName,
				"viewers":     e.Viewers,
			}
		}
	}

	if err != nil {
		c.logger.Error("Failed to parse event", zap.String("type", eventType), zap.Error(err))
		return
	}
	if payload != nil {
		data, _ := json.Marshal(payload)
		c.hub.Broadcast <- data
	}
}
