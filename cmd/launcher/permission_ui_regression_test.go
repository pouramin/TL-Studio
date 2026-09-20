package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserPermissionsUseTLStudioEngine(t *testing.T) {
	js, err := webFS.ReadFile("web/runtime-api.js")
	if err != nil {
		t.Fatalf("read embedded runtime-api.js: %v", err)
	}
	text := string(js)
	for _, expected := range []string{
		`/local/permissions`,
		`/local/permissions/rules`,
		`removeRule`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("browser permission adapter is missing TL Studio route %q", expected)
		}
	}
	if strings.Contains(text, `request(route("/permission"))`) {
		t.Fatal("browser permission UI must not list permissions directly from the bundled runtime")
	}

	root := releaseRepoRoot(t)
	engine, err := os.ReadFile(filepath.Join(root, "cmd", "launcher", "permission_engine.go"))
	if err != nil {
		t.Fatal(err)
	}
	engineText := string(engine)
	for _, expected := range []string{
		`"interactive": interactive`,
		`"Approved by TL Studio project permission policy."`,
		`permissionCanRemember`,
		`permissions.json`,
	} {
		if !strings.Contains(engineText, expected) {
			t.Fatalf("TL Studio permission engine is missing %q", expected)
		}
	}
}

func TestPermissionUIExplainsScopedAlwaysRules(t *testing.T) {
	js, err := webFS.ReadFile("web/attention.js")
	if err != nil {
		t.Fatalf("read embedded attention.js: %v", err)
	}
	text := string(js)
	for _, expected := range []string{
		`Agent wants permission to`,
		`Always allow in this project`,
		`TL Studio can remember these rules for this project`,
		`metadata.skillShell === true`,
		`metadata.sandboxEscalation === true`,
		`metadata.disableAlways !== true`,
		`alwaysRules.length > 0`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("attention.js is missing expected permission behavior %q", expected)
		}
	}
	if strings.Contains(text, `Kilo wants permission to`) {
		t.Fatal("permission UI should use TL Studio product language instead of upstream branding")
	}
}
