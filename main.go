package main

import (
	"log"
	"path/filepath"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/database"
	"VLX_Robot/internal/server"
	"VLX_Robot/internal/twitch"
	"VLX_Robot/internal/websocket"
	"VLX_Robot/internal/youtube"
)

func main() {
	// 1. Load config
	cfg, err := config.Load("config.yml")
	if err != nil {
		log.Fatalf("[FATAL] Config load error: %v", err)
	}

	// 2. Database connection
	db, err := database.NewConnection(cfg.Database)
	if err != nil {
		log.Fatalf("[FATAL] DB connection error: %v", err)
	}
	defer db.Close()

	// 3. WebSocket Hub
	hub := websocket.NewHub()
	go hub.Run()

	// 4. Twitch Client (API & EventSub)
	channels := []string{cfg.Twitch.ChannelName}

	twitchClient, err := twitch.NewClient(cfg.Twitch, channels, cfg.Server.BaseURL, hub, db)
	if err != nil {
		log.Printf("[ERROR] Twitch Client init failed: %v", err)
	} else {
		if err := twitchClient.StartMonitoring(channels); err != nil {
			log.Printf("[ERROR] Twitch monitoring failed: %v", err)
		}
	}

	// 5. Twitch Chat Bot
	cmdMap, err := twitch.ScanAudioCommands(filepath.Join("static", "sounds", "commands"))
	if err != nil {
		log.Printf("[WARN] Audio commands scan failed: %v", err)
	} else {
		chatClient := twitch.NewChatClient(cfg.Twitch.Chat, hub, cmdMap)
		chatClient.Start()
	}

	// 6. YouTube Client
	_ = youtube.NewClient(cfg.YouTube, hub, db)

	// 7. HTTP Server
	srv := server.NewServer(cfg, hub, twitchClient)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("[FATAL] HTTP Server error: %v", err)
	}
}
