package proxy

import (
	"testing"
	"time"
)

func TestOriginState_AvailableByDefault(t *testing.T) {
	s := &originState{}
	if !s.available(time.Now(), time.Second, 0, 0) {
		t.Fatal("available() = false, want true for a fresh origin with no failures")
	}
}

func TestOriginState_UnavailableDuringCooldownAfterFailure(t *testing.T) {
	s := &originState{}
	now := time.Now()
	s.recordFailure(now)

	if s.available(now.Add(time.Millisecond), time.Second, 0, 0) {
		t.Fatal("available() = true, want false immediately after a failure and within the cooldown")
	}
}

func TestOriginState_AvailableAfterCooldownElapses(t *testing.T) {
	s := &originState{}
	now := time.Now()
	s.recordFailure(now)

	if !s.available(now.Add(2*time.Second), time.Second, 0, 0) {
		t.Fatal("available() = false, want true once the cooldown has elapsed")
	}
}

func TestOriginState_ForcedDownOverridesCooldown(t *testing.T) {
	s := &originState{}
	s.setForcedDown(true)

	if s.available(time.Now(), time.Second, 0, 0) {
		t.Fatal("available() = true, want false while forcedDown is set, even with no recorded failure")
	}
}

func TestOriginState_ForcedDownClearedByHealthyProbe(t *testing.T) {
	s := &originState{}
	s.setForcedDown(true)
	s.setForcedDown(false)

	if !s.available(time.Now(), time.Second, 0, 0) {
		t.Fatal("available() = false, want true once forcedDown is cleared")
	}
}

func TestOriginState_BreakerDisabledWhenThresholdZero(t *testing.T) {
	s := &originState{}
	now := time.Now()
	for i := 0; i < 10; i++ {
		s.recordFailure(now)
	}

	if s.breakerOpen(now, 0, time.Hour) {
		t.Fatal("breakerOpen() = true, want false when threshold is 0 (breaker disabled)")
	}
	if !s.available(now.Add(2*time.Second), time.Second, 0, time.Hour) {
		t.Fatal("available() = false, want true: with the breaker disabled, only the plain cooldown should apply")
	}
}

func TestOriginState_BreakerOpensAfterThresholdConsecutiveFailures(t *testing.T) {
	s := &originState{}
	now := time.Now()
	for i := 0; i < 3; i++ {
		s.recordFailure(now)
	}

	if !s.breakerOpen(now, 3, time.Minute) {
		t.Fatal("breakerOpen() = false, want true once consecutive failures reach the threshold")
	}
}

func TestOriginState_BreakerNotOpenBelowThreshold(t *testing.T) {
	s := &originState{}
	now := time.Now()
	for i := 0; i < 2; i++ {
		s.recordFailure(now)
	}

	if s.breakerOpen(now, 3, time.Minute) {
		t.Fatal("breakerOpen() = true, want false: only 2 of 3 required consecutive failures recorded")
	}
}

func TestOriginState_BreakerCooldownOutlivesPlainCooldown(t *testing.T) {
	s := &originState{}
	now := time.Now()
	for i := 0; i < 3; i++ {
		s.recordFailure(now)
	}

	// The plain cooldown (100ms) would have elapsed by now, but the
	// breaker's own (much longer) cooldown has not - the breaker must
	// still hold the origin unavailable.
	later := now.Add(500 * time.Millisecond)
	if s.available(later, 100*time.Millisecond, 3, time.Minute) {
		t.Fatal("available() = true, want false: breaker cooldown (1m) has not elapsed even though the plain cooldown (100ms) has")
	}
}

func TestOriginState_HalfOpenTrialAllowedAfterBreakerCooldownElapses(t *testing.T) {
	s := &originState{}
	now := time.Now()
	for i := 0; i < 3; i++ {
		s.recordFailure(now)
	}

	after := now.Add(2 * time.Minute)
	if s.breakerOpen(after, 3, time.Minute) {
		t.Fatal("breakerOpen() = true, want false once the breaker cooldown has elapsed (should allow a half-open trial)")
	}
	if !s.available(after, time.Second, 3, time.Minute) {
		t.Fatal("available() = false, want true: a half-open trial request should be allowed through")
	}
}

func TestOriginState_SuccessResetsBreakerImmediately(t *testing.T) {
	s := &originState{}
	now := time.Now()
	for i := 0; i < 3; i++ {
		s.recordFailure(now)
	}
	s.recordSuccess()

	if s.breakerOpen(now, 3, time.Minute) {
		t.Fatal("breakerOpen() = true, want false immediately after a success resets consecutive failures")
	}
	if !s.available(now, time.Second, 3, time.Minute) {
		t.Fatal("available() = false, want true immediately after a success closes the breaker")
	}
}

func TestOriginState_FailedHalfOpenTrialEscalatesCooldown(t *testing.T) {
	s := &originState{}
	now := time.Now()
	for i := 0; i < 3; i++ {
		s.recordFailure(now)
	}

	// The half-open trial (at now+1m) itself fails, extending the outage.
	trialAt := now.Add(time.Minute)
	s.recordFailure(trialAt)

	// One more base cooldown (1m) past the failed trial is not enough -
	// the window should have escalated (doubled) to 2m.
	if s.breakerOpen(trialAt.Add(time.Minute+time.Second), 3, time.Minute) == false {
		t.Fatal("breakerOpen() = false, want true: cooldown should have escalated past a single base window after a failed half-open trial")
	}
	// But it must still clear eventually.
	if s.breakerOpen(trialAt.Add(3*time.Minute), 3, time.Minute) {
		t.Fatal("breakerOpen() = true, want false: escalated cooldown should still eventually elapse")
	}
}
