package main

import (
	"strings"
	"testing"
)

func TestBrowserUsesTLStudioLiveEventContract(t *testing.T) {
	runtimeAPI := readBrowserSource(t, "runtime-api.ts")
	chat := readBrowserSource(t, "chat.ts")

	for _, required := range []string{
		"new EventSource(path)",
		`"/local/events"`,
	} {
		if !strings.Contains(runtimeAPI, required) {
			t.Fatalf("runtime-api.ts missing TL Studio live event behavior %q", required)
		}
	}

	for _, required := range []string{
		"handleLiveEvent",
		`type === "stream.ready"`,
		`type === "attention.changed"`,
		`type === "session.changed"`,
		`type === "message.changed"`,
		`type === "workspace.changed"`,
		"event?.sessionID",
	} {
		if !strings.Contains(chat, required) {
			t.Fatalf("chat.ts missing semantic live event behavior %q", required)
		}
	}

	for _, forbidden := range []string{
		"/global/event",
		"handleRuntimeEvent",
		"event?.properties",
		"message.updated",
		"message.part.updated",
		"session.status",
		"permission.asked",
		"question.asked",
	} {
		if strings.Contains(runtimeAPI, forbidden) || strings.Contains(chat, forbidden) {
			t.Fatalf("Browser still depends on raw runtime live event detail %q", forbidden)
		}
	}
}
