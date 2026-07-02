package config

import (
	"context"
	"log"
	"os"
	"time"
)

// Watcher polls a config file on disk for changes and invokes onReload with
// the freshly parsed Config whenever its modification time or size changes.
// If onReload returns an error, the failure is logged and polling continues
// unaffected — a bad edit never stops the watcher from picking up a
// subsequent, valid one.
type Watcher struct {
	path     string
	interval time.Duration
	onReload func(*Config) error

	lastModTime time.Time
	lastSize    int64
}

// NewWatcher creates a Watcher for path, polling every interval. The file's
// current modification time/size are captured immediately so the first poll
// does not treat the caller's already-loaded initial config as a change.
func NewWatcher(path string, interval time.Duration, onReload func(*Config) error) *Watcher {
	w := &Watcher{
		path:     path,
		interval: interval,
		onReload: onReload,
	}
	if info, err := os.Stat(path); err == nil {
		w.lastModTime = info.ModTime()
		w.lastSize = info.Size()
	}
	return w
}

// Start polls until ctx is canceled. It is intended to be run in its own
// goroutine and returns once ctx.Done() fires.
func (w *Watcher) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.pollOnce()
		}
	}
}

func (w *Watcher) pollOnce() {
	info, err := os.Stat(w.path)
	if err != nil {
		log.Printf("config watcher: stat %s: %v", w.path, err)
		return
	}

	if info.ModTime().Equal(w.lastModTime) && info.Size() == w.lastSize {
		return
	}

	cfg, err := Load(w.path)
	if err != nil {
		log.Printf("config watcher: reload %s: %v", w.path, err)
		return
	}

	if err := w.onReload(cfg); err != nil {
		log.Printf("config watcher: apply reload of %s: %v", w.path, err)
		return
	}

	w.lastModTime = info.ModTime()
	w.lastSize = info.Size()
}
