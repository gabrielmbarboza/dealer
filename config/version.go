package config

// Version is overridden at build time via
// -ldflags "-X github.com/gabrielmbarboza/dealer/config.Version=<version>"
// (see Dockerfile). Defaults to "dev" for local, non-release builds.
var Version = "dev"
