package main

import (
	"strings"
	"testing"
)

func TestParseTargetsAcceptsOnlyReleaseMatrix(t *testing.T) {
	targets, err := parseTargets("darwin/arm64,linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].OS != "darwin" || targets[1].Arch != "amd64" {
		t.Fatalf("targets = %#v", targets)
	}
	for _, value := range []string{"windows/amd64", "linux/386", "linux/amd64,linux/amd64", ""} {
		if _, err := parseTargets(value); err == nil {
			t.Fatalf("parseTargets(%q) succeeded", value)
		}
	}
}

func TestReleaseEnvironmentDisablesDownloadsAndTelemetry(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOTELEMETRY", "on")
	environment := releaseEnvironment([]string{"GOOS=linux", "GOARCH=arm64"})
	joined := strings.Join(environment, "\n")
	for _, want := range []string{"GOTOOLCHAIN=local", "GOTELEMETRY=off", "GOOS=linux", "GOARCH=arm64"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("environment does not contain %q", want)
		}
	}
	if strings.Contains(joined, "GOTOOLCHAIN=auto") || strings.Contains(joined, "GOTELEMETRY=on") {
		t.Fatalf("environment retained overridden policy: %q", joined)
	}
}
