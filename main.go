package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) getFileserverHits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	message := fmt.Sprintf(`<html>
							  <body>
							    <h1>Welcome, Chirpy Admin</h1>
							    <p>Chirpy has been visited %d times!</p>
							  </body>
							</html>`, cfg.fileserverHits.Load())
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

func respondWithError(w http.ResponseWriter, code int, msg string) error {
	type returnVals struct {
		// the key will be the name of struct field unless you give it an explicit JSON tag
		Error string `json:"error"`
	}
	respBody := returnVals{
		Error: msg,
	}
	dat, err := json.Marshal(respBody)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)

	return nil
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) error {
	dat, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)

	return nil
}

func replaceBadWords(body string) string {
	badWords := []string{"kerfuffle", "sharbert", "fornax"}
	words := strings.Split(body, " ")

	for i, word := range words {
		if slices.Contains(badWords, strings.ToLower(word)) {
			words[i] = "****"
		}
	}

	return strings.Join(words, " ")
}

func handlerValidateChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		// an error will be thrown if the JSON is invalid or has the wrong types
		// any missing fields will simply have their values in the struct set to their zero value
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}

	if len(params.Body) > 140 {
		err = respondWithError(w, 400, "chirp too long")
	} else {
		type returnVals struct {
			// the key will be the name of struct field unless you give it an explicit JSON tag
			CleanedBody string `json:"cleaned_body"`
		}
		respBody := returnVals{
			CleanedBody: replaceBadWords(params.Body),
		}

		err = respondWithJSON(w, 200, respBody)
	}
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
}

func main() {
	mux := http.NewServeMux()
	apiCfg := &apiConfig{}

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /api/healthz", readinessEndpoint)
	mux.HandleFunc("GET /admin/metrics", apiCfg.getFileserverHits)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetFileserverHits)
	mux.HandleFunc("POST /api/validate_chirp", handlerValidateChirp)

	fmt.Println("Starting server on ", server.Addr)
	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		fmt.Printf("Server failed: %v\n", err)
		os.Exit(1)
	}
}
