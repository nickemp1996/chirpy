package main

import (
	"os"
	"fmt"
	"net/http"
)

func readinessEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	message := "OK"
	_, err := w.Write([]byte(message))
	if err != nil {
		fmt.Printf("Error writing response: %v", err)
	}
}

func main() {
	mux := http.NewServeMux()

	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
	}

	mux.Handle("/app/", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))
	mux.HandleFunc("/healthz", readinessEndpoint)

	fmt.Println("Starting server on ", server.Addr)
	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		fmt.Printf("Server failed: %v\n", err)
		os.Exit(1)
	}
}