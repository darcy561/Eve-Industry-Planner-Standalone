package util

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var internalSecret = []byte("my_super_secret_key")
var refreshSecret = []byte("my_refresh_secret_key")

type AuthPayload struct {
	Token     string `json:"token"`
	PublicKey string `json:"public_key"`
}

type InternalClaims struct {
	CharacterID   string `json:"character_id"`
	CharacterName string `json:"character_name"`
	jwt.RegisteredClaims
}

func GenerateTokens(characterID, characterName string) (string, string, error) {
	// Access token
	accessExp := time.Now().Add(20 * time.Minute)
	accessClaims := jwt.MapClaims{
		"character_id":   characterID,
		"character_name": characterName,
		"exp":            accessExp.Unix(),
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString([]byte(internalSecret))
	if err != nil {
		return "", "", err
	}

	// Refresh token
	refreshExp := time.Now().Add(7 * 24 * time.Hour)
	refreshClaims := jwt.MapClaims{
		"character_id": characterID,
		"exp":          refreshExp.Unix(),
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(refreshSecret))
	if err != nil {
		return "", "", err
	}

	// refreshTokens[characterID] = refreshTokenString

	return accessTokenString, refreshTokenString, nil
}
