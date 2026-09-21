package main

import (
	"strings"
	"testing"
)

func TestLegacySessionRecoveryContract(t *testing.T) {
	adapterText := readBrowserSource(t, "runtime-api.ts")
	for _, required := range []string{"legacySessions", "/api/session", "legacyPageQuery", "messages:"} {
		if !strings.Contains(adapterText, required) {
			t.Fatalf("runtime-api.ts missing legacy recovery marker %q", required)
		}
	}
	for _, forbidden := range []string{"legacySessions.create", "legacySessions.remove", "legacySessions.prompt"} {
		if strings.Contains(adapterText, forbidden) {
			t.Fatalf("legacy compatibility must remain read-only: found %q", forbidden)
		}
	}

	legacyText := readBrowserSource(t, "legacy-sessions.ts")
	for _, required := range []string{
		"__legacy", "Legacy TL Studio session · read-only", "K.api.legacySessions.messages",
		"This legacy session is read-only", "legacy-session", "sessionDirectory",
	} {
		if !strings.Contains(legacyText, required) {
			t.Fatalf("legacy-sessions.ts missing %q", required)
		}
	}
	if strings.Contains(legacyText, "cdn.") || strings.Contains(legacyText, "unpkg") || strings.Contains(legacyText, "jsdelivr") {
		t.Fatal("legacy recovery must not depend on external assets")
	}

	entry := readBrowserSource(t, "browser.ts")
	legacyAt := strings.Index(entry, `import "./legacy-sessions";`)
	attachmentsAt := strings.Index(entry, `import "./attachments";`)
	providersAt := strings.Index(entry, `import "./providers-settings-bridge";`)
	if legacyAt < 0 {
		t.Fatal("Browser module graph does not include legacy-sessions")
	}
	if legacyAt < attachmentsAt || legacyAt < providersAt {
		t.Fatal("legacy recovery must initialize after composer/provider modules so read-only guards are final")
	}
}
