package main

import (
	"fmt"
	"runtime/debug"
)

// Semantic versioning components.
// Bump these when making a new release.
const (
	majorVer = 0
	minorVer = 1
	patchVer = 0
)

// Version of srchdsh.
var Version = "unknown"

func init() {
	// All of this could technically be done at build time but it would
	// require extra steps and therefore more than just the Go toolchain to
	// build.
	//
	// Also, at least on my machine, `go run` won't fill in the VCS
	// information but `go build` will, so if you're here to investigate
	// why you're seeing vX.Y.Z-unknown, I am sorry that I ruined the
	// surprise.

	info, ok := debug.ReadBuildInfo()
	if !ok {
		Version = fmt.Sprintf("v%d.%d.%d-unknown", majorVer, minorVer, patchVer)
		return
	}

	// XXX: Unfortunatly, this does not do what you may expect!
	// From what I understand, unless you check out a tag this will
	// *always* read "(devel)".
	// Version = info.Main.Version

	// Find the commit that this build is on.
	rev := "unknown"
	dirty := false
	for _, kv := range info.Settings {
		if kv.Key == "vcs.revision" {
			// Just use the short commit ID
			rev = kv.Value[:7]
		} else if kv.Key == "vcs.modified" {
			dirty = kv.Value == "true"
		}
	}

	// Set version.
	dirtyStr := ""
	if dirty {
		dirtyStr = "-dirty"
	}
	Version = fmt.Sprintf("v%d.%d.%d-%s%s", majorVer, minorVer, patchVer, rev, dirtyStr)
}
