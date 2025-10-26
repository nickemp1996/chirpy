package auth

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreateAndValidate(t *testing.T) {
	tokenSecret := "super-secret-test-key-please-change-me"
	id := uuid.New()

	s, err := MakeJWT(id, tokenSecret, time.Hour)
	if err != nil {
		t.Errorf("Error creating JWT: %v", err)
	}

	validatedID, err := ValidateJWT(s, tokenSecret)
	if err != nil {
		t.Errorf("Error validating JWT: %v", err)
	}

	if id != validatedID {
		t.Errorf("User ID from JWT does not match! %v != %v", id, validatedID)
	}
}

func TestExpiredToken(t *testing.T) {
	tokenSecret := "super-secret-test-key-please-change-me"
	id := uuid.New()

	s, err := MakeJWT(id, tokenSecret, time.Nanosecond)
	if err != nil {
		t.Errorf("Error creating JWT: %v", err)
	}

	validatedID, err := ValidateJWT(s, tokenSecret)
	if err != nil {
		if !strings.Contains(err.Error(), "token is expired") {
			t.Errorf("Expired token check did not work: %v", err)
		}
	}

	if id == validatedID {
		t.Errorf("User ID from JWT should not match! %v == %v", id, validatedID)
	}
}

func TestWrongSecret(t *testing.T) {
	wrongTokenSecret := "super-wrong-secret-test-key-please-change-me"
	tokenSecret := "super-secret-test-key-please-change-me"
	id := uuid.New()

	s, err := MakeJWT(id, wrongTokenSecret, time.Hour)
	if err != nil {
		t.Errorf("Error creating JWT: %v", err)
	}

	validatedID, err := ValidateJWT(s, tokenSecret)
	if err != nil {
		if !strings.Contains(err.Error(), "signature is invalid") {
			t.Errorf("Invalid signature check did not work: %v", err)
		}
	}

	if id == validatedID {
		t.Errorf("User ID from JWT should not match! %v == %v", id, validatedID)
	}
}

func TestGetBearerToken(t *testing.T) {
	tokenSecret := "super-secret-test-key-please-change-me"
	id := uuid.New()

	s, err := MakeJWT(id, tokenSecret, time.Hour)
	if err != nil {
		t.Errorf("Error creating JWT: %v", err)
	}

	bearerToken := "Bearer " + s

	url := "https://api.example.com/data"
	req, err := http.NewRequest("GET", url, nil) // Or "POST", "PUT", etc., with a body if needed
	if err != nil {
		// Handle error
		t.Errorf("Error creating request: %v", err)
	}

	req.Header.Set("Authorization", bearerToken)

	tokenString, err := GetBearerToken(req.Header)
	if err != nil {
		// Handle error
		t.Errorf("Error getting bearer token: %v", err)
	}

	if tokenString != s {
		t.Errorf("token string from JWT does not equal token string from http request! %v == %v", s, tokenString)
	}
}

func TestMakeRefreshToken(t *testing.T) {
	token, _ := MakeRefreshToken()
	if len(token) != 64 {
		t.Errorf("Error making refresh token")
	}
}
