package twitch

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/websocket"

	"github.com/gempir/go-twitch-irc/v4"
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

func NewChatClient(cfg config.TwitchChatConfig, hub *websocket.Hub, commands AudioCommandsMap) *ChatClient {
	// Set a "safety default" in case the cooldown it is not set (or null) in the config file
	cd := cfg.CommandCooldown
	if cd <= 0 {
		cd = 15 // Default a 15 secondi se non specificato
	}

	return &ChatClient{
		config:           cfg,
		hub:              hub,
		commands:         commands,
		lastUsage:        make(map[string]time.Time),
		cooldownDuration: time.Duration(cd) * time.Second, // Conversione int -> Duration
	}
}

// ScanAudioCommands recursively scans command folders
func ScanAudioCommands(baseDir string) (AudioCommandsMap, error) {
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
			log.Printf("[WARN] Could not read folder '%s': %v", fullPath, err)
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
				log.Printf("[WARN] Duplicate command '!%s' in '%s'. Skipping.", commandName, relativePath)
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

func (c *ChatClient) Start() {
	log.Println("[INFO] [Chat] Connecting to Twitch IRC...")
	c.client = twitch.NewClient(c.config.BotUsername, c.config.BotOAuthToken)
	// Force port 443 instead of standard 6697 to avoid fw issues
	c.client.IrcAddress = "irc.chat.twitch.tv:443"

	c.client.OnPrivateMessage(c.handlePrivateMessage)

	c.client.OnConnect(func() {
		log.Printf("[INFO] [Chat] Connected to %s", c.config.ChannelToJoin)
	})

	c.client.Join(c.config.ChannelToJoin)

	// Background reconnection loop
	go func() {
		for {
			if err := c.client.Connect(); err != nil {
				log.Printf("[ERROR] [Chat] Connection failed: %v. Retrying in 10s...", err)
				time.Sleep(10 * time.Second)
			}
		}
	}()
}

func (c *ChatClient) handlePrivateMessage(message twitch.PrivateMessage) {
	// 1. EMOTE WALL (reads all messages)
	if len(message.Emotes) > 0 {
		var emoteURLs []string
		// used emotes list
		for _, emote := range message.Emotes {
			for i := 0; i < emote.Count; i++ {
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

	// 3. LIST COMMANDS Logic (!commands / !comandi)
	if commandName == "commands" || commandName == "comandi" {
		var everyone []string
		var subs []string
		var vips []string

		// Create rank based command lists
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

		// Everyone
		if len(everyone) > 0 {
			sb.WriteString(strings.Join(everyone, ", "))
		}

		// Subscribers
		if len(subs) > 0 {
			if sb.Len() > 0 {
				sb.WriteString(" / ")
			}
			sb.WriteString("Subscribers: ")
			sb.WriteString(strings.Join(subs, ", "))
		}

		// VIPs
		if len(vips) > 0 {
			if sb.Len() > 0 {
				sb.WriteString(" / ")
			}
			sb.WriteString("Vips: ")
			sb.WriteString(strings.Join(vips, ", "))
		}

		// Answer
		response := sb.String()
		if response == "" {
			response = "No Commands, No party."
		}
		c.client.Say(message.Channel, response)
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
	// Use c.cooldownDuration instead of constant
	if lastUsed, ok := c.lastUsage[commandName]; ok {
		if time.Since(lastUsed) < c.cooldownDuration {
			log.Printf("[INFO] [Chat] Command !%s is on cooldown. Ignored.", commandName)
			return
		}
	}
	c.lastUsage[commandName] = time.Now()
	// ----------------------

	log.Printf("[INFO] [Chat] !%s triggered by %s", commandName, message.User.Name)

	payload := ChatAlertPayload{
		Type:      "sound_command",
		Filename:  cmdData.Filename,
		MediaType: cmdData.MediaType,
	}

	payloadBytes, _ := json.Marshal(payload)
	c.hub.Broadcast <- payloadBytes
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
