package utils

import (
	"database/sql"
	"fmt"
	"os"
	"time"
)

// Database handler for storing message history
type MessageStore struct {
	Db *sql.DB
}

// function to create postgres database string
func CreatePostgresConnectionString() string {
	username := os.Getenv("RDS_USERNAME")
	password := os.Getenv("RDS_PASSWORD")
	host := os.Getenv("RDS_HOSTNAME")
	port := os.Getenv("RDS_PORT")
	database := os.Getenv("RDS_DB_NAME")
	return "postgres://" + username + ":" + password + "@" + host + ":" + port + "/" + database
}

// Initialize message store
func NewMessageStore(dbConnectionString string) (*MessageStore, error) {
	// Open SQLite database for messages
	db, err := sql.Open("pgx", dbConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open message database: %v", err)
	}

	// Create tables if they don't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			name TEXT,
			last_message_time TIMESTAMP
		);
		
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT,
			chat_jid TEXT,
			sender TEXT,
			content TEXT,
			timestamp TIMESTAMP,
			is_from_me BOOLEAN,
			media_type TEXT,
			filename TEXT,
			url TEXT,
			media_key BYTEA,
			file_sha256 BYTEA,
			file_enc_sha256 BYTEA,
			file_length INTEGER,
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES chats(jid)
		);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %v", err)
	}

	return &MessageStore{Db: db}, nil
}

// Get media info from the database
func (store *MessageStore) GetMediaInfo(id, chatJID string) (string, string, string, []byte, []byte, []byte, uint64, error) {
	var mediaType, filename, url string
	var mediaKey, fileSHA256, fileEncSHA256 []byte
	var fileLength uint64

	err := store.Db.QueryRow(
		"SELECT media_type, filename, url, media_key, file_sha256, file_enc_sha256, file_length FROM messages WHERE id = $1 AND chat_jid = $2",
		id, chatJID,
	).Scan(&mediaType, &filename, &url, &mediaKey, &fileSHA256, &fileEncSHA256, &fileLength)

	return mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength, err
}

// Close the database connection
func (store *MessageStore) Close() error {
	return store.Db.Close()
}

// Store a chat in the database
func (store *MessageStore) StoreChat(jid, name string, lastMessageTime time.Time) error {
	_, err := store.Db.Exec(
		`INSERT INTO chats (jid, name, last_message_time)
		VALUES ($1, $2, $3)
		ON CONFLICT (jid)
		DO UPDATE SET
		name = EXCLUDED.name,
		last_message_time = EXCLUDED.last_message_time;`,
		jid, name, lastMessageTime,
	)
	return err
}

// Store a message in the database
func (store *MessageStore) StoreMessage(id, chatJID, sender, content string, timestamp time.Time, isFromMe bool,
	mediaType, filename, url string, mediaKey, fileSHA256, fileEncSHA256 []byte, fileLength uint64) error {
	// Only store if there's actual content or media
	if content == "" && mediaType == "" {
		return nil
	}

	_, err := store.Db.Exec(
		`INSERT INTO messages (
			id, chat_jid, sender, content, timestamp, is_from_me, 
			media_type, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $13
		)
		ON CONFLICT (id, chat_jid) DO UPDATE SET
			sender = EXCLUDED.sender,
			content = EXCLUDED.content,
			timestamp = EXCLUDED.timestamp,
			is_from_me = EXCLUDED.is_from_me,
			media_type = EXCLUDED.media_type,
			filename = EXCLUDED.filename,
			url = EXCLUDED.url,
			media_key = EXCLUDED.media_key,
			file_sha256 = EXCLUDED.file_sha256,
			file_enc_sha256 = EXCLUDED.file_enc_sha256,
			file_length = EXCLUDED.file_length
		`,
		id, chatJID, sender, content, timestamp, isFromMe,
		mediaType, filename, url, mediaKey, fileSHA256,
		fileEncSHA256, fileLength,
	)
	return err
}