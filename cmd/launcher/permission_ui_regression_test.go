package main

import (
	"strings"
	"testing"
)

func TestPermissionRepliesAreMarkedInteractive(t *testing.T) {
	js, err := webFS.ReadFile("web/runtime-api.js")
	if err != nil {
		t.Fatalf("read embedded runtime-api.js: %v", err)
	}
	text := string(js)
	if !strings.Contains(text, `interactive: true`) {
		t.Fatal("human permission replies must set interactive: true for sensitive runtime permission classes")
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
		`Always allow these`,
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
