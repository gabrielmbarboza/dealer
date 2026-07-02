package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const testPollInterval = 20 * time.Millisecond
const testWaitTimeout = 2 * time.Second

type reloadRecorder struct {
	mu      sync.Mutex
	configs []*Config
	fail    bool
}

func (r *reloadRecorder) onReload(c *Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("simulated reload failure")
	}
	r.configs = append(r.configs, c)
	return nil
}

func (r *reloadRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.configs)
}

func (r *reloadRecorder) last() *Config {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.configs) == 0 {
		return nil
	}
	return r.configs[len(r.configs)-1]
}

func (r *reloadRecorder) setFail(fail bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail = fail
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

const yamlV1 = `
services:
  - name: "a"
    path: "/a"
    origin_url: "http://0.0.0.0:3001"
`

const yamlV2 = `
services:
  - name: "b"
    path: "/b"
    origin_url: "http://0.0.0.0:3002"
`

func TestWatcher_NoReloadBeforeAnyChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yamlV1), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	rec := &reloadRecorder{}
	w := NewWatcher(path, testPollInterval, rec.onReload)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(ctx)

	time.Sleep(testPollInterval * 3)
	if got := rec.count(); got != 0 {
		t.Fatalf("reload count = %d, want 0 before any file change", got)
	}
}

func TestWatcher_ReloadsOnFileChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yamlV1), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	rec := &reloadRecorder{}
	w := NewWatcher(path, testPollInterval, rec.onReload)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(ctx)

	time.Sleep(testPollInterval * 2)
	if err := os.WriteFile(path, []byte(yamlV2), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	waitUntil(t, func() bool {
		last := rec.last()
		return last != nil && len(last.Services) == 1 && last.Services[0].Name == "b"
	})
}

func TestWatcher_FailedReloadDoesNotWedgeFutureReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yamlV1), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	rec := &reloadRecorder{}
	rec.setFail(true)
	w := NewWatcher(path, testPollInterval, rec.onReload)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(ctx)

	time.Sleep(testPollInterval * 2)
	if err := os.WriteFile(path, []byte(yamlV2), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	time.Sleep(testPollInterval * 5)
	if got := rec.count(); got != 0 {
		t.Fatalf("reload count = %d, want 0 while onReload fails", got)
	}

	rec.setFail(false)
	if err := os.WriteFile(path, []byte(yamlV2+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	waitUntil(t, func() bool {
		return rec.count() > 0
	})
}

func TestWatcher_CloseStopsPolling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yamlV1), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	rec := &reloadRecorder{}
	w := NewWatcher(path, testPollInterval, rec.onReload)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Start(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(testWaitTimeout):
		t.Fatal("Start() did not return after context cancellation")
	}
}
