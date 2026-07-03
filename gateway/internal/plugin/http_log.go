package plugin

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gabrielmbarboza/dealer/gateway/internal/tracing"
)

// httpLog writes "<timestamp> METHOD path" lines for every request.
//
// Writes go through a bufio.Writer guarded by a mutex instead of straight
// to the underlying writer. A per-request log.Logger.Printf (one syscall
// per request, e.g. to stdout) was measured under load to be the top
// mutex-contention source and ~26% of cumulative CPU in this plugin's
// Wrap handler. Buffering batches many lines into one syscall, shrinking
// both the lock's critical section and the syscall count by roughly the
// buffer-to-line-size ratio. This intentionally does not spawn a
// background goroutine: the Plugin interface has no lifecycle/Close hook,
// and a goroutine tied to plugin-instantiation would leak one per config
// hot-reload.
//
// Trade-off: lines sit in the buffer until it fills (or flush is called
// explicitly), so a handful of the most recent lines can be lost on an
// ungraceful process termination (e.g. SIGKILL) - acceptable for access
// logging, not for an audit trail.
type httpLog struct {
	mu     sync.Mutex
	writer *bufio.Writer
}

func newHTTPLog(cfg map[string]any) (Plugin, error) {
	return newHTTPLogWithWriter(os.Stdout), nil
}

func newHTTPLogWithWriter(w io.Writer) *httpLog {
	return &httpLog{writer: bufio.NewWriter(w)}
}

func (p *httpLog) Name() string { return "http_log" }

func (p *httpLog) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.log(r.Method, r.URL.Path, tracing.FromContext(r.Context()))
		next.ServeHTTP(w, r)
	})
}

func (p *httpLog) log(method, path, requestID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// bufio.Writer only errors here if a prior implicit flush (buffer
	// full) failed writing to the underlying writer; nothing this plugin
	// can usefully react to per line, so errors are explicitly ignored
	// rather than left unchecked.
	_, _ = p.writer.WriteString(time.Now().Format("2006/01/02 15:04:05"))
	_ = p.writer.WriteByte(' ')
	_, _ = p.writer.WriteString(method)
	_ = p.writer.WriteByte(' ')
	_, _ = p.writer.WriteString(path)
	if requestID != "" {
		_, _ = p.writer.WriteString(" request_id=")
		_, _ = p.writer.WriteString(requestID)
	}
	_ = p.writer.WriteByte('\n')
}

// flush forces any buffered log lines out to the underlying writer.
func (p *httpLog) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.writer.Flush()
}
