// Package router compiles a gateway config.Config into a ready-to-serve
// *http.ServeMux. A fresh mux is built on every load/reload and swapped in
// atomically by the caller — it is never mutated in place.
package router

import (
	"fmt"
	"net/http"

	"github.com/gabrielmbarboza/dealer/gateway/internal/config"
)

// Build compiles cfg into a *http.ServeMux, using handlerFor to obtain the
// http.Handler that should serve each configured service (plugins + proxy).
func Build(cfg *config.Config, handlerFor func(config.Service) (http.Handler, error)) (*http.ServeMux, error) {
	mux := http.NewServeMux()

	for _, svc := range cfg.Services {
		h, err := handlerFor(svc)
		if err != nil {
			return nil, fmt.Errorf("service %q: %w", svc.Name, err)
		}

		if len(svc.Methods) == 0 {
			mux.Handle(svc.Path, h)
			continue
		}

		hasOptions := false
		for _, method := range svc.Methods {
			mux.Handle(method+" "+svc.Path, h)
			if method == http.MethodOptions {
				hasOptions = true
			}
		}

		// A service using the cors plugin needs to see preflight OPTIONS
		// requests to answer them, even if OPTIONS isn't in its declared
		// methods - otherwise the mux itself 405s them before the plugin
		// chain ever runs.
		if !hasOptions && hasCORSPlugin(svc) {
			mux.Handle(http.MethodOptions+" "+svc.Path, h)
		}
	}

	return mux, nil
}

func hasCORSPlugin(svc config.Service) bool {
	for _, p := range svc.Plugins {
		if p.Name == "cors" {
			return true
		}
	}
	return false
}
