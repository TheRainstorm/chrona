// Package buildinfo provides metadata shared by the CLI, startup log and health API.
package buildinfo

// Version and Commit are set by the release build's linker flags.
var Version = "dev"
var Commit = ""

func String() string {
	if Commit == "" {
		return "Chrona " + Version
	}
	return "Chrona " + Version + " (commit " + Commit + ")"
}
