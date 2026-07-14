// Package version pins the single source of truth for the okpage version.
package version

// Version is the SemVer version of okpage. It must match CHANGELOG.md and
// the manifest header in go.mod; scripts/smoke.sh asserts on it.
const Version = "0.1.0"
