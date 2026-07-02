package config

import "sync/atomic"

// Store holds the latest known-good Config behind an atomic pointer, so
// readers never observe a partially-written value while a reload is in
// progress.
type Store struct {
	ptr atomic.Pointer[Config]
}

// NewStore creates a Store initialized with the given Config.
func NewStore(initial *Config) *Store {
	s := &Store{}
	s.ptr.Store(initial)
	return s
}

// Get returns the current Config.
func (s *Store) Get() *Config {
	return s.ptr.Load()
}

// Set atomically replaces the current Config.
func (s *Store) Set(c *Config) {
	s.ptr.Store(c)
}
