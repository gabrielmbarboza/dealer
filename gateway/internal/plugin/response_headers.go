package plugin

import (
	"fmt"
	"net/http"
	"strings"
)

// responseHeaders sets headers on the response sent back to the client,
// replacing any value the origin sent for the same header. add_header only
// touches the request forwarded to the origin; this is its counterpart for
// edge policies such as Strict-Transport-Security or Content-Security-Policy.
//
// Headers are applied right before the status line is written, so they also
// reach responses the gateway produces itself (e.g. a 502 from the proxy).
// A rule adds headers only when the response Content-Type starts with its
// content_type, e.g. Cache-Control: no-cache for text/html only, so an SPA
// shell is always revalidated while its hashed assets stay cacheable.
type responseHeaders struct {
	headers map[string]string
	rules   []responseHeaderRule
}

type responseHeaderRule struct {
	contentType string
	headers     map[string]string
}

func newResponseHeaders(cfg map[string]any) (Plugin, error) {
	headers, err := stringMap(cfg["headers"], "response_headers: config.headers")
	if err != nil {
		return nil, err
	}

	p := &responseHeaders{headers: headers}

	raw, ok := cfg["rules"]
	if !ok || raw == nil {
		return p, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("response_headers: config.rules must be a list, got %T", raw)
	}
	for i, item := range list {
		rule, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("response_headers: config.rules[%d] must be a map, got %T", i, item)
		}
		contentType, _ := rule["content_type"].(string)
		if contentType == "" {
			return nil, fmt.Errorf("response_headers: config.rules[%d].content_type is required", i)
		}
		ruleHeaders, err := stringMap(rule["headers"], fmt.Sprintf("response_headers: config.rules[%d].headers", i))
		if err != nil {
			return nil, err
		}
		if len(ruleHeaders) == 0 {
			return nil, fmt.Errorf("response_headers: config.rules[%d].headers is required", i)
		}
		p.rules = append(p.rules, responseHeaderRule{contentType: strings.ToLower(contentType), headers: ruleHeaders})
	}

	return p, nil
}

// stringMap converts a yaml-decoded map of header names to values. A nil
// value is an empty map; anything other than a map of strings is an error.
func stringMap(raw any, field string) (map[string]string, error) {
	if raw == nil {
		return map[string]string{}, nil
	}
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a map, got %T", field, raw)
	}
	out := make(map[string]string, len(rawMap))
	for k, v := range rawMap {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%q] must be a string, got %T", field, k, v)
		}
		out[k] = s
	}
	return out, nil
}

func (p *responseHeaders) Name() string { return "response_headers" }

func (p *responseHeaders) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&responseHeadersWriter{ResponseWriter: w, plugin: p}, r)
	})
}

func (p *responseHeaders) apply(h http.Header) {
	for k, v := range p.headers {
		h.Set(k, v)
	}
	if len(p.rules) == 0 {
		return
	}
	contentType := strings.ToLower(h.Get("Content-Type"))
	for _, rule := range p.rules {
		if strings.HasPrefix(contentType, rule.contentType) {
			for k, v := range rule.headers {
				h.Set(k, v)
			}
		}
	}
}

type responseHeadersWriter struct {
	http.ResponseWriter
	plugin  *responseHeaders
	applied bool
}

func (w *responseHeadersWriter) applyOnce() {
	if w.applied {
		return
	}
	w.applied = true
	w.plugin.apply(w.ResponseWriter.Header())
}

func (w *responseHeadersWriter) WriteHeader(code int) {
	// 1xx responses are informational; the final status still follows.
	if code >= http.StatusOK {
		w.applyOnce()
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseHeadersWriter) Write(b []byte) (int, error) {
	w.applyOnce()
	return w.ResponseWriter.Write(b)
}

// Flush applies the headers before flushing, since a flush without a prior
// WriteHeader sends the status line implicitly.
func (w *responseHeadersWriter) Flush() {
	w.applyOnce()
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap exposes the underlying writer to http.ResponseController, so
// hijacking for protocol upgrades keeps working through this plugin.
func (w *responseHeadersWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
