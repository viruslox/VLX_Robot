package twitch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/websocket"

	"github.com/gempir/go-twitch-irc/v4"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Permission constants
const (
	PermissionEveryone   = "everyone"   // Public/Followers
	PermissionSubscriber = "subscriber" // Paid Subscribers
	PermissionVIP        = "vip"        // VIP/Mods
)

// CommandData holds metadata for media commands
type CommandData struct {
	Filename   string
	Permission string
	MediaType  string // "audio" or "video"
}

type AudioCommandsMap map[string]CommandData

// ChatClient handles Twitch IRC connection
type ChatClient struct {
	config           config.TwitchChatConfig
	hub              *websocket.Hub
	client           *twitch.Client
	commands         AudioCommandsMap
	lastUsage        map[string]time.Time // Tracks command cooldowns
	cooldownDuration time.Duration        // Configured cooldown
	logger           *zap.Logger
	sayLimiter       *rate.Limiter // Rate limiter for outgoing chat messages
}

// ChatAlertPayload defines the JSON sent to the overlay
type ChatAlertPayload struct {
	Type      string `json:"type"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
}

type EmoteWallPayload struct {
	Type   string   `json:"type"`
	Emotes []string `json:"emotes"`
}

// NewChatClient initializes the ChatClient with dependencies and rate limiters.
func NewChatClient(cfg config.TwitchChatConfig, hub *websocket.Hub, commands AudioCommandsMap, logger *zap.Logger) *ChatClient {
	// Set default cooldown if invalid
	cd := cfg.CommandCooldown
	if cd <= 0 {
		cd = 15
	}

	// Initialize Rate Limiter for outgoing messages.
	// Twitch limits: 20/30s for users, 100/30s for mods.
	// We use a conservative bucket: 1 message per second, burst of 5.
	limiter := rate.NewLimiter(rate.Every(time.Second), 5)

	return &ChatClient{
		config:           cfg,
		hub:              hub,
		commands:         commands,
		lastUsage:        make(map[string]time.Time),
		cooldownDuration: time.Duration(cd) * time.Second,
		logger:           logger,
		sayLimiter:       limiter,
	}
}

// ScanAudioCommands recursively scans command folders to build the command map.
func ScanAudioCommands(baseDir string, logger *zap.Logger) (AudioCommandsMap, error) {
	commands := make(AudioCommandsMap)

	folders := map[string]string{
		"everyone":    PermissionEveryone,
		"subscribers": PermissionSubscriber,
		"vips":        PermissionVIP,
	}

	for folderName, permission := range folders {
		fullPath := filepath.Join(baseDir, folderName)

		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		}

		files, err := os.ReadDir(fullPath)
		if err != nil {
			logger.Warn("Could not read command folder", zap.String("path", fullPath), zap.Error(err))
			continue
		}

		for _, file := range files {
			if file.IsDir() {
				continue
			}

			filename := file.Name()
			ext := strings.ToLower(filepath.Ext(filename))
			commandName := strings.ToLower(strings.TrimSuffix(filename, ext))

			var mediaType string
			switch ext {
			case ".mp3", ".wav", ".ogg":
				mediaType = "audio"
			case ".mp4", ".webm":
				mediaType = "video"
			default:
				continue
			}

			relativePath := folderName + "/" + filename

			if _, exists := commands[commandName]; exists {
				logger.Warn("Duplicate command detected, skipping", zap.String("command", commandName), zap.String("path", relativePath))
			} else {
				commands[commandName] = CommandData{
					Filename:   relativePath,
					Permission: permission,
					MediaType:  mediaType,
				}
			}
		}
	}

	return commands, nil
}

// Start initiates the Twitch IRC connection.
func (c *ChatClient) Start() {
	c.logger.Info("Connecting to Twitch IRC...")
	c.client = twitch.NewClient(c.config.BotUsername, c.config.BotOAuthToken)
	// Force port 443 (SSL)
	c.client.IrcAddress = "irc.chat.twitch.tv:443"

	c.client.OnPrivateMessage(c.handlePrivateMessage)

	c.client.OnConnect(func() {
		c.logger.Info("Connected to IRC channel", zap.String("channel", c.config.ChannelToJoin))
	})

	c.client.Join(c.config.ChannelToJoin)

	// Background reconnection loop
	go func() {
		for {
			if err := c.client.Connect(); err != nil {
				c.logger.Error("IRC Connection failed. Retrying in 10s...", zap.Error(err))
				time.Sleep(10 * time.Second)
			}
		}
	}()
}

func (c *ChatClient) handlePrivateMessage(message twitch.PrivateMessage) {
	// 1. EMOTE WALL (Broadcasts valid emotes to WebSocket)
	if len(message.Emotes) > 0 {
		var emoteURLs []string
		for _, emote := range message.Emotes {
			for i := 0; i < emote.Count; i++ {
				// Twitch CDN URL format 3.0 (Scale)
				url := "https://static-cdn.jtvnw.net/emoticons/v2/" + emote.ID + "/default/dark/3.0"
				emoteURLs = append(emoteURLs, url)
			}
		}

		if len(emoteURLs) > 0 {
			payload := EmoteWallPayload{
				Type:   "emote_wall",
				Emotes: emoteURLs,
			}
			payloadBytes, _ := json.Marshal(payload)
			c.hub.Broadcast <- payloadBytes
		}
	}

	// 2. Check for Command Prefix
	if !strings.HasPrefix(message.Message, "!") {
		return
	}

	rawCommand := strings.Fields(message.Message)[0]
	commandName := strings.ToLower(strings.TrimPrefix(rawCommand, "!"))

	// 3. LIST COMMANDS Logic (!commands)
	if commandName == "commands" || commandName == "comandi" {
		c.handleListCommands(message.Channel)
		return
	}

	// 4. MEDIA COMMAND Logic
	cmdData, exists := c.commands[commandName]
	if !exists {
		return
	}

	// Permission check
	if !c.hasPermission(message.User, cmdData.Permission) {
		return
	}

	// --- COOLDOWN CHECK ---
	if lastUsed, ok := c.lastUsage[commandName]; ok {
		if time.Since(lastUsed) < c.cooldownDuration {
			c.logger.Info("Command on cooldown", zap.String("command", commandName), zap.String("user", message.User.Name))
			return
		}
	}
	c.lastUsage[commandName] = time.Now()
	// ----------------------

	c.logger.Info("Command triggered", zap.String("command", commandName), zap.String("user", message.User.Name))

	payload := ChatAlertPayload{
		Type:      "sound_command",
		Filename:  cmdData.Filename,
		MediaType: cmdData.MediaType,
	}

	payloadBytes, _ := json.Marshal(payload)
	c.hub.Broadcast <- payloadBytes
}

// handleListCommands constructs and sends the list of available commands.
func (c *ChatClient) handleListCommands(channel string) {
	// Check outgoing rate limit before sending
	if err := c.sayLimiter.Wait(context.Background()); err != nil {
		c.logger.Warn("Rate limit exceeded for outgoing message", zap.Error(err))
		return
	}

	var everyone []string
	var subs []string
	var vips []string

	for name, data := range c.commands {
		cmd := "!" + name
		switch data.Permission {
		case PermissionEveryone:
			everyone = append(everyone, cmd)
		case PermissionSubscriber:
			subs = append(subs, cmd)
		case PermissionVIP:
			vips = append(vips, cmd)
		}
	}

	sort.Strings(everyone)
	sort.Strings(subs)
	sort.Strings(vips)

	var sb strings.Builder

	if len(everyone) > 0 {
		sb.WriteString(strings.Join(everyone, ", "))
	}

	if len(subs) > 0 {
		if sb.Len() > 0 {
			sb.WriteString(" / ")
		}
		sb.WriteString("Subscribers: ")
		sb.WriteString(strings.Join(subs, ", "))
	}

	if len(vips) > 0 {
		if sb.Len() > 0 {
			sb.WriteString(" / ")
		}
		sb.WriteString("Vips: ")
		sb.WriteString(strings.Join(vips, ", "))
	}

	response := sb.String()
	if response == "" {
		response = "No active commands found."
	}
	c.client.Say(channel, response)
}

// hasPermission checks Twitch badges against required level
func (c *ChatClient) hasPermission(user twitch.User, requiredLevel string) bool {
	// Broadcasters and Mods have all permissions
	if _, ok := user.Badges["broadcaster"]; ok {
		return true
	}
	if _, ok := user.Badges["moderator"]; ok {
		return true
	}

	switch requiredLevel {
	case PermissionEveryone:
		return true
	case PermissionSubscriber:
		// Checks for sub or founder badge
		_, isSub := user.Badges["subscriber"]
		_, isFounder := user.Badges["founder"]
		return isSub || isFounder
	case PermissionVIP:
		_, isVIP := user.Badges["vip"]
		return isVIP
	default:
		return false
	}
}
