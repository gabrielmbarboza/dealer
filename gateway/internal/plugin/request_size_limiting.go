package plugin

import (
	"fmt"
	"net/http"
)

type requestSizeLimiting struct {
	maxBytes int64
}

func newRequestSizeLimiting(cfg map[string]any) (Plugin, error) {
	raw, ok := cfg["max_bytes"]
	if !ok {
		return nil, fmt.Errorf("request_size_limiting: config.max_bytes is required")
	}

	maxBytes, err := toInt64(raw)
	if err != nil {
		return nil, fmt.Errorf("request_size_limiting: config.max_bytes: %w", err)
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("request_size_limiting: config.max_bytes must be positive, got %d", maxBytes)
	}

	return &requestSizeLimiting{maxBytes: maxBytes}, nil
}

func (p *requestSizeLimiting) Name() string { return "request_size_limiting" }

func (p *requestSizeLimiting) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > p.maxBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, p.maxBytes)
		next.ServeHTTP(w, r)
	})
}

// toInt64 converts a yaml-decoded numeric value (int, int64 or float64,
// depending on how the yaml library represented the literal) to int64.
func toInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	case float64:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("expected a number, got %T", v)
	}
}
