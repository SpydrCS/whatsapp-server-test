package utils

import (
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Handle history sync events
func HandleHistorySync(client *whatsmeow.Client, messageStore *MessageStore, historySync *events.HistorySync, logger waLog.Logger) {
	fmt.Printf("Received history sync event with %d conversations\n", len(historySync.Data.Conversations))

	syncedCount := 0
	for _, conversation := range historySync.Data.Conversations {
		// Parse JID from the conversation
		if conversation.ID == nil {
			continue
		}

		chatJID := *conversation.ID

		// Try to parse the JID
		jid, err := types.ParseJID(chatJID)
		if err != nil {
			logger.Warnf("Failed to parse JID %s: %v", chatJID, err)
			continue
		}

		// Get appropriate chat name by passing the history sync conversation directly
		name := getChatName(client, messageStore, jid, chatJID, conversation, "", logger)

		// Process messages
		messages := conversation.Messages
		if len(messages) > 0 {
			// Update chat with latest message timestamp
			latestMsg := messages[0]
			if latestMsg == nil || latestMsg.Message == nil {
				continue
			}

			// Get timestamp from message info
			ts := latestMsg.Message.GetMessageTimestamp()
			if ts == 0 {
				continue
			}
			timestamp := time.Unix(int64(ts), 0)

			messageStore.storeChat(chatJID, name, timestamp)

			// Store messages
			for _, msg := range messages {
				if msg == nil || msg.Message == nil {
					continue
				}

				// Extract text content
				var content string
				if msg.Message.Message != nil {
					if conv := msg.Message.Message.GetConversation(); conv != "" {
						content = conv
					} else if ext := msg.Message.Message.GetExtendedTextMessage(); ext != nil {
						content = ext.GetText()
					}
				}

				// Extract media info
				var mediaType, filename, url string
				var mediaKey, fileSHA256, fileEncSHA256 []byte
				var fileLength uint64

				if msg.Message.Message != nil {
					mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength = extractMediaInfo(msg.Message.Message)
				}

				// Log the message content for debugging
				logger.Infof("Message content: %v, Media Type: %v", content, mediaType)

				// Skip messages with no content and no media
				if content == "" && mediaType == "" {
					continue
				}

				// Determine sender
				var sender string
				isFromMe := false
				if msg.Message.Key != nil {
					if msg.Message.Key.FromMe != nil {
						isFromMe = *msg.Message.Key.FromMe
					}
					if !isFromMe && msg.Message.Key.Participant != nil && *msg.Message.Key.Participant != "" {
						sender = *msg.Message.Key.Participant
					} else if isFromMe {
						sender = client.Store.ID.User
					} else {
						sender = jid.User
					}
				} else {
					sender = jid.User
				}

				// Store message
				msgID := ""
				if msg.Message.Key != nil && msg.Message.Key.ID != nil {
					msgID = *msg.Message.Key.ID
				}

				// Get message timestamp
				ts := msg.Message.GetMessageTimestamp()
				if ts == 0 {
					continue
				}
				timestamp := time.Unix(int64(ts), 0)

				err = messageStore.storeMessage(
					msgID,
					chatJID,
					sender,
					content,
					timestamp,
					isFromMe,
					mediaType,
					filename,
					url,
					mediaKey,
					fileSHA256,
					fileEncSHA256,
					fileLength,
				)
				if err != nil {
					logger.Warnf("Failed to store history message: %v", err)
				} else {
					syncedCount++
					// Log successful message storage
					if mediaType != "" {
						logger.Infof("Stored message: [%s] %s -> %s: [%s: %s] %s",
							timestamp.Format("2006-01-02 15:04:05"), sender, chatJID, mediaType, filename, content)
					} else {
						logger.Infof("Stored message: [%s] %s -> %s: %s",
							timestamp.Format("2006-01-02 15:04:05"), sender, chatJID, content)
					}
				}
			}
		}
	}

	fmt.Printf("History sync complete. Stored %d messages.\n", syncedCount)
}

// Handle regular incoming messages with media support
func HandleMessage(client *whatsmeow.Client, messageStore *MessageStore, awsConfig aws.Config, msg *events.Message, logger waLog.Logger) {
	messageID := msg.Info.ID
	chatJID := msg.Info.Chat.String()
	sender := msg.Info.Sender.User
	
	// Extract text content
	content, mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength := extractMessageContent(msg.Message)
	
	// SKip if no text content and mediaType is not "audio"
	if content == "" && mediaType != "audio" {
		logger.Infof("Ignoring non-audio media type: %s", mediaType)
		return
	}
	
	// Skip if there's no content and no media
	if content == "" && mediaType == "" {
		logger.Infof("No text or media content found in message from %s", chatJID)
		return
	}
	
	// Get appropriate chat name (pass nil for conversation since we don't have one for regular messages)
	name := getChatName(client, messageStore, msg.Info.Chat, chatJID, nil, sender, logger)

	// Update "chat" table with latest message timestamp
	err := messageStore.storeChat(chatJID, name, msg.Info.Timestamp)
	if err != nil {
		logger.Warnf("Failed to store chat: %v", err)
	} else {
		logger.Infof("Updated chat %s with latest timestamp %s", chatJID, msg.Info.Timestamp.Format("2006-01-02 15:04:05"))
	}

	// Store message in database
	err = messageStore.storeMessage(
		messageID,
		chatJID,
		sender,
		content,
		msg.Info.Timestamp,
		msg.Info.IsFromMe,
		mediaType,
		filename,
		url,
		mediaKey,
		fileSHA256,
		fileEncSHA256,
		fileLength,
	)

	if err != nil {
		logger.Warnf("Failed to store message: %v", err)
		return
	} else {
		logger.Infof("Stored message %s from %s in chat %s", messageID, sender, chatJID)
	}
	
	// Upload message to S3
	filePath, err := uploadMessageToS3(client, awsConfig, os.Getenv("AWS_S3_BUCKET_NAME"), content, messageID, chatJID, mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength)
	if err != nil {
		logger.Warnf("Failed to upload message to S3: %v", err)
		return
	} else {
		logger.Infof("Uploaded message %s to S3 %s", messageID, filePath)
	}

	// Log message reception
	logMessageReception(msg, sender, mediaType, filename, content)
}
