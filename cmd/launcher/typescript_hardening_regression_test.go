package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypeScriptHardeningPhase1Contract(t *testing.T) {
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	tsconfig := read("../../tsconfig.json")
	check := read("../../tsconfig.check.json")
	global := read("ui/global.d.ts")

	var buildConfig map[string]any
	if err := json.Unmarshal([]byte(tsconfig), &buildConfig); err != nil {
		t.Fatal(err)
	}
	var checkConfig map[string]any
	if err := json.Unmarshal([]byte(check), &checkConfig); err != nil {
		t.Fatal(err)
	}

	buildCompiler, _ := buildConfig["compilerOptions"].(map[string]any)
	checkCompiler, _ := checkConfig["compilerOptions"].(map[string]any)

	if buildCompiler["strict"] != true || checkCompiler["strict"] != true {
		t.Fatalf("both Browser TypeScript configs must keep strict=true")
	}
	if _, exists := buildCompiler["noCheck"]; exists {
		t.Fatal("build tsconfig must not reintroduce noCheck")
	}
	if _, exists := checkCompiler["noCheck"]; exists {
		t.Fatal("check tsconfig must not reintroduce noCheck")
	}

	checkIncludes, _ := checkConfig["include"].([]any)
	joined := make([]string, 0, len(checkIncludes))
	for _, item := range checkIncludes {
		if value, ok := item.(string); ok {
			joined = append(joined, value)
		}
	}
	includeText := strings.Join(joined, "\n")
	if !strings.Contains(includeText, "cmd/launcher/ui/**/*.ts") || !strings.Contains(includeText, "cmd/launcher/ui/**/*.d.ts") {
		t.Fatalf("strict Browser check must cover the complete UI TypeScript surface: %#v", checkIncludes)
	}

	for _, required := range []string{
		"interface TLStudioKernel",
		"interface TLStudioState",
		"interface TLStudioElements",
		"interface TLStudioRuntimeContract",
		"interface TLStudioLiveEvent",
	} {
		if !strings.Contains(global, required) {
			t.Fatalf("global TypeScript contract missing %q", required)
		}
	}
	if strings.Contains(global, "KLU:") {
		t.Fatal("Browser declarations must not expose a global KLU kernel")
	}
}

func TestTypeScriptHardeningPhase2ModuleBuildContract(t *testing.T) {
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	tsconfig := read("../../tsconfig.json")
	check := read("../../tsconfig.check.json")
	entry := read("ui/browser.ts")
	build := read("../../scripts/build-web.mjs")
	index := read("web/index.html")
	ignore := read("../../.gitignore")

	for name, raw := range map[string]string{"tsconfig.json": tsconfig, "tsconfig.check.json": check} {
		var config map[string]any
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			t.Fatal(err)
		}
		compiler, _ := config["compilerOptions"].(map[string]any)
		if compiler["strict"] != true {
			t.Fatalf("%s must keep strict=true", name)
		}
		if compiler["module"] != "ESNext" {
			t.Fatalf("%s must use ESNext modules", name)
		}
		if compiler["moduleResolution"] != "Bundler" {
			t.Fatalf("%s must use Bundler module resolution", name)
		}
		if compiler["noEmit"] != true {
			t.Fatalf("%s must leave JavaScript emission to the Browser build", name)
		}
	}

	requiredImports := []string{
		`import "./kernel";`,
		`import "./core";`,
		`import "./runtime-api";`,
		`import "./workspace";`,
		`import "./monaco";`,
		`import "./preview";`,
		`import "./providers-ui";`,
		`import "./legacy-sessions";`,
		`import "./app";`,
	}
	last := -1
	for _, required := range requiredImports {
		at := strings.Index(entry, required)
		if at < 0 {
			t.Fatalf("Browser module entry missing %q", required)
		}
		if at <= last {
			t.Fatalf("Browser module entry order regressed at %q", required)
		}
		last = at
	}

	for _, required := range []string{
		`"cmd", "launcher", "ui", "browser.ts"`,
		`bundle: true`,
		`format: "esm"`,
		`"browser.js"`,
		`name.endsWith(".js")`,
	} {
		if !strings.Contains(build, required) {
			t.Fatalf("Browser build missing %q", required)
		}
	}
	if strings.Contains(build, "tsconfig.legacy") {
		t.Fatal("Browser build must not use the removed legacy TypeScript emit")
	}
	if _, err := os.Stat("../../tsconfig.legacy.json"); !os.IsNotExist(err) {
		t.Fatal("tsconfig.legacy.json must be removed")
	}

	if !strings.Contains(index, `<script type="module" src="/browser.js"></script>`) {
		t.Fatal("index.html must load the bundled Browser module")
	}
	if strings.Contains(index, `<script src="/core.js"`) || strings.Contains(index, `<script src="/app.js"`) {
		t.Fatal("index.html must not load the historical global scripts directly")
	}
	if !strings.Contains(ignore, "/cmd/launcher/web/*.js") {
		t.Fatal("generated Browser JavaScript must remain untracked")
	}
}

func TestTypeScriptHardeningPhase3ModuleKernelContract(t *testing.T) {
	kernel := readBrowserSource(t, "kernel.ts")
	if !strings.Contains(kernel, "export const K =") {
		t.Fatal("module-owned Browser kernel must export K")
	}
	if strings.Contains(kernel, "window.KLU") {
		t.Fatal("module kernel must not publish itself through window.KLU")
	}

	app := readBrowserSource(t, "app.ts")
	for _, forbidden := range []string{"loadScript", "loadExtensions", "document.createElement(\"script\")"} {
		if strings.Contains(app, forbidden) {
			t.Fatalf("app.ts still contains legacy script loading behavior %q", forbidden)
		}
	}

	files, err := filepath.Glob("ui/*.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		sourceBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source := string(sourceBytes)
		name := filepath.Base(path)
		if strings.Contains(source, "window.KLU") {
			t.Fatalf("%s still depends on window.KLU", name)
		}
		if name != "kernel.ts" && strings.Contains(source, "K.") && !strings.Contains(source, `import { K } from "./kernel";`) {
			t.Fatalf("%s uses K without importing the module-owned kernel", name)
		}
	}
}
