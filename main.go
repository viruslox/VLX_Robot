package main

import (
	"path/filepath"

	"VLX_Robot/internal/config"
	"VLX_Robot/internal/database"
	"VLX_Robot/internal/server"
	"VLX_Robot/internal/twitch"
	"VLX_Robot/internal/websocket"
	"VLX_Robot/internal/youtube"

	"go.uber.org/zap"
)

func main() {
	// 1. Initialize Structured Logger (Zap)
	logger, _ := zap.NewProduction()
	defer logger.Sync() // Flushes buffer, if any

	// 2. Load configuration
	cfg, err := config.Load("config.yml")
	if err != nil {
		logger.Fatal("Config load error", zap.Error(err))
	}

	// 3. Initialize Database connection
	db, err := database.NewConnection(cfg.Database, logger)
	if err != nil {
		logger.Fatal("DB connection error", zap.Error(err))
	}
	defer db.Close()

	// 4. Start WebSocket Hub
	hub := websocket.NewHub(logger)
	go hub.Run()

	// 5. Initialize Twitch API Client (EventSub)
	monitorChannels := []string{cfg.Twitch.ChannelName}
	twitchClient, err := twitch.NewClient(cfg.Twitch, monitorChannels, cfg.Server.BaseURL, hub, db, logger)
	if err != nil {
		logger.Error("Twitch Client init failed", zap.Error(err))
	} else {
		if err := twitchClient.StartMonitoring(monitorChannels); err != nil {
			logger.Error("Twitch monitoring failed", zap.Error(err))
		}
	}

	// 6. Initialize Twitch Chat Bot
	cmdMap, err := twitch.ScanAudioCommands(filepath.Join("static", "chat"), logger)
	if err != nil {
		logger.Warn("Audio commands scan failed", zap.Error(err))
	} else {
		chatClient := twitch.NewChatClient(cfg.Twitch.Chat, hub, cmdMap, logger)
		chatClient.Start()
	}

	// 7. Initialize YouTube Client (Polling) with Rate Limiting
	youtubeClient, err := youtube.NewClient(cfg.YouTube, hub, db, cmdMap, logger)
	if err != nil {
		logger.Error("YouTube Client init failed", zap.Error(err))
	} else if youtubeClient != nil {
		youtubeClient.Start()
	}

	// 8. Start Private Test Server
	testPort := cfg.Server.TestPort
	if testPort == "" {
		testPort = "8001"
	}
	testSrv := server.NewTestServer(testPort, hub, logger)
	go func() {
		if err := testSrv.ListenAndServe(); err != nil {
			logger.Error("Test Server failed", zap.Error(err))
		}
	}()

	// 9. Start Main Public Server
	srv := server.NewServer(cfg, hub, twitchClient, logger)
	if err := srv.ListenAndServe(); err != nil {
		logger.Fatal("Main HTTP Server error", zap.Error(err))
	}
}
