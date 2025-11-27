package websocket

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestHubIntegration(t *testing.T) {
	logger := zap.NewNop()
	hub := NewHub(logger)

	// Start Hub in background
	go hub.Run()

	// 1. Simulate a Client
	mockClient := &Client{
		hub:    hub,
		send:   make(chan []byte, 256),
		logger: logger,
	}

	// 2. Register Client
	hub.register <- mockClient
	
	// Give a moment for registration
	time.Sleep(50 * time.Millisecond)

	// Check internal state (white-box testing)
	if _, ok := hub.clients[mockClient]; !ok {
		t.Fatal("Client was not registered in Hub")
	}

	// 3. Broadcast Message
	testMsg := []byte("test_payload")
	hub.Broadcast <- testMsg

	// 4. Verify Receipt
	select {
	case received := <-mockClient.send:
		if string(received) != string(testMsg) {
			t.Errorf("Expected %s, got %s", testMsg, received)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout: Client did not receive broadcast")
	}

	// 5. Unregister Client
	hub.unregister <- mockClient
	time.Sleep(50 * time.Millisecond)

	if _, ok := hub.clients[mockClient]; ok {
		t.Fatal("Client was not removed from Hub")
	}
}

