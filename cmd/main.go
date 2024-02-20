package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gabrielmbarboza/dealer/internal/dealer"
)

func main() {
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe("0.0.0.0:3000", nil))
}

func handler(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.RequestURI()

	w.Header().Set("Content-Type", "application/json")

	if strings.Compare(uri, "/") == 0 {
		data, err := json.Marshal(dealer.ProjectInfo())
		if err != nil {
			log.Fatal(err)
		}
		w.Write(data)
		return
	}

	serviceName := strings.Split(uri, "/")[1]

	if serviceName == "" {
		log.Fatal("Could not find the service")
	}
}
