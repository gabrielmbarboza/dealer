// Command jwtgen mints a single HS256 JWT signed with JWT_SECRET, matching
// what the gateway's jwt_auth plugin expects. It exists so load test scripts
// can obtain a valid Authorization header without depending on a real auth
// service.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	sub := flag.String("sub", "loadtest", "JWT subject claim")
	ttl := flag.Duration("ttl", time.Hour, "token time-to-live")
	flag.Parse()

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("jwtgen: JWT_SECRET must be set")
	}

	token, err := mint(secret, *sub, *ttl)
	if err != nil {
		log.Fatalf("jwtgen: %v", err)
	}
	fmt.Println(token)
}

func mint(secret, sub string, ttl time.Duration) (string, error) {
	header, err := b64json(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims, err := b64json(map[string]any{
		"sub": sub,
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
	})
	if err != nil {
		return "", err
	}

	signingInput := header + "." + claims
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig, nil
}

func b64json(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
