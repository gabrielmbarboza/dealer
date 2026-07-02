package plugin

import (
	"fmt"
	"net/http"
)

type addHeader struct {
	headers map[string]string
}

func newAddHeader(cfg map[string]any) (Plugin, error) {
	raw, ok := cfg["headers"]
	if !ok || raw == nil {
		return &addHeader{}, nil
	}

	rawMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("add_header: config.headers must be a map, got %T", raw)
	}

	headers := make(map[string]string, len(rawMap))
	for k, v := range rawMap {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("add_header: config.headers[%q] must be a string, got %T", k, v)
		}
		headers[k] = s
	}

	return &addHeader{headers: headers}, nil
}

func (p *addHeader) Name() string { return "add_header" }

func (p *addHeader) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range p.headers {
			r.Header.Set(k, v)
		}
		next.ServeHTTP(w, r)
	})
}
