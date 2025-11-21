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
	"log"
	"net/http"
	"time"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/database"
	"VLX_Robot/internal/websocket"

	"github.com/nicklaw5/helix/v2"
)

// EventSubTypes constants
const (
	EventSubFollow     = "channel.follow"
	EventSubSubscribe  = "channel.subscribe"
	EventSubSubGift    = "channel.subscription.gift"
	EventSubSubMessage = "channel.subscription.message"
	EventSubCheer      = "channel.cheer"
	EventSubRaid       = "channel.raid"
)

// Client handles all Twitch API and EventSub logic
type Client struct {
	config      config.TwitchConfig
	helix       *helix.Client
	hub         *websocket.Hub
	db          *database.DB
	selfBaseURL string
}

// NewClient creates a new Twitch client and uses the User Access Token.
func NewClient(cfg config.TwitchConfig, monitoringChannels []string, baseURL string, hub *websocket.Hub, db *database.DB) (*Client, error) {

	helixClient, err := helix.NewClient(&helix.Options{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("[ERROR] : Failed to create Helix client: %w", err)
	}

	appToken, err := helixClient.RequestAppAccessToken(nil) // nil = scopes di default
	if err != nil {
		return nil, fmt.Errorf("[ERROR] : Failed to generate App Access Token: %w", err)
	}
	helixClient.SetAppAccessToken(appToken.Data.AccessToken)
	log.Println("[INFO] : App Access Token generated successfully.")

	c := &Client{
		helix: helixClient,
		db:    db,
	}

	// --- NEW DATABASE-AWARE TOKEN LOGIC ---
	// 1. Get the User ID from the *first* channel in the monitoring list
	if len(monitoringChannels) == 0 {
		return nil, errors.New("[FATAL] : 'monitoring.twitch_channels' in config.yml is empty. Cannot determine user ID for token.")
	}
	primaryLogin := monitoringChannels[0]
	usersResp, err := helixClient.GetUsers(&helix.UsersParams{Logins: []string{primaryLogin}})
	if err != nil ||
		usersResp.StatusCode != http.StatusOK || len(usersResp.Data.Users) == 0 {
		log.Printf("[ERROR] : Could not get user ID for login '%s'. Using config token as fallback.", primaryLogin)
		// Fallback to config token logic if we can't get the user
		return useConfigToken(cfg, baseURL, hub, db, helixClient)
	}
	userID := usersResp.Data.Users[0].ID
	log.Printf("[INFO] : Primary user ID identified as: %s (for login %s)", userID, primaryLogin)

	// 2. Try to get credentials from the database
	creds, err := db.GetTwitchCredentials(userID)
	if err != nil {
		if err == sql.ErrNoRows {
			// No token in DB. Use the one from config.yml as the "first run" token.
			log.Println("[WARN] : No credentials found in database. Using 'user_access_token' from config.yml.")
			log.Println("[WARN] : This token will NOT be refreshed. Please populate 'twitch_credentials' table for refresh logic.")
			return useConfigToken(cfg, baseURL, hub, db, helixClient)
		}
		// A real database error occurred
		return nil, fmt.Errorf("[ERROR] : Failed to query database for credentials: %w", err)
	}

	// 3. Credentials found. Check if expired.
	if time.Now().UTC().After(creds.ExpiresAt) {
		log.Println("[INFO] : Access token is expired. Attempting to refresh...")
		newCreds, err := c.refreshToken(creds)
		if err != nil {
			log.Printf("[ERROR] : Failed to refresh token: %v. Falling back to config.yml token.", err)
			return useConfigToken(cfg, baseURL, hub, db, helixClient)
		}
		log.Println("[INFO] : Token refreshed successfully.")
		creds = newCreds // Use the newly refreshed credentials
	}

	// 4. Set the valid token (from DB or refresh)
	log.Println("[INFO] : Successfully authenticated using token from database.")

	c.config = cfg
	c.hub = hub
	c.selfBaseURL = baseURL

	return c, nil
}

// useConfigToken is a fallback helper to use the token from config.yml
func useConfigToken(cfg config.TwitchConfig, baseURL string, hub *websocket.Hub, db *database.DB, helixClient *helix.Client) (*Client, error) {
	if cfg.UserAccessToken == "" ||
		cfg.UserAccessToken == "il_token_lungo_che_hai_appena_copiato" {
		log.Println("************************************************************")
		log.Println("[FATAL] : 'user_access_token' in config.yml is missing or default.")
		log.Println("[FATAL] : And no valid token was found in the database.")
		log.Println("************************************************************")
		return nil, errors.New("'user_access_token' is missing from config.yml and DB")
	}

	log.Println("[INFO] : Successfully authenticated using User Access Token from config.yml (fallback).")

	// Validate the config token
	isValid, validationData, err := helixClient.ValidateToken(cfg.UserAccessToken)
	if err != nil {
		log.Printf("[WARN] : Could not validate token from config.yml (network error?): %v", err)
	} else if !isValid {
		if validationData != nil {
			log.Printf("[FATAL] : Token from config.yml is invalid. (Status %d): %s", validationData.StatusCode, validationData.ErrorMessage)
			return nil, fmt.Errorf("token from config.yml is invalid: %s", validationData.ErrorMessage)
		}
		log.Printf("[FATAL] : Token from config.yml is invalid or expired.")
		return nil, errors.New("token from config.yml is invalid or expired")
	} else {
		log.Printf("[INFO] : Token from config.yml validated for user: %s", validationData.Data.Login)
	}

	return &Client{
		config:      cfg,
		helix:       helixClient,
		hub:         hub,
		db:          db,
		selfBaseURL: baseURL,
	}, nil
}

// refreshToken attempts to get a new token using the refresh_token
func (c *Client) refreshToken(creds *database.TwitchCredentials) (*database.TwitchCredentials, error) {
	if creds.RefreshToken == "" {
		return nil, errors.New("refresh token is empty, cannot refresh")
	}

	token, err := c.helix.RefreshUserAccessToken(creds.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("helix refresh failed: %w", err)
	}
	if token.StatusCode >= 400 {
		return nil, fmt.Errorf("helix refresh failed (Status %d): %s", token.StatusCode, token.ErrorMessage)
	}

	// Update our credentials struct with the new data
	newCreds := &database.TwitchCredentials{
		UserID:       creds.UserID,
		AccessToken:  token.Data.AccessToken,
		RefreshToken: token.Data.RefreshToken, // Store the new refresh token
		ExpiresAt:    time.Now().UTC().Add(time.Second * time.Duration(token.Data.ExpiresIn)),
	}

	// Save the new credentials to the database
	err = c.db.UpsertTwitchCredentials(newCreds)
	if err != nil {
		return nil, fmt.Errorf("failed to save new credentials to DB: %w", err)
	}

	return newCreds, nil
}

// StartMonitoring subscribes to all specified events for the given channels.
func (c *Client) StartMonitoring(channelLogins []string) error {
	if c.selfBaseURL == "" ||
		c.selfBaseURL == "https://abcdef123.eu.ngrok.io" { // A placeholder check
		return errors.New("[ERROR] : Please update selfBaseURL in config.yml with your public ngrok/server URL")
	}

	// Convert channel names into User IDs
	usersResp, err := c.helix.GetUsers(&helix.UsersParams{
		Logins: channelLogins,
	})
	if err != nil ||
		usersResp.StatusCode != http.StatusOK {
		return fmt.Errorf("[ERROR] : Cannot properly get Twitch user ID from channel name: %w (status: %d)", err, usersResp.StatusCode)
	}

	if len(usersResp.Data.Users) == 0 {
		log.Println("[WARN] : No valid Twitch channels found for monitoring.")
		return nil
	}

	// Twitch will send webhooks to this URL
	callbackURL := c.selfBaseURL + "/webhooks/twitch"

	// Subscribe to events for each user
	for _, user := range usersResp.Data.Users {
		log.Printf("[INFO] : Starting event subscriptions for channel: %s (ID: %s)", user.Login, user.ID)

		// "channel.follow" (Version 2)
		c.subscribeToEvent(user.ID, EventSubFollow, "2", callbackURL)

		// "channel.raid" (from another channel TO this channel)
		c.subscribeToRaidEvent(user.ID, callbackURL)

		// "channel.subscribe"
		c.subscribeToEvent(user.ID, EventSubSubscribe, "1", callbackURL)

		// "channel.subscription.gift"
		c.subscribeToEvent(user.ID, EventSubSubGift, "1", callbackURL)

		// "channel.subscription.message"
		c.subscribeToEvent(user.ID, EventSubSubMessage, "1", callbackURL)

		// "channel.cheer"
		c.subscribeToEvent(user.ID, EventSubCheer, "1", callbackURL)
	}
	return nil
}

// subscribeToEvent is a wrapper for createSubscription that includes DB checks
func (c *Client) subscribeToEvent(userID, eventType, version, callbackURL string) {
	// 1. Check if subscription already exists in DB
	sub, err := c.db.GetSubscription(userID, eventType)
	if err == nil && sub.Status == "enabled" {
		log.Printf("[INFO] : Subscription for %s on %s already active.", eventType, userID)
		return
	}

	// 2. Create the subscription
	newSubData, err := c.createSubscription(userID, eventType, version, callbackURL)
	if err != nil {
		log.Printf("[ERROR] : Failed to subscribe to %s for %s: %v", eventType, userID, err)
		return
	}

	// 3. Save the new subscription to the DB
	dbSub := &database.TwitchSubscription{
		ID:        newSubData.ID,
		UserID:    userID,
		EventType: eventType,
		Status:    newSubData.Status,
		CreatedAt: newSubData.CreatedAt.Time,
	}
	if err := c.db.CreateSubscription(dbSub); err != nil {
		log.Printf("[ERROR] : Failed to save subscription %s to DB: %v", newSubData.ID, err)
	}
}

// subscribeToRaidEvent is a wrapper for createRaidSubscription that includes DB checks
func (c *Client) subscribeToRaidEvent(userID, callbackURL string) {
	// 1. Check if subscription already exists in DB
	sub, err := c.db.GetSubscription(userID, EventSubRaid)
	if err == nil && sub.Status == "enabled" {
		log.Printf("[INFO] : Subscription for %s on %s already active.", EventSubRaid, userID)
		return
	}

	// 2. Create the subscription
	newSubData, err := c.createRaidSubscription(userID, callbackURL)
	if err != nil {
		log.Printf("[ERROR] : Failed to subscribe to %s for %s: %v", EventSubRaid, userID, err)
		return
	}

	// 3. Save the new subscription to the DB
	dbSub := &database.TwitchSubscription{
		ID:        newSubData.ID,
		UserID:    userID,
		EventType: EventSubRaid,
		Status:    newSubData.Status,
		CreatedAt: newSubData.CreatedAt.Time,
	}
	if err := c.db.CreateSubscription(dbSub); err != nil {
		log.Printf("[ERROR] : Failed to save subscription %s to DB: %v", newSubData.ID, err)
	}
}

// createSubscription is a helper for standard subscriptions
func (c *Client) createSubscription(userID, eventType, version, callbackURL string) (*helix.EventSubSubscription, error) {

	condition := helix.EventSubCondition{
		BroadcasterUserID: userID,
	}

	// 'channel.follow' (v2) richiede *anche* 'moderator_user_id'
	if eventType == EventSubFollow {
		condition.ModeratorUserID = userID
	}

	resp, err := c.helix.CreateEventSubSubscription(&helix.EventSubSubscription{
		Type:    eventType,
		Version: version,
		Condition: condition,
		Transport: helix.EventSubTransport{
			Method:   "webhook",
			Callback: callbackURL,
			Secret:   c.config.WebhookSecret,
		},
	})

	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("[ERROR] : API error (%d): %s", resp.StatusCode, resp.ErrorMessage)
	}

	if len(resp.Data.EventSubSubscriptions) > 0 {
		sub := resp.Data.EventSubSubscriptions[0]
		log.Printf("[INFO] : Subscription to %s (v%s) for %s created (Status: %s)", eventType, version, userID, sub.Status)
		return &sub, nil
	}

	return nil, errors.New("subscription call successful but no data returned")
}

