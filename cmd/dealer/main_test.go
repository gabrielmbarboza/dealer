package main

import "testing"

func TestNewDebugServer_DisabledWhenAddrEmpty(t *testing.T) {
	if s := newDebugServer(""); s != nil {
		t.Fatalf("newDebugServer(\"\") = %v, want nil (profiling must be opt-in)", s)
	}
}

func TestNewDebugServer_EnabledWithAddr(t *testing.T) {
	const addr = "127.0.0.1:6060"

	s := newDebugServer(addr)
	if s == nil {
		t.Fatal("newDebugServer() = nil, want a non-nil *http.Server")
	}
	if s.Addr != addr {
		t.Fatalf("Addr = %q, want %q", s.Addr, addr)
	}
	if s.Handler != nil {
		t.Fatalf("Handler = %v, want nil so it falls back to http.DefaultServeMux (where net/http/pprof registers itself)", s.Handler)
	}
}
