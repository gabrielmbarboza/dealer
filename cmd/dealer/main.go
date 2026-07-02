package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	dealer "github.com/gabrielmbarboza/dealer/config"
	"github.com/gabrielmbarboza/dealer/gateway"
)

func main() {
	configPath := envOr("DEALER_CONFIG_PATH", "config.yml")

	pollInterval := gateway.DefaultPollInterval
	if raw := os.Getenv("DEALER_CONFIG_POLL_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("main: invalid DEALER_CONFIG_POLL_INTERVAL %q: %v", raw, err)
		}
		pollInterval = d
	}

	gw, err := gateway.New(configPath, gateway.Options{PollInterval: pollInterval})
	if err != nil {
		log.Fatalf("gateway: %v", err)
	}
	defer gw.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", infoHandler)
	mux.Handle("/", gw)

	log.Fatal(http.ListenAndServe("0.0.0.0:3000", mux))
}

func infoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(dealer.ProjectInfo()); err != nil {
		log.Printf("main: encode info response: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
