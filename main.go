package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/nickemp1996/chirpy/internal/auth"
	"github.com/nickemp1996/chirpy/internal/database"

	_ "github.com/lib/pq"
)

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
	Token     string    `json:"token"`
}

type Chirp struct {
	// the key will be the name of struct field unless you give it an explicit JSON tag
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

type apiConfig struct {
	fileserverHits atomic.Int32
	queries        *database.Queries
	platform       string
	secret         string
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
		log.Printf("Error writing response: %v", err)
	}
}

func (cfg *apiConfig) reset(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		respondWithError(w, 403, "Access denied", nil)
		return
	}
	cfg.fileserverHits.Store(0)
	err := cfg.queries.DeleteUsers(r.Context())
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}
	err = cfg.queries.DeleteChirps(r.Context())
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

func (cfg *apiConfig) addUser(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		// an error will be thrown if the JSON is invalid or has the wrong types
		// any missing fields will simply have their values in the struct set to their zero value
		respondWithError(w, 500, "internal error", err)
		return
	}

	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	userParams := database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
	}

	dbUser, err := cfg.queries.CreateUser(r.Context(), userParams)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	user := User{
		ID:        dbUser.ID,
		CreatedAt: dbUser.CreatedAt,
		UpdatedAt: dbUser.UpdatedAt,
		Email:     dbUser.Email,
	}

	respondWithJSON(w, 201, user)
}

func (cfg *apiConfig) userLogin(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		ExpiresIn *int   `json:"expires_inseconds,omitempty"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	dbUser, err := cfg.queries.GetPassword(r.Context(), params.Email)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, 401, "Incorrect email or password", err)
			return
		} else {
			respondWithError(w, 500, "internal error", err)
			return
		}
	}

	valid, err := auth.CheckPasswordHash(params.Password, dbUser.HashedPassword)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	if !valid {
		respondWithError(w, 401, "Incorrect email or password", nil)
		return
	}

	expiresIn := time.Duration(3600) * time.Second
	if params.ExpiresIn != nil {
		if *params.ExpiresIn <= 3600 {
			expiresIn = time.Duration(*params.ExpiresIn) * time.Second
		}
	}

	tokenString, err := auth.MakeJWT(dbUser.ID, cfg.secret, expiresIn)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	user := User{
		ID:        dbUser.ID,
		CreatedAt: dbUser.CreatedAt,
		UpdatedAt: dbUser.UpdatedAt,
		Email:     dbUser.Email,
		Token:     tokenString,
	}

	respondWithJSON(w, 200, user)
}

func (cfg *apiConfig) createChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		// an error will be thrown if the JSON is invalid or has the wrong types
		// any missing fields will simply have their values in the struct set to their zero value
		respondWithError(w, 500, "internal error", err)
		return
	}

	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	validUser, err := auth.ValidateJWT(tokenString, cfg.secret)
	if err != nil {
		if strings.Contains(err.Error(), "invalid token") {
			respondWithError(w, 401, "Unauthorized", err)
			return
		}
		respondWithError(w, 500, "internal error", err)
		return
	}

	log.Printf("user id = %v\n", validUser)

	if len(params.Body) > 140 {
		respondWithError(w, 400, "chirp too long", nil)
	} else {
		chirpParams := database.CreateChirpParams{
			Body:   replaceBadWords(params.Body),
			UserID: validUser,
		}

		dbChirp, err1 := cfg.queries.CreateChirp(r.Context(), chirpParams)
		if err1 != nil {
			respondWithError(w, 500, "internal error", err)
			return
		}

		chirp := Chirp{
			ID:        dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt,
			UpdatedAt: dbChirp.UpdatedAt,
			Body:      dbChirp.Body,
			UserID:    dbChirp.UserID,
		}

		respondWithJSON(w, 201, chirp)
	}
}

func (cfg *apiConfig) getChirps(w http.ResponseWriter, r *http.Request) {
	dbChirps, err := cfg.queries.GetChirps(r.Context())
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, 404, "no chirps found", err)
			return
		} else {
			respondWithError(w, 500, "internal error", err)
			return
		}
	}

	chirps := make([]Chirp, len(dbChirps))
	for i, dbChirp := range dbChirps {
		chirps[i].ID = dbChirp.ID
		chirps[i].CreatedAt = dbChirp.CreatedAt
		chirps[i].UpdatedAt = dbChirp.UpdatedAt
		chirps[i].Body = dbChirp.Body
		chirps[i].UserID = dbChirp.UserID
	}

	respondWithJSON(w, 200, chirps)
}

func (cfg *apiConfig) getChirp(w http.ResponseWriter, r *http.Request) {
	chirpID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, 404, "Invalid UUID format", err)
		return
	}

	dbChirp, err := cfg.queries.GetChirp(r.Context(), chirpID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, 404, "chirp not found", err)
			return
		} else {
			respondWithError(w, 500, "internal error", err)
			return
		}
	}

	chirp := Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		Body:      dbChirp.Body,
		UserID:    dbChirp.UserID,
	}

	respondWithJSON(w, 200, chirp)
}

func readinessEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	message := "OK"
	_, err := w.Write([]byte(message))
	if err != nil {
		log.Printf("Error writing response: %v", err)
	}
}

func respondWithError(w http.ResponseWriter, code int, msg string, err error) {
	log.Printf("%s: %s", msg, err)

	type returnVals struct {
		// the key will be the name of struct field unless you give it an explicit JSON tag
		Error string `json:"error"`
	}
	respBody := returnVals{
		Error: msg,
	}
	dat, err := json.Marshal(respBody)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	dat, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
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

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")
	secret := os.Getenv("SECRET")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Printf("Failed to open connection to database: %v\n", err)
		os.Exit(1)
	}
	dbQueries := database.New(db)

	mux := http.NewServeMux()
	apiCfg := &apiConfig{}
	apiCfg.queries = dbQueries
	apiCfg.platform = platform
	apiCfg.secret = secret

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /api/healthz", readinessEndpoint)
	mux.HandleFunc("GET /admin/metrics", apiCfg.getFileserverHits)
	mux.HandleFunc("POST /admin/reset", apiCfg.reset)
	mux.HandleFunc("POST /api/chirps", apiCfg.createChirp)
	mux.HandleFunc("GET /api/chirps", apiCfg.getChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.getChirp)
	mux.HandleFunc("POST /api/users", apiCfg.addUser)
	mux.HandleFunc("POST /api/login", apiCfg.userLogin)

	fmt.Println("Starting server on ", server.Addr)
	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		fmt.Printf("Server failed: %v\n", err)
		os.Exit(1)
	}
}
