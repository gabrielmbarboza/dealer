package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-secret-0123456789-0123456789-abcd"

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
	t.Setenv("JWT_SECRET", testJWTSecret)
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
	token := signToken(t, testJWTSecret, time.Now().Add(time.Hour))

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

func newTestJWTAuthWithQueryToken(t *testing.T) Plugin {
	t.Helper()
	t.Setenv("JWT_SECRET", testJWTSecret)
	p, err := newJWTAuth(map[string]any{"secret_env": "JWT_SECRET", "allow_query_token": true})
	if err != nil {
		t.Fatalf("newJWTAuth() error = %v", err)
	}
	return p
}

func TestJWTAuth_QueryStringTokenRejectedByDefault(t *testing.T) {
	p := newTestJWTAuth(t)
	token := signToken(t, testJWTSecret, time.Now().Add(time.Hour))

	var called bool
	req := httptest.NewRequest(http.MethodGet, "/payments?token="+token, nil)
	rec := httptest.NewRecorder()
	p.Wrap(nextThatMarksCalled(&called)).ServeHTTP(rec, req)

	if called || rec.Code != http.StatusUnauthorized {
		t.Fatalf("called = %v, status = %d, want blocked with %d", called, rec.Code, http.StatusUnauthorized)
	}
}

func TestJWTAuth_AllowQueryTokenMustBeABool(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	if _, err := newJWTAuth(map[string]any{"secret_env": "JWT_SECRET", "allow_query_token": "yes"}); err == nil {
		t.Fatal("newJWTAuth() error = nil, want non-nil for a non-boolean allow_query_token")
	}
}

func TestJWTAuth_TokenWithoutExpiryRejected(t *testing.T) {
	p := newTestJWTAuth(t)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "user-1"}).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	var called bool
	req := httptest.NewRequest(http.MethodGet, "/payments", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()
	p.Wrap(nextThatMarksCalled(&called)).ServeHTTP(rec, req)

	if called || rec.Code != http.StatusUnauthorized {
		t.Fatalf("called = %v, status = %d, want blocked with %d", called, rec.Code, http.StatusUnauthorized)
	}
}

func TestJWTAuth_ValidTokenInQueryString(t *testing.T) {
	p := newTestJWTAuthWithQueryToken(t)
	token := signToken(t, testJWTSecret, time.Now().Add(time.Hour))

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
	token := signToken(t, testJWTSecret+"-wrong", time.Now().Add(time.Hour))

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
	token := signToken(t, testJWTSecret, time.Now().Add(-time.Hour))

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

func TestJWTAuth_ShortSecretFailsClosedAtConstruction(t *testing.T) {
	t.Setenv("JWT_SECRET_SHORT", "changeme")
	if _, err := newJWTAuth(map[string]any{"secret_env": "JWT_SECRET_SHORT"}); err == nil {
		t.Fatal("newJWTAuth() error = nil, want non-nil for a secret shorter than 32 bytes")
	}
}
