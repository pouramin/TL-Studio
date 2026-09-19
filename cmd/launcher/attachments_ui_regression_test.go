package main

import (
	"strings"
	"testing"
)

func TestComposerAttachmentsAreEmbeddedAndWired(t *testing.T) {
	app, err := webFS.ReadFile("web/app.js")
	if err != nil {
		t.Fatalf("read embedded app.js: %v", err)
	}
	if !strings.Contains(string(app), "/attachments.js") {
		t.Fatal("app.js does not load the composer attachments extension")
	}

	js, err := webFS.ReadFile("web/attachments.js")
	if err != nil {
		t.Fatalf("read embedded attachments.js: %v", err)
	}
	text := string(js)
	for _, expected := range []string{
		`type: "file"`,
		`readAsDataURL`,
		`MAX_FILE_BYTES`,
		`attachmentInput.multiple = true`,
		`K.sendPrompt = async () =>`,
		`K.api.sessions.promptAsync`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("attachments.js is missing expected behavior %q", expected)
		}
	}
	if strings.Contains(text, "K.request(") || strings.Contains(text, "/kilo/session/") || strings.Contains(text, "/runtime/session/") {
		t.Fatal("attachments UI bypasses the TL Studio runtime adapter")
	}

	adapter, err := webFS.ReadFile("web/runtime-api.js")
	if err != nil {
		t.Fatalf("read embedded runtime-api.js: %v", err)
	}
	adapterText := string(adapter)
	if !strings.Contains(adapterText, "{ text, parts, agent, model") ||
		!strings.Contains(adapterText, "Array.isArray(parts) && parts.length ? parts") {
		t.Fatal("runtime API adapter does not preserve structured prompt parts")
	}

	css, err := webFS.ReadFile("web/attachments.css")
	if err != nil {
		t.Fatalf("read embedded attachments.css: %v", err)
	}
	if !strings.Contains(string(css), ".attach-button") || !strings.Contains(string(css), ".composer-attachment") {
		t.Fatal("attachments.css is missing composer attachment styles")
	}
}
