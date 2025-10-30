package endpoints

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"

	"eveindustryplanner.com/esiworkers/apiServer/util"
	"eveindustryplanner.com/esiworkers/logger"
	"github.com/golang-jwt/jwt/v5"
)

type AuthPayload struct {
	Token     string `json:"token"`
	PublicKey string `json:"public_key"`
}

func Authenticate(w http.ResponseWriter, r *http.Request) {
	log := logger.GetLogger(r.Context(), logger.ApiProvider)

	var payload AuthPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	claims, err := validateEveToken(payload.Token, payload.PublicKey)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
		return
	}

	accessToken, refreshToken, err := util.GenerateTokens(claims.CharacterID, claims.CharacterName)
	if err != nil {
		http.Error(w, "Failed to generate tokens", http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func validateEveToken(tokenString, pubKeyPEM string) (*util.Claims, error) {
	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("invalid public key format")
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %v", err)
	}

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("invalid RSA public key")
	}

	claims := &util.Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return rsaPubKey, nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token: %v", err)
	}

	return claims, nil
}
