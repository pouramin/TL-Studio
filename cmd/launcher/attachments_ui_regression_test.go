package main

import (
	"strings"
	"testing"
)

func TestComposerAttachmentsAreEmbeddedAndWired(t *testing.T) {
	entry := readBrowserSource(t, "browser.ts")
	if !strings.Contains(entry, `import "./attachments";`) {
		t.Fatal("Browser module graph does not include the composer attachments module")
	}

	text := readBrowserSource(t, "attachments.ts")
	for _, expected := range []string{
		`type: "file"`,
		`readAsDataURL`,
		`MAX_FILE_BYTES`,
		`attachmentInput.multiple = true`,
		`K.sendPrompt = async () =>`,
		`K.api.sessionCommands.run`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("attachments.ts is missing expected behavior %q", expected)
		}
	}
	if strings.Contains(text, "K.request(") || strings.Contains(text, "/kilo/session/") || strings.Contains(text, "/runtime/session/") {
		t.Fatal("attachments UI bypasses the TL Studio runtime adapter")
	}

	adapterText := readBrowserSource(t, "runtime-api.ts")
	if !strings.Contains(adapterText, "sessionCommands: {") ||
		!strings.Contains(adapterText, "/local/sessions/") ||
		!strings.Contains(adapterText, "/runs") {
		t.Fatal("runtime API adapter does not route structured prompts through the TL Studio session command contract")
	}

	css, err := webFS.ReadFile("web/attachments.css")
	if err != nil {
		t.Fatalf("read embedded attachments.css: %v", err)
	}
	if !strings.Contains(string(css), ".attach-button") || !strings.Contains(string(css), ".composer-attachment") {
		t.Fatal("attachments.css is missing composer attachment styles")
	}
}
