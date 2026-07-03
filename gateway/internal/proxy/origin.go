package proxy

import (
	"sync"
	"time"
)

// maxBreakerCooldownShift caps the circuit breaker's exponential cooldown
// escalation at breakerCooldown << maxBreakerCooldownShift, so a long run
// of repeated half-open failures can't overflow the resulting duration.
const maxBreakerCooldownShift = 10

// originState tracks one origin's health, combining reactive failure
// tracking (set by the reverse proxy's ErrorHandler/ModifyResponse) with
// the result of active probing (set by a Prober). Any of these can mark an
// origin unavailable.
type originState struct {
	mu                  sync.Mutex
	lastFailureAt       time.Time
	forcedDown          bool
	consecutiveFailures int
}

// recordFailure marks a failed request at now, starting a cooldown window
// during which available reports false, and counting toward the circuit
// breaker's threshold.
func (s *originState) recordFailure(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFailureAt = now
	s.consecutiveFailures++
}

// recordSuccess closes the circuit breaker and clears any passive cooldown
// immediately: a request that reached the origin and got a response
// (regardless of its HTTP status code) proves the origin is reachable
// right now, so there's nothing left to wait out.
func (s *originState) recordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveFailures = 0
	s.lastFailureAt = time.Time{}
}

// setForcedDown marks the origin as up/down based on active probing,
// independent of the request-driven cooldown/breaker.
func (s *originState) setForcedDown(down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forcedDown = down
}

// available reports whether the origin should be attempted. Once
// consecutiveFailures reaches breakerThreshold, the plain cooldown is
// superseded by the circuit breaker's own (typically longer) cooldown,
// which escalates on each further failure - so a half-open trial request
// is naturally let through by the same check once that window elapses.
func (s *originState) available(now time.Time, cooldown time.Duration, breakerThreshold int, breakerCooldown time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.forcedDown {
		return false
	}
	if s.lastFailureAt.IsZero() {
		return true
	}

	effective := cooldown
	if breakerThreshold > 0 && s.consecutiveFailures >= breakerThreshold {
		effective = escalatedCooldown(breakerCooldown, s.consecutiveFailures-breakerThreshold)
	}
	return now.Sub(s.lastFailureAt) > effective
}

// breakerOpen reports whether the circuit breaker is currently tripped for
// this origin - used only to decide whether a last-resort fallback attempt
// (when every origin is otherwise unavailable) should still dial out, or
// fast-fail without ever attempting the origin. false also covers the
// half-open case (threshold reached, but the escalated cooldown elapsed),
// deliberately matching available's own transition back to true.
func (s *originState) breakerOpen(now time.Time, breakerThreshold int, breakerCooldown time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if breakerThreshold <= 0 || s.consecutiveFailures < breakerThreshold {
		return false
	}
	effective := escalatedCooldown(breakerCooldown, s.consecutiveFailures-breakerThreshold)
	return now.Sub(s.lastFailureAt) <= effective
}

func escalatedCooldown(base time.Duration, shift int) time.Duration {
	if shift > maxBreakerCooldownShift {
		shift = maxBreakerCooldownShift
	}
	return base << shift
}
