package main

import "github.com/emcniece/s3-summary/cmd"

// version, commit, and date are populated at build time by goreleaser via
// -ldflags. They keep their dev defaults when built with `go build` directly.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	cmd.Execute()
}
