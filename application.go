package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"whatsapp-server-test/utils"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"

	"github.com/aws/aws-sdk-go-v2/config"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)



func main() {
	// Set up logger
	logger := waLog.Stdout("Client", "INFO", true)
	logger.Infof("Starting WhatsApp client...")

	// TODO: remove for prod
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Initialize database
	container, err := utils.InitDB()
	if err != nil {
		logger.Errorf("Failed to initialize database: %v", err)
		return
	}
	
	// Get device store - This contains session information
	deviceStore, err := utils.InitDeviceStore(container, logger)
	if err != nil {
		logger.Errorf("Failed to get device: %v", err)
		return
	}

	// Create client instance
	client := whatsmeow.NewClient(deviceStore, logger)
	if client == nil {
		logger.Errorf("Failed to create WhatsApp client")
		return
	}

	// Initialize message store
	messageStore, err := utils.InitMessageStore()
	if err != nil {
		logger.Errorf("Failed to initialize message store: %v", err)
		return
	}
	defer messageStore.Close()

	// Initialize AWS config
	// Uses env vars
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Printf("Failed to initialize AWS config: %v\n", err)
		return
	}


	// Setup event handling for messages and history sync
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			// Process regular messages
			utils.HandleMessage(client, messageStore, cfg, v, logger)

		case *events.HistorySync:
			// Process history sync events
			utils.HandleHistorySync(client, messageStore, v, logger)

		case *events.Connected:
			logger.Infof("Connected to WhatsApp")

		case *events.LoggedOut:
			logger.Warnf("Device logged out, please pair phone to log in again")
		}
	})

	// Connect to WhatsApp
	success := utils.ConnectToWhatsApp(client, logger)
	if !success {
		return
	}

	// Wait a moment for connection to stabilize
	time.Sleep(2 * time.Second)

	if !client.IsConnected() {
		logger.Errorf("Failed to establish stable connection")
		return
	}

	fmt.Println("\n✓ Connected to WhatsApp!")

    port := os.Getenv("PORT")
    if port == "" {
        port = "5000"
    }
    
	utils.StartRESTServer(client, port)

	// Create a channel to keep the main goroutine alive
	exitChan := make(chan os.Signal, 1)
	signal.Notify(exitChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("REST server is running. Press Ctrl+C to disconnect and exit.")

	// Wait for termination signal
	<-exitChan

	fmt.Println("Disconnecting...")
	// Disconnect client
	client.Disconnect()
}