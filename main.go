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
	// 1. Load configuration
	cfg, err := config.Load("config.yml")
	if err != nil {
		log.Fatalf("[FATAL] Config load error: %v", err)
	}

	// 2. Initialize Database connection
	db, err := database.NewConnection(cfg.Database)
	if err != nil {
		log.Fatalf("[FATAL] DB connection error: %v", err)
	}
	defer db.Close()

	// 3. Start WebSocket Hub
	hub := websocket.NewHub()
	go hub.Run()

	// 4. Initialize Twitch API Client (EventSub)
	monitorChannels := []string{cfg.Twitch.ChannelName}
	twitchClient, err := twitch.NewClient(cfg.Twitch, monitorChannels, cfg.Server.BaseURL, hub, db)
	if err != nil {
		log.Printf("[ERROR] Twitch Client init failed: %v", err)
	} else {
		if err := twitchClient.StartMonitoring(monitorChannels); err != nil {
			log.Printf("[ERROR] Twitch monitoring failed: %v", err)
		}
	}

	// 5. Initialize Twitch Chat Bot
	cmdMap, err := twitch.ScanAudioCommands(filepath.Join("static", "chat"))
	if err != nil {
		log.Printf("[WARN] Audio commands scan failed: %v", err)
	} else {
		chatClient := twitch.NewChatClient(cfg.Twitch.Chat, hub, cmdMap)
		chatClient.Start()
	}

	// 6. Initialize YouTube Client (Polling)
	youtubeClient, err := youtube.NewClient(cfg.YouTube, hub, db, cmdMap)
	if err != nil {
		log.Printf("[ERROR] YouTube Client init failed: %v", err)
	} else if youtubeClient != nil {
		youtubeClient.Start()
	}

	// 7. Start the Private Test Server (e.g., Port 8001)
	testPort := cfg.Server.TestPort
	if testPort == "" {
		testPort = "8001" // Default fallback
	}

	testSrv := server.NewTestServer(testPort, hub)
	go func() {
		if err := testSrv.ListenAndServe(); err != nil {
			log.Printf("[ERROR] Test Server failed: %v", err)
		}
	}()

	// 8. Start the Main Public Server (e.g., Port 8000)
	srv := server.NewServer(cfg, hub, twitchClient)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("[FATAL] Main HTTP Server error: %v", err)
	}
}
