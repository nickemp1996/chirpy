package main

import (
	"log"
	"net/http"

	_ "github.com/lib/pq"
)

func readinessEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	message := "OK"
	_, err := w.Write([]byte(message))
	if err != nil {
		log.Printf("Error writing response: %v", err)
	}
}
