package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nickemp1996/chirpy/internal/auth"
	"github.com/nickemp1996/chirpy/internal/database"
)

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
		ID:          dbUser.ID,
		CreatedAt:   dbUser.CreatedAt,
		UpdatedAt:   dbUser.UpdatedAt,
		Email:       dbUser.Email,
		IsChirpyRed: dbUser.IsChirpyRed,
	}

	respondWithJSON(w, 201, user)
}

func (cfg *apiConfig) updateUserLogin(w http.ResponseWriter, r *http.Request) {
	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "token missing", err)
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

	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
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

	userParams := database.UpdateUserParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
		ID:             validUser,
	}

	dbUser, err := cfg.queries.UpdateUser(r.Context(), userParams)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	user := User{
		ID:          dbUser.ID,
		CreatedAt:   dbUser.CreatedAt,
		UpdatedAt:   dbUser.UpdatedAt,
		Email:       dbUser.Email,
		IsChirpyRed: dbUser.IsChirpyRed,
	}

	respondWithJSON(w, 200, user)
}

func (cfg *apiConfig) userLogin(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
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

	tokenString, err := auth.MakeJWT(dbUser.ID, cfg.secret)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	refreshToken, _ := auth.MakeRefreshToken()

	addRefreshTokenParams := database.AddRefreshTokenParams{
		Token:     refreshToken,
		UserID:    dbUser.ID,
		ExpiresAt: time.Now().Add(time.Hour * 24 * 60),
	}

	dbRefreshToken, err := cfg.queries.AddRefreshToken(r.Context(), addRefreshTokenParams)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	fmt.Println("Token:", dbRefreshToken.Token)
	fmt.Println("Expires at:", dbRefreshToken.ExpiresAt)

	user := User{
		ID:           dbUser.ID,
		CreatedAt:    dbUser.CreatedAt,
		UpdatedAt:    dbUser.UpdatedAt,
		Email:        dbUser.Email,
		Token:        tokenString,
		RefreshToken: dbRefreshToken.Token,
		IsChirpyRed:  dbUser.IsChirpyRed,
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
		respondWithError(w, 401, "no token", err)
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

func (cfg *apiConfig) deleteChirp(w http.ResponseWriter, r *http.Request) {
	chirpID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, 404, "Invalid UUID format", err)
		return
	}

	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "no token", err)
		return
	}

	validUser, err := auth.ValidateJWT(tokenString, cfg.secret)
	if err != nil {
		if strings.Contains(err.Error(), "invalid token") {
			respondWithError(w, 403, "Unauthorized", err)
			return
		}
		respondWithError(w, 500, "internal error", err)
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

	if validUser == dbChirp.UserID {
		err = cfg.queries.DeleteChirp(r.Context(), chirpID)
		if err != nil {
			respondWithError(w, 500, "internal error", err)
			return
		}
		w.WriteHeader(204)
	} else {
		respondWithError(w, 403, "Unauthorized", err)
		return
	}
}

func (cfg *apiConfig) refresh(w http.ResponseWriter, r *http.Request) {
	refreshTokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "no token", err)
		return
	}

	dbRefreshToken, err := cfg.queries.GetUserFromRefreshToken(r.Context(), refreshTokenString)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, 401, "user does not exist", err)
			return
		} else {
			respondWithError(w, 500, "internal error", err)
			return
		}
	}

	if dbRefreshToken.ExpiresAt.Compare(time.Now()) <= 0 || dbRefreshToken.RevokedAt.Valid {
		respondWithError(w, 401, "user access not allowed", err)
		return
	}

	tokenString, err := auth.MakeJWT(dbRefreshToken.UserID, cfg.secret)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	type parameters struct {
		Token string `json:"token"`
	}

	response := parameters{
		Token: tokenString,
	}

	respondWithJSON(w, 200, response)
}

func (cfg *apiConfig) revoke(w http.ResponseWriter, r *http.Request) {
	refreshTokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "no token", err)
		return
	}

	err = cfg.queries.RevokeRefreshToken(r.Context(), refreshTokenString)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	w.WriteHeader(204)
}
