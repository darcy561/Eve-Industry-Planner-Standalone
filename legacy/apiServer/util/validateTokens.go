package util

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt"
)

const (
	eveJwksURL  = "https://login.eveonline.com/oauth/jwks"
	clientID    = "YOUR_CLIENT_ID" // Replace with your EVE SSO Client ID
	issuer      = "https://login.eveonline.com"
	internalKey = "my_super_secret_key"
	refreshKey  = "my_refresh_secret_key"
)

type Claims struct {
	CharacterID   string `json:"sub"`
	CharacterName string `json:"name"`
	Audience      string `json:"aud"`
	Issuer        string `json:"iss"`
	jwt.RegisteredClaims
}

func validateEveToken(tokenString string) (*Claims, error) {
	keys, err := fetchEveJwks()
	if err != nil {
		return nil, err
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, errors.New("missing kid in header")
		}
		key, exists := keys[kid]
		if !exists {
			return nil, fmt.Errorf("unknown key id: %s", kid)
		}
		return key, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	// Validate claims
	if claims.Issuer != issuer {
		return nil, errors.New("invalid issuer")
	}
	if claims.Audience != clientID {
		return nil, errors.New("invalid audience")
	}
	if time.Now().After(claims.ExpiresAt.Time) {
		return nil, errors.New("token expired")
	}

	return claims, nil
}

func fetchEveJwks() (map[string]*rsa.PublicKey, error) {
	resp, err := http.Get(eveJwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch JWKS, status: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read JWKS response: %v", err)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
			Alg string `json:"alg"`
			Kty string `json:"kty"`
			Use string `json:"use"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JWKS: %v", err)
	}

	// Convert JWKS to RSA keys
	keys := make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		nBytes, err := jwt.DecodeSegment(key.N)
		if err != nil {
			return nil, fmt.Errorf("invalid N value: %v", err)
		}
		eBytes, err := jwt.DecodeSegment(key.E)
		if err != nil {
			return nil, fmt.Errorf("invalid E value: %v", err)
		}

		e := int(eBytes[0])
		rsaKey := &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: e,
		}
		keys[key.Kid] = rsaKey
	}

	return keys, nil
}
