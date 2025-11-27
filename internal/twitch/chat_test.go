package twitch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gempir/go-twitch-irc/v4"
	"go.uber.org/zap"
)

func TestScanAudioCommands(t *testing.T) {
	// 1. Setup temporary directory structure
	tmpDir := t.TempDir()
	
	subDirs := []string{"everyone", "subscribers", "vips"}
	for _, dir := range subDirs {
		if err := os.Mkdir(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// 2. Create dummy files
	files := []struct {
		path string
	}{
		{"everyone/hello.mp3"},
		{"subscribers/secret.wav"},
		{"vips/exclusive.mp4"},
		{"everyone/ignored.txt"}, // Should be ignored
	}

	for _, f := range files {
		fullPath := filepath.Join(tmpDir, f.path)
		if err := os.WriteFile(fullPath, []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// 3. Run Scan
	logger := zap.NewNop()
	cmds, err := ScanAudioCommands(tmpDir, logger)
	if err != nil {
		t.Fatalf("ScanAudioCommands failed: %v", err)
	}

	// 4. Assertions
	expectedCount := 3
	if len(cmds) != expectedCount {
		t.Errorf("Expected %d commands, got %d", expectedCount, len(cmds))
	}

	if data, ok := cmds["hello"]; !ok || data.Permission != PermissionEveryone || data.MediaType != "audio" {
		t.Errorf("Incorrect parsing for 'hello' command")
	}

	if data, ok := cmds["exclusive"]; !ok || data.Permission != PermissionVIP || data.MediaType != "video" {
		t.Errorf("Incorrect parsing for 'exclusive' command")
	}
}

func TestHasPermission(t *testing.T) {
	// Helper to create a ChatClient with minimal config
	client := &ChatClient{}

	tests := []struct {
		name          string
		userBadges    map[string]int
		requiredLevel string
		expected      bool
	}{
		// Everyone Level
		{"Everyone_NoBadges", map[string]int{}, PermissionEveryone, true},
		
		// Subscriber Level
		{"Sub_NoBadges", map[string]int{}, PermissionSubscriber, false},
		{"Sub_WithSubBadge", map[string]int{"subscriber": 1}, PermissionSubscriber, true},
		{"Sub_WithFounderBadge", map[string]int{"founder": 1}, PermissionSubscriber, true},
		
		// VIP Level
		{"VIP_NoBadges", map[string]int{}, PermissionVIP, false},
		{"VIP_WithVIPBadge", map[string]int{"vip": 1}, PermissionVIP, true},
		{"VIP_WithModBadge", map[string]int{"moderator": 1}, PermissionVIP, true}, // Mods inherit VIP perms
		{"VIP_WithBroadcaster", map[string]int{"broadcaster": 1}, PermissionVIP, true}, // Streamer inherits all
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := twitch.User{Badges: tt.userBadges}
			if got := client.hasPermission(user, tt.requiredLevel); got != tt.expected {
				t.Errorf("hasPermission() = %v, want %v", got, tt.expected)
			}
		})
	}
}
