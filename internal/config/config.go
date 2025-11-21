package config

import (
	"os"

	"gopkg.in/yaml.v2"
)

// Config struct to hold all configuration
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Twitch   TwitchConfig   `yaml:"twitch"`
	YouTube  YouTubeConfig  `yaml:"youtube"`
}

type ServerConfig struct {
	Address       string `yaml:"address"`
	Port          string `yaml:"port"`
	BaseURL       string `yaml:"base_url"`
	WebsocketPath string `yaml:"websocket_path"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
}

type TwitchConfig struct {
	ClientID        string           `yaml:"client_id"`
	ClientSecret    string           `yaml:"client_secret"`
	RedirectURI     string           `yaml:"redirect_uri"`
	ChannelName     string           `yaml:"channel_name"`
	UserAccessToken string           `yaml:"user_access_token"`
	WebhookSecret   string           `yaml:"webhook_secret"`
	Chat            TwitchChatConfig `yaml:"chat"`
}

type TwitchChatConfig struct {
	BotUsername   string `yaml:"bot_username"`
	BotOAuthToken string `yaml:"bot_token"`
	ChannelToJoin string `yaml:"channel_to_join"`
}

type YouTubeConfig struct {
	APIKey  string           `yaml:"api_key"`
	Monitor MonitoringConfig `yaml:"monitor"`
}

type MonitoringConfig struct {
	ChannelIDs []string `yaml:"channel_ids"`
}

// LoadConfig reads configuration from a YAML file
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