// createRaidSubscription is a helper for 'channel.raid' which uses a different condition
func (c *Client) createRaidSubscription(userID, callbackURL string) (*helix.EventSubSubscription, error) {
	resp, err := c.helix.CreateEventSubSubscription(&helix.EventSubSubscription{
		Type:    EventSubRaid,
		Version: "1",
		Condition: helix.EventSubCondition{
			ToBroadcasterUserID: userID, // Note: This is 'ToBroadcasterUserID'
		},
		Transport: helix.EventSubTransport{
			Method:   "webhook",
			Callback: callbackURL,
			Secret:   c.config.WebhookSecret,
		},
	})

	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("[ERROR] : API error (%d): %s", resp.StatusCode, resp.ErrorMessage)
	}

	if len(resp.Data.EventSubSubscriptions) > 0 {
		sub := resp.Data.EventSubSubscriptions[0]
		log.Printf("[INFO] : Subscription to %s (v1) for %s created (Status: %s)", EventSubRaid, userID, sub.Status)
		return &sub, nil
	}

	return nil, errors.New("subscription call successful but no data returned")
}

// HandleEventSubCallback handles ALL incoming requests from Twitch
func (c *Client) HandleEventSubCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[ERROR] : Failed to read webhook request body: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	// 1. Verify the Webhook Signature (CRITICAL for security)
	if !c.verifyEventSubSignature(r, body) {
		log.Printf("[WARN] : Invalid Webhook Signature received from %s", r.RemoteAddr)
		http.Error(w, "Invalid Signature", http.StatusUnauthorized)
		return
	}

	// 2. Identify the message type
	messageType := r.Header.Get("Twitch-Eventsub-Message-Type")

	// 3. Handle different message types
	switch messageType {

	// Case A: "webhook_callback_verification"
	case "webhook_callback_verification":
		var verification struct {
			Challenge string `json:"challenge"`
		}

		if err := json.Unmarshal(body, &verification); err != nil {
			log.Printf("[ERROR] : Failed to unmarshal webhook verification: %v", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		log.Println("[INFO] : Webhook verification received. Sending challenge response.")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(verification.Challenge))
		return

	// Case B: A real event notification (e.g., a follow)
	case "notification":
		// Parse the body into a generic struct to find the type
		var notification struct {
			Subscription helix.EventSubSubscription `json:"subscription"`
			Event        json.RawMessage            `json:"event"`
		}
		if err := json.Unmarshal(body, &notification); err != nil {
			log.Printf("[ERROR] : Failed to unmarshal notification: %v", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Handle the notification and send it to the hub
		c.handleNotification(notification.Subscription.Type, notification.Event)

		// Respond to Twitch with 200 OK to acknowledge receipt
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return

	// Case C: Twitch tells us a subscription was revoked
	case "revocation":
		// Parse the subscription to get the ID
		var revocation struct {
			Subscription helix.EventSubSubscription `json:"subscription"`
		}
		if err := json.Unmarshal(body, &revocation); err == nil {
			log.Printf("[WARN] : Subscription revoked (ID: %s). Deleting from database.", revocation.Subscription.ID)
			// Delete from our database
			if err := c.db.DeleteSubscription(revocation.Subscription.ID); err != nil {
				log.Printf("[ERROR] : Failed to delete revoked subscription %s from DB: %v", revocation.Subscription.ID, err)
			}
		} else {
			log.Printf("[WARN] : Subscription revoked. Could not parse body: %s", string(body))
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return

	default:
		log.Printf("[WARN] : Received unknown message type: %s", messageType)
		w.WriteHeader(http.StatusNoContent) // Respond to Twitch
	}
}

// verifyEventSubSignature verifies the request from Twitch
func (c *Client) verifyEventSubSignature(r *http.Request, body []byte) bool {
	messageID := r.Header.Get("Twitch-Eventsub-Message-Id")
	timestamp := r.Header.Get("Twitch-Eventsub-Message-Timestamp")

	message := messageID + timestamp + string(body)

	mac := hmac.New(sha256.New, []byte(c.config.WebhookSecret))
	mac.Write([]byte(message))
	expectedSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Compare with the received signature
	receivedSignature := r.Header.Get("Twitch-Eventsub-Message-Signature")

	return hmac.Equal([]byte(receivedSignature), []byte(expectedSignature))
}

// handleNotification parses the specific event and sends it to the WebSocket hub
func (c *Client) handleNotification(eventType string, eventData json.RawMessage) {
	log.Printf("[INFO] : Notification received: %s", eventType)

	// Create a generic message to send to the frontend
	// Our overlay.js will need to understand this.
	var alertPayload map[string]interface{}

	switch eventType {

	case EventSubFollow:
		var event helix.EventSubChannelFollowEvent
		if err := json.Unmarshal(eventData, &event); err != nil {
			log.Printf("[ERROR] : Failed to unmarshal event %s: %v", eventType, err)
			return
		}

		alertPayload = map[string]interface{}{
			"type":         "twitch_follow",
			"user_name":    event.UserName,
			"channel_name": event.BroadcasterUserName,
		}

	case EventSubSubscribe:
		var event helix.EventSubChannelSubscribeEvent
		if err := json.Unmarshal(eventData, &event); err != nil {
			log.Printf("[ERROR] : Failed to unmarshal event %s: %v", eventType, err)
			return
		}
		alertPayload = map[string]interface{}{
			"type":      "twitch_subscribe",
			"user_name": event.UserName,
			"tier":      event.Tier, // "1000", "2000", "3000"
			"is_gift":   event.IsGift,
		}

	case EventSubSubMessage:
		var event helix.EventSubChannelSubscriptionMessageEvent
		if err := json.Unmarshal(eventData, &event); err != nil {
			log.Printf("[ERROR] : Failed to unmarshal event %s: %v", eventType, err)
			return
		}
		alertPayload = map[string]interface{}{
			"type":              "twitch_resubscribe",
			"user_name":         event.UserName,
			"tier":              event.Tier,
			"message":           event.Message.Text,
			"cumulative_months": event.CumulativeMonths,
			"streak_months":     event.StreakMonths, // Can be 0 if no streak
		}

	case EventSubSubGift:
		var event helix.EventSubChannelSubscriptionGiftEvent
		if err := json.Unmarshal(eventData, &event); err != nil {
			log.Printf("[ERROR] : Failed to unmarshal event %s: %v", eventType, err)
			return
		}
		alertPayload = map[string]interface{}{
			"type":         "twitch_gift_sub",
			"gifter_name":  event.UserName, // Can be "" for anonymous
			"total_gifts":  event.Total,    // Number of gifts in this batch
			"tier":         event.Tier,
			"is_anonymous": event.IsAnonymous,
		}

	case EventSubCheer:
		var event helix.EventSubChannelCheerEvent
		if err := json.Unmarshal(eventData, &event); err != nil {
			log.Printf("[ERROR] : Failed to unmarshal event %s: %v", eventType, err)
			return
		}
		alertPayload = map[string]interface{}{
			"type":         "twitch_cheer",
			"user_name":    event.UserName, // Can be "" for anonymous
			"bits":         event.Bits,
			"message":      event.Message,
			"is_anonymous": event.IsAnonymous,
		}

	case EventSubRaid:
		var event helix.EventSubChannelRaidEvent
		if err := json.Unmarshal(eventData, &event); err != nil {
			log.Printf("[ERROR] : Failed to unmarshal event %s: %v", eventType, err)
			return
		}
		alertPayload = map[string]interface{}{
			"type":          "twitch_raid",
			"raider_name":   event.FromBroadcasterUserName,
			"viewers":       event.Viewers,
		}

	default:
		log.Printf("[WARN] : Unhandled event type: %s", eventType)
		return
	}

	// Serialize our payload to JSON
	payloadBytes, err := json.Marshal(alertPayload)
	if err != nil {
		log.Printf("[ERROR] : Failed to marshal alert payload: %v", err)
		return
	}

	// Send the JSON to the Hub's Broadcast channel!
	c.hub.Broadcast <- payloadBytes
}
