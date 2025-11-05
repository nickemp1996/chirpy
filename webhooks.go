package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/nickemp1996/chirpy/internal/auth"
)

func (cfg *apiConfig) upgradeUser(w http.ResponseWriter, r *http.Request) {
	polkaKey, err := auth.GetAPIKey(r.Header)
	if err != nil || polkaKey != cfg.polkaKey {
		respondWithError(w, 401, "incorrect api key", err)
		return
	}

	type parameters struct {
		Event string `json:"event"`
		Data  struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	if params.Event != "user.upgraded" {
		fmt.Println("wrong event")
		w.WriteHeader(204)
		return
	}

	id, err := uuid.Parse(params.Data.UserID)
	if err != nil {
		respondWithError(w, 500, "internal error", err)
		return
	}

	_, err = cfg.queries.UpgradeUser(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, 404, "user not found", err)
			return
		} else {
			respondWithError(w, 500, "internal error", err)
			return
		}
	}

	w.WriteHeader(204)
}
