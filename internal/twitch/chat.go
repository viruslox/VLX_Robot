package twitch

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
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
	config   config.TwitchChatConfig
	hub      *websocket.Hub
	client   *twitch.Client
	commands AudioCommandsMap
}

// ChatAlertPayload defines the JSON sent to the overlay
type ChatAlertPayload struct {
	Type      string `json:"type"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
}

func NewChatClient(cfg config.TwitchChatConfig, hub *websocket.Hub, commands AudioCommandsMap) *ChatClient {
	return &ChatClient{
		config:   cfg,
		hub:      hub,
		commands: commands,
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
	if !strings.HasPrefix(message.Message, "!") {
		return
	}

	rawCommand := strings.Fields(message.Message)[0]
	commandName := strings.ToLower(strings.TrimPrefix(rawCommand, "!"))

	cmdData, exists := c.commands[commandName]
	if !exists {
		return
	}

	// Permission check
	if !c.hasPermission(message.User, cmdData.Permission) {
		return
	}

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
