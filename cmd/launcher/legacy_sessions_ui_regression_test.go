package main

import (
	"io/fs"
	"strings"
	"testing"
)

func TestLegacySessionRecoveryContract(t *testing.T) {
	assets, err := fs.Sub(webFS, "web")
	if err != nil { t.Fatal(err) }

	adapter, err := fs.ReadFile(assets, "runtime-api.js")
	if err != nil { t.Fatal(err) }
	legacy, err := fs.ReadFile(assets, "legacy-sessions.js")
	if err != nil { t.Fatal(err) }
	app, err := fs.ReadFile(assets, "app.js")
	if err != nil { t.Fatal(err) }

	adapterText := string(adapter)
	for _, required := range []string{"legacySessions", "/api/session", "legacyPageQuery", "messages:"} {
		if !strings.Contains(adapterText, required) {
			t.Fatalf("runtime-api.js missing legacy recovery marker %q", required)
		}
	}
	for _, forbidden := range []string{"legacySessions.create", "legacySessions.remove", "legacySessions.prompt"} {
		if strings.Contains(adapterText, forbidden) {
			t.Fatalf("legacy compatibility must remain read-only: found %q", forbidden)
		}
	}

	legacyText := string(legacy)
	for _, required := range []string{
		"__legacy", "Legacy TL Studio session · read-only", "K.api.legacySessions.messages",
		"This legacy session is read-only", "legacy-session", "sessionDirectory",
	} {
		if !strings.Contains(legacyText, required) {
			t.Fatalf("legacy-sessions.js missing %q", required)
		}
	}
	if strings.Contains(legacyText, "cdn.") || strings.Contains(legacyText, "unpkg") || strings.Contains(legacyText, "jsdelivr") {
		t.Fatal("legacy recovery must not depend on external assets")
	}

	appText := string(app)
	legacyAt := strings.Index(appText, `"/legacy-sessions.js"`)
	attachmentsAt := strings.Index(appText, `"/attachments.js"`)
	providersAt := strings.Index(appText, `"/providers-settings-bridge.js"`)
	if legacyAt < 0 {
		t.Fatal("app.js does not load legacy-sessions.js")
	}
	if legacyAt < attachmentsAt || legacyAt < providersAt {
		t.Fatal("legacy recovery must load after composer/provider extensions so read-only guards are final")
	}
}
