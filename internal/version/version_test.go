package version

import (
	"regexp"
	"testing"
)

func TestCurrentIsASemanticVersion(t *testing.T) {
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(Current) {
		t.Fatalf("Current must look like v1.2.3: %q", Current)
	}
	if String() != Current {
		t.Fatal("String returns Current when nothing overrides it")
	}
}

func TestOverrideWins(t *testing.T) {
	Override = "v9.9.9"
	defer func() { Override = "" }()
	if String() != "v9.9.9" {
		t.Fatal("a version set at link time wins")
	}
}
