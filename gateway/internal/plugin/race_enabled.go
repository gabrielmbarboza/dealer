//go:build race

package plugin

// raceDetectorEnabled is true when built with -race. Some tests need to
// know this at runtime (see ratelimit_sugardb_test.go) since the standard
// library's race detector has no other public way to query it.
const raceDetectorEnabled = true
