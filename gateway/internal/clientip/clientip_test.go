package clientip

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParse_AcceptsCIDRsAndBareIPs(t *testing.T) {
	r, err := Parse(" 10.0.0.0/8, 192.168.1.10 ,2001:db8::/32")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(r.trusted) != 3 {
		t.Fatalf("trusted prefixes = %d, want 3", len(r.trusted))
	}
}

func TestParse_RejectsInvalidEntries(t *testing.T) {
	if _, err := Parse("10.0.0.0/8,not-an-ip"); err == nil {
		t.Fatal("Parse() error = nil, want an error for an invalid entry")
	}
}

func TestParse_EmptyTrustsNothing(t *testing.T) {
	r, err := Parse("  ")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(r.trusted) != 0 {
		t.Fatalf("trusted prefixes = %d, want 0", len(r.trusted))
	}
}

func TestResolve(t *testing.T) {
	trusted, _ := Parse("10.0.0.0/8,192.0.2.1")

	tests := []struct {
		name     string
		resolver *Resolver
		remote   string
		xff      []string
		want     string
	}{
		{"no trusted proxies: the direct peer, header ignored", nil, "203.0.113.7:5000", []string{"198.51.100.1"}, "203.0.113.7"},
		{"untrusted peer: the header is not believed", trusted, "203.0.113.7:5000", []string{"198.51.100.1"}, "203.0.113.7"},
		{"trusted peer: the rightmost untrusted entry", trusted, "10.1.2.3:5000", []string{"198.51.100.1"}, "198.51.100.1"},
		{"skips every trusted hop from the right", trusted, "10.1.2.3:5000", []string{"198.51.100.9, 198.51.100.1, 192.0.2.1"}, "198.51.100.1"},
		{"joins repeated headers in order", trusted, "10.1.2.3:5000", []string{"198.51.100.9", "198.51.100.1, 10.9.9.9"}, "198.51.100.1"},
		{"a forged leftmost entry does not win", trusted, "10.1.2.3:5000", []string{"1.1.1.1, 198.51.100.1"}, "198.51.100.1"},
		{"all hops trusted: the leftmost one", trusted, "10.1.2.3:5000", []string{"10.0.0.5, 10.0.0.6"}, "10.0.0.5"},
		{"garbage stops the walk at the last good hop", trusted, "10.1.2.3:5000", []string{"198.51.100.1, garbage"}, "10.1.2.3"},
		{"trusted peer without the header", trusted, "10.1.2.3:5000", nil, "10.1.2.3"},
		{"IPv6 peer", trusted, "[2001:db8::1]:5000", nil, "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			for _, value := range tt.xff {
				req.Header.Add("X-Forwarded-For", value)
			}
			if got := tt.resolver.Resolve(req); got != tt.want {
				t.Fatalf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMiddleware_StoresTheClientIPForLaterHandlers(t *testing.T) {
	trusted, _ := Parse("10.0.0.0/8")
	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = FromRequest(r) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.2.3:5000"
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	Middleware(trusted)(next).ServeHTTP(httptest.NewRecorder(), req)

	if seen != "198.51.100.1" {
		t.Fatalf("FromRequest() = %q, want %q", seen, "198.51.100.1")
	}
}

func TestFromRequest_FallsBackToTheDirectPeerWithoutTheMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:5000"
	if got := FromRequest(req); got != "203.0.113.7" {
		t.Fatalf("FromRequest() = %q, want %q", got, "203.0.113.7")
	}
}
