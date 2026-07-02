package plugin

import (
	"io"
	"log"
	"net/http"
	"os"
)

type httpLog struct {
	logger *log.Logger
}

func newHTTPLog(cfg map[string]any) (Plugin, error) {
	return newHTTPLogWithWriter(os.Stdout), nil
}

func newHTTPLogWithWriter(w io.Writer) *httpLog {
	return &httpLog{logger: log.New(w, "", log.LstdFlags)}
}

func (p *httpLog) Name() string { return "http_log" }

func (p *httpLog) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.logger.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
