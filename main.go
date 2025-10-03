package main

import (
	"os"
	"fmt"
	"net/http"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request){
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) getFileserverHits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	message := fmt.Sprintf("Hits: %v", cfg.fileserverHits.Load())
	_, err := w.Write([]byte(message))
	if err != nil {
		fmt.Printf("Error writing response: %v", err)
	}
}

func (cfg *apiConfig) resetFileserverHits(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
}

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
	apiCfg := &apiConfig{}

	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
	}

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("/healthz", readinessEndpoint)
	mux.HandleFunc("/metrics", apiCfg.getFileserverHits)
	mux.HandleFunc("/reset", apiCfg.resetFileserverHits)

	fmt.Println("Starting server on ", server.Addr)
	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		fmt.Printf("Server failed: %v\n", err)
		os.Exit(1)
	}
}