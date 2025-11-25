package config

import (
	"os"

	"gopkg.in/yaml.v2"
)

// Config holds the global application configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Twitch   TwitchConfig   `yaml:"twitch"`
	YouTube  YouTubeConfig  `yaml:"youtube"`
}

// ServerConfig defines HTTP server settings.
type ServerConfig struct {
	Address       string `yaml:"address"`
	Port          string `yaml:"port"`           // Main public port (e.g., 8000)
	TestPort      string `yaml:"test_port"`      // Private test port (e.g., 8001)
	BaseURL       string `yaml:"base_url"`       // Public URL for Webhooks

	// PathPrefix is the security string used by the Reverse Proxy (e.g., "/vlxrobot")
	// It is NOT used for routing (Go listens on /), but for generating HTML links.
	PathPrefix    string `yaml:"path_prefix"`
	WebsocketPath string `yaml:"websocket_path"` // Internal endpoint (e.g., "/ws")
}

// DatabaseConfig defines PostgreSQL connection settings.
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
}

// TwitchConfig defines API credentials and webhook settings.
type TwitchConfig struct {
	ClientID        string           `yaml:"client_id"`
	ClientSecret    string           `yaml:"client_secret"`
	RedirectURI     string           `yaml:"redirect_uri"`
	ChannelName     string           `yaml:"channel_name"`
	UserAccessToken string           `yaml:"user_access_token"`
	WebhookSecret   string           `yaml:"webhook_secret"`
	Chat            TwitchChatConfig `yaml:"chat"`
}

// TwitchChatConfig defines IRC bot credentials.
type TwitchChatConfig struct {
	BotUsername   string `yaml:"bot_username"`
	BotOAuthToken string `yaml:"bot_token"`
	ChannelToJoin string `yaml:"channel_to_join"`
	CommandCooldown  int `yaml:"command_cooldown"`
}

// YouTubeConfig defines API credentials for YouTube.
type YouTubeConfig struct {
	APIKey  string           `yaml:"api_key"`
	ChannelID string         `yaml:"channel_id"`
	PollingInterval int      `yaml:"polling_interval"`
	Monitor MonitoringConfig `yaml:"monitor"`
}

// MonitoringConfig holds lists of IDs to monitor.
type MonitoringConfig struct {
	ChannelIDs []string `yaml:"channel_ids"`
}

// Load reads and parses the YAML configuration file.
func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
