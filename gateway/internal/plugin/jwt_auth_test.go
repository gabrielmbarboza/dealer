package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func signToken(t *testing.T, secret string, expiresAt time.Time) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub": "user-1",
		"exp": expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return signed
}

func newTestJWTAuth(t *testing.T) Plugin {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret")
	p, err := newJWTAuth(map[string]any{"secret_env": "JWT_SECRET"})
	if err != nil {
		t.Fatalf("newJWTAuth() error = %v", err)
	}
	return p
}

func nextThatMarksCalled(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestJWTAuth_ValidTokenInAuthorizationHeader(t *testing.T) {
	p := newTestJWTAuth(t)
	token := signToken(t, "test-secret", time.Now().Add(time.Hour))

	var called bool
	req := httptest.NewRequest(http.MethodGet, "/payments", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	p.Wrap(nextThatMarksCalled(&called)).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called for a valid header token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestJWTAuth_ValidTokenInQueryString(t *testing.T) {
	p := newTestJWTAuth(t)
	token := signToken(t, "test-secret", time.Now().Add(time.Hour))

	var called bool
	req := httptest.NewRequest(http.MethodGet, "/payments?token="+token, nil)
	rec := httptest.NewRecorder()
	p.Wrap(nextThatMarksCalled(&called)).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called for a valid query token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestJWTAuth_MissingTokenBlocks(t *testing.T) {
	p := newTestJWTAuth(t)

	var called bool
	req := httptest.NewRequest(http.MethodGet, "/payments", nil)
	rec := httptest.NewRecorder()
	p.Wrap(nextThatMarksCalled(&called)).ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called, want blocked")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestJWTAuth_InvalidSignatureBlocks(t *testing.T) {
	p := newTestJWTAuth(t)
	token := signToken(t, "wrong-secret", time.Now().Add(time.Hour))

	var called bool
	req := httptest.NewRequest(http.MethodGet, "/payments", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	p.Wrap(nextThatMarksCalled(&called)).ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called, want blocked")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestJWTAuth_ExpiredTokenBlocks(t *testing.T) {
	p := newTestJWTAuth(t)
	token := signToken(t, "test-secret", time.Now().Add(-time.Hour))

	var called bool
	req := httptest.NewRequest(http.MethodGet, "/payments", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	p.Wrap(nextThatMarksCalled(&called)).ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called, want blocked")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestJWTAuth_MissingEnvVarFailsClosedAtConstruction(t *testing.T) {
	t.Setenv("JWT_SECRET_UNSET", "")
	if _, err := newJWTAuth(map[string]any{"secret_env": "JWT_SECRET_UNSET"}); err == nil {
		t.Fatal("newJWTAuth() error = nil, want non-nil when the env var is unset")
	}
}

func TestJWTAuth_MissingSecretEnvConfigErrors(t *testing.T) {
	if _, err := newJWTAuth(map[string]any{}); err == nil {
		t.Fatal("newJWTAuth() error = nil, want non-nil when config.secret_env is missing")
	}
}

func TestJWTAuth_Name(t *testing.T) {
	p := newTestJWTAuth(t)
	if p.Name() != "jwt_auth" {
		t.Fatalf("Name() = %q, want %q", p.Name(), "jwt_auth")
	}
}
