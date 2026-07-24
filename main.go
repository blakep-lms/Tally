// Command tally is an automatic, local-first time tracker for multi-project
// builders and their agents. See `tally --help`.
package main

import "github.com/blakep-lms/tally/cmd"

// buildVersion is overridden at release time via -ldflags "-X main.buildVersion=...".
var buildVersion = "dev"

func main() {
	cmd.SetVersion(buildVersion)
	cmd.Execute()
}
