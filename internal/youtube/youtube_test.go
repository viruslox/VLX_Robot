package youtube

import (
	"encoding/json"
	"testing"
	"time"

	"VLX_Robot/internal/twitch"
	"VLX_Robot/internal/websocket"

	"go.uber.org/zap"
	"google.golang.org/api/youtube/v3"
)

func TestProcessMessages(t *testing.T) {
	// 1. Setup Hub to capture broadcasts
	logger := zap.NewNop()
	hub := websocket.NewHub(logger)
	
	// Create a dummy command map
	commands := twitch.AudioCommandsMap{
		"test": {Filename: "test.mp3", Permission: twitch.PermissionEveryone, MediaType: "audio"},
	}

	client := &Client{
		hub:      hub,
		commands: commands,
		logger:   logger,
	}

	// 2. Prepare mock messages
	messages := []*youtube.LiveChatMessage{
		{
			Snippet: &youtube.LiveChatMessageSnippet{
				DisplayMessage: "!test",
				SuperChatDetails: nil,
			},
			AuthorDetails: &youtube.LiveChatMessageAuthorDetails{
				DisplayName: "User1",
			},
		},
		{
			Snippet: &youtube.LiveChatMessageSnippet{
				SuperChatDetails: &youtube.LiveChatSuperChatDetails{
					AmountDisplayString: "$5.00",
					UserComment:         "Great stream!",
					Tier:                1,
				},
			},
			AuthorDetails: &youtube.LiveChatMessageAuthorDetails{
				DisplayName: "Donator1",
			},
		},
	}

	// 3. Run processing in a goroutine to not block reading from channel
	go func() {
		client.processMessages(messages)
	}()

	// 4. Assertions (Read from Hub.Broadcast)
	timeout := time.After(1 * time.Second)
	receivedCount := 0

	for i := 0; i < 2; i++ {
		select {
		case msg := <-hub.Broadcast:
			var payload map[string]interface{}
			if err := json.Unmarshal(msg, &payload); err != nil {
				t.Fatalf("Failed to unmarshal JSON: %v", err)
			}

			msgType := payload["type"].(string)
			if msgType == "sound_command" {
				if payload["filename"] != "test.mp3" {
					t.Errorf("Expected filename test.mp3, got %v", payload["filename"])
				}
			} else if msgType == "youtube_super_chat" {
				if payload["amount_string"] != "$5.00" {
					t.Errorf("Expected amount $5.00, got %v", payload["amount_string"])
				}
			} else {
				t.Errorf("Unexpected message type: %s", msgType)
			}
			receivedCount++
		case <-timeout:
			t.Fatal("Timeout waiting for broadcasts")
		}
	}

	if receivedCount != 2 {
		t.Errorf("Expected 2 broadcasts, got %d", receivedCount)
	}
}
