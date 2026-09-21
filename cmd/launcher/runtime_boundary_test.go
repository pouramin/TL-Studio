package main

import (
	"strings"
	"testing"
)

func TestBrowserRuntimeBoundaryHidesImplementationRoute(t *testing.T) {
	source := readBrowserSource(t, "runtime-api.ts")
	if !strings.Contains(source, "/runtime") {
		t.Fatal("runtime adapter must use the TL Studio /runtime boundary")
	}
	if strings.Contains(source, `/kilo${path}`) || strings.Contains(source, `/kilo${route(path)}`) {
		t.Fatal("browser adapter must not call the implementation proxy prefix")
	}
	if strings.Contains(source, "window.KLU") {
		t.Fatal("runtime adapter must use the module-owned Browser kernel")
	}
	if _, err := webFS.ReadFile("web/kilo-api.js"); err == nil {
		t.Fatal("legacy implementation-named browser adapter must not be shipped")
	}
}
