package auth

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestParseTokenRequiresHS256AndExpiration(t *testing.T) {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		t.Fatal("generating synthetic auth secret failed")
	}
	secret := hex.EncodeToString(secretBytes)
	service := NewService(secret, time.Hour)

	noExpiration := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{UserID: 7})
	noExpirationToken, err := noExpiration.SignedString([]byte(secret))
	if err != nil {
		t.Fatal("signing no-expiration token failed")
	}
	if _, err := service.ParseToken(noExpirationToken); err == nil {
		t.Fatal("token without expiration was accepted")
	}

	wrongMethod := jwt.NewWithClaims(jwt.SigningMethodHS384, Claims{
		UserID:           7,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	})
	wrongMethodToken, err := wrongMethod.SignedString([]byte(secret))
	if err != nil {
		t.Fatal("signing wrong-method token failed")
	}
	if _, err := service.ParseToken(wrongMethodToken); err == nil {
		t.Fatal("non-HS256 token was accepted")
	}
}
