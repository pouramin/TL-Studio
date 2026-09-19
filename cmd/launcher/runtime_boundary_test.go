package main

import (
  "io/fs"
  "strings"
  "testing"
)

func TestBrowserRuntimeBoundaryHidesImplementationRoute(t *testing.T) {
  assets, err := fs.Sub(webFS, "web")
  if err != nil { t.Fatal(err) }
  runtimeAPI, err := fs.ReadFile(assets, "runtime-api.js")
  if err != nil { t.Fatal(err) }
  source := string(runtimeAPI)
  if !strings.Contains(source, "/runtime") {
    t.Fatal("runtime adapter must use the TL Studio /runtime boundary")
  }
  if strings.Contains(source, "`/kilo${path}`") || strings.Contains(source, "`/kilo${route(path)}`") {
    t.Fatal("browser adapter must not call the implementation proxy prefix")
  }
  if _, err := fs.Stat(assets, "kilo-api.js"); err == nil {
    t.Fatal("legacy implementation-named browser adapter must not be shipped")
  }
}
