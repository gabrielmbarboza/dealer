package proxy

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type weightedOrigin struct {
	handler     http.Handler
	lastFailure atomic.Int64
}

type roundRobinProxy struct {
	origins  []*weightedOrigin
	cooldown time.Duration
	now      func() time.Time
	next     atomic.Uint64
}

func NewOriginProxy(name string, originURLs []string, timeout, cooldown time.Duration) (http.Handler, error) {
	if len(originURLs) == 0 {
		return nil, fmt.Errorf("proxy: service %q: at least one origin_url is required", name)
	}
	if len(originURLs) == 1 {
		return NewReverseProxy(name, originURLs[0], timeout)
	}

	lb := &roundRobinProxy{
		origins:  make([]*weightedOrigin, 0, len(originURLs)),
		cooldown: cooldown,
		now:      time.Now,
	}
	for _, u := range originURLs {
		rp, err := NewReverseProxy(name, u, timeout)
		if err != nil {
			return nil, err
		}
		o := &weightedOrigin{}
		originalErrorHandler := rp.ErrorHandler
		rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			o.lastFailure.Store(lb.now().UnixNano())
			originalErrorHandler(w, r, err)
		}
		o.handler = rp
		lb.origins = append(lb.origins, o)
	}

	return lb, nil
}

func (lb *roundRobinProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := uint64(len(lb.origins))
	start := lb.next.Add(1)
	now := lb.now()

	for i := uint64(0); i < n; i++ {
		o := lb.origins[(start+i)%n]
		lastFailure := o.lastFailure.Load()
		if lastFailure == 0 || now.Sub(time.Unix(0, lastFailure)) > lb.cooldown {
			o.handler.ServeHTTP(w, r)
			return
		}
	}

	lb.origins[start%n].handler.ServeHTTP(w, r)
}
