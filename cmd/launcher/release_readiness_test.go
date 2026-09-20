package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func releaseRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve release readiness test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func TestProductUIKeepsRuntimeBrandingBehindBoundary(t *testing.T) {
	root := releaseRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{
		"powered by Kilo Code",
		"TL Studio runs Kilo",
		"configured in Kilo",
		">Kilo account<",
		">Sign in to Kilo<",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("product UI leaked bundled runtime branding %q", forbidden)
		}
	}
}

func TestWorkflowsUsePublicRuntimeBoundary(t *testing.T) {
	root := releaseRepoRoot(t)
	for _, workflow := range []string{"release.yml", "custom-provider-contract.yml"} {
		data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", workflow))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		if strings.Contains(source, "/kilo/global/health") {
			t.Fatalf("%s still uses the legacy runtime route", workflow)
		}
		if !strings.Contains(source, "/runtime/global/health") {
			t.Fatalf("%s is missing the TL Studio runtime health boundary", workflow)
		}
	}
}

func TestReleaseWorkflowBuildsBrowserUI(t *testing.T) {
	root := releaseRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"npm run check:web",
		"npm run build:web",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("release workflow missing %q", required)
		}
	}
}
