package parser

import (
	"regexp"
	"testing"
)

// semverRe matches MAJOR.MINOR.PATCH (digits only — no prerelease/build suffix).
var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// TestVersionsNonEmpty ensures parsers populate the centralized version map.
func TestVersionsNonEmpty(t *testing.T) {
	v := Versions()
	if len(v) == 0 {
		t.Fatal("Versions() returned empty map; expected at least one parser registered")
	}
}

// TestVersionsKeysMatchKnownFormats ensures every key in Versions() is a real
// parser format name we recognize — no orphan entries.
func TestVersionsKeysMatchKnownFormats(t *testing.T) {
	known := map[string]bool{
		"spring":    true,
		"compose":   true,
		"k8s":       true,
		"envfile":   true,
		"buildfile": true,
	}
	v := Versions()
	for name := range v {
		if !known[name] {
			t.Errorf("Versions() has unknown format key %q", name)
		}
	}
	// Also ensure every known format is present.
	for name := range known {
		if _, ok := v[name]; !ok {
			t.Errorf("Versions() missing expected format %q", name)
		}
	}
}

// TestVersionsValuesAreSemver ensures every value matches MAJOR.MINOR.PATCH.
func TestVersionsValuesAreSemver(t *testing.T) {
	for name, ver := range Versions() {
		if !semverRe.MatchString(ver) {
			t.Errorf("parser %q version %q is not MAJOR.MINOR.PATCH", name, ver)
		}
	}
}
