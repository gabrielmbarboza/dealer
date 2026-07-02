package plugin

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type jwtAuth struct {
	secret []byte
}

func newJWTAuth(cfg map[string]any) (Plugin, error) {
	envName, ok := cfg["secret_env"].(string)
	if !ok || envName == "" {
		return nil, fmt.Errorf("jwt_auth: config.secret_env is required")
	}

	secret := os.Getenv(envName)
	if secret == "" {
		return nil, fmt.Errorf("jwt_auth: environment variable %q is not set", envName)
	}

	return &jwtAuth{secret: []byte(secret)}, nil
}

func (p *jwtAuth) Name() string { return "jwt_auth" }

func (p *jwtAuth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString := extractToken(r)
		if tokenString == "" {
			http.Error(w, "missing jwt", http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return p.secret, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "invalid jwt", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if rest, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return rest
		}
	}
	return r.URL.Query().Get("token")
}
