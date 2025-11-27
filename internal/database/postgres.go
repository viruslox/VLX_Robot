package database

import (
	"database/sql"
	"fmt"
	"time"

	"VLX_Robot/internal/config"

	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

// DB is a wrapper around the sql.DB connection pool.
type DB struct {
	sql    *sql.DB
	logger *zap.Logger
}

// TwitchCredentials maps to the 'twitch_credentials' table
type TwitchCredentials struct {
	UserID       string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// TwitchSubscription maps to the 'twitch_subscriptions' table
type TwitchSubscription struct {
	ID        string
	UserID    string
	EventType string
	Status    string
	CreatedAt time.Time
}

// YouTubeState maps to the 'youtube_state' table
type YouTubeState struct {
	ChannelID     string
	LiveChatID    sql.NullString
	NextPageToken sql.NullString
	UpdatedAt     time.Time
}

// NewConnection creates, configures, and tests a new connection.
func NewConnection(cfg config.DatabaseConfig, logger *zap.Logger) (*DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
	)

	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open DB connection: %w", err)
	}

	if err = sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping DB: %w", err)
	}

	logger.Info("Database connection established")
	return &DB{sql: sqlDB, logger: logger}, nil
}

// Close gracefully closes the database connection pool.
func (db *DB) Close() {
	if err := db.sql.Close(); err != nil {
		db.logger.Error("Error closing DB", zap.Error(err))
	}
}

func (db *DB) GetTwitchCredentials(userID string) (*TwitchCredentials, error) {
	creds := &TwitchCredentials{UserID: userID}
	query := `SELECT access_token, refresh_token, expires_at FROM twitch_credentials WHERE user_id = $1`
	err := db.sql.QueryRow(query, userID).Scan(&creds.AccessToken, &creds.RefreshToken, &creds.ExpiresAt)
	return creds, err
}

func (db *DB) UpsertTwitchCredentials(creds *TwitchCredentials) error {
	query := `
		INSERT INTO twitch_credentials (user_id, access_token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET
			access_token = EXCLUDED.access_token,
			refresh_token = EXCLUDED.refresh_token,
			expires_at = EXCLUDED.expires_at
	`
	_, err := db.sql.Exec(query, creds.UserID, creds.AccessToken, creds.RefreshToken, creds.ExpiresAt)
	return err
}

func (db *DB) GetSubscription(userID, eventType string) (*TwitchSubscription, error) {
	sub := &TwitchSubscription{}
	query := `SELECT id, status, created_at FROM twitch_subscriptions WHERE user_id = $1 AND event_type = $2`
	err := db.sql.QueryRow(query, userID, eventType).Scan(&sub.ID, &sub.Status, &sub.CreatedAt)
	if err != nil {
		return nil, err
	}
	sub.UserID = userID
	sub.EventType = eventType
	return sub, nil
}

func (db *DB) CreateSubscription(sub *TwitchSubscription) error {
	query := `
		INSERT INTO twitch_subscriptions (id, user_id, event_type, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := db.sql.Exec(query, sub.ID, sub.UserID, sub.EventType, sub.Status, sub.CreatedAt)
	return err
}

func (db *DB) DeleteSubscription(subscriptionID string) error {
	query := `DELETE FROM twitch_subscriptions WHERE id = $1`
	_, err := db.sql.Exec(query, subscriptionID)
	return err
}

func (db *DB) GetYouTubeState(channelID string) (*YouTubeState, error) {
	state := &YouTubeState{ChannelID: channelID}
	query := `SELECT live_chat_id, next_page_token, updated_at FROM youtube_state WHERE channel_id = $1`
	err := db.sql.QueryRow(query, channelID).Scan(&state.LiveChatID, &state.NextPageToken, &state.UpdatedAt)
	return state, err
}

func (db *DB) UpsertYouTubeState(state *YouTubeState) error {
	query := `
		INSERT INTO youtube_state (channel_id, live_chat_id, next_page_token, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel_id) DO UPDATE SET
			live_chat_id = EXCLUDED.live_chat_id,
			next_page_token = EXCLUDED.next_page_token,
			updated_at = EXCLUDED.updated_at
	`
	_, err := db.sql.Exec(query, state.ChannelID, state.LiveChatID, state.NextPageToken, state.UpdatedAt)
	return err
}
