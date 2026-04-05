package main

import (
	"flag"
	"log"
	"os"
	"strings"
	"time"
)

const version = "1.0.0"

func main() {
	token := flag.String("token", "", "Valpero agent token (val_agnt_...)")
	apiURL := flag.String("api", "https://valpero.com/api", "API base URL")
	interval := flag.Int("interval", 30, "Metrics push interval in seconds")
	flag.Parse()

	// Also accept token from environment
	if *token == "" {
		*token = os.Getenv("VALPERO_TOKEN")
	}
	if *token == "" {
		log.Fatal("[valpero-agent] ERROR: --token is required (or set VALPERO_TOKEN env var)")
	}
	if !strings.HasPrefix(*token, "val_agnt_") {
		log.Fatal("[valpero-agent] ERROR: invalid token format, expected val_agnt_...")
	}

	*apiURL = strings.TrimRight(*apiURL, "/")

	log.Printf("[valpero-agent] v%s starting (API: %s, interval: %ds)", version, *apiURL, *interval)

	sender := NewSender(*apiURL, *token)

	// Register on startup (retry until success)
	for {
		if err := sender.Register(); err != nil {
			log.Printf("[valpero-agent] Registration failed: %v — retrying in 15s", err)
			time.Sleep(15 * time.Second)
			continue
		}
		log.Println("[valpero-agent] Registered successfully")
		break
	}

	// Push loop
	ticker := time.NewTicker(time.Duration(*interval) * time.Second)
	defer ticker.Stop()

	// Push immediately, then on each tick
	push(sender)
	for range ticker.C {
		push(sender)
	}
}

func push(sender *Sender) {
	metrics, err := Collect()
	if err != nil {
		log.Printf("[valpero-agent] Collection error: %v", err)
		return
	}
	if err := sender.Push(metrics); err != nil {
		log.Printf("[valpero-agent] Push failed: %v", err)
	}
}
