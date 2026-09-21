package main

import (
	"encoding/json"
	"os"
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

	if strings.Contains(global, "KLU: any") {
		t.Fatal("global Browser kernel regressed to any")
	}
	for _, required := range []string{
		"interface TLStudioKernel",
		"interface TLStudioState",
		"interface TLStudioElements",
		"interface TLStudioRuntimeContract",
		"interface TLStudioLiveEvent",
		"KLU: TLStudioKernel",
	} {
		if !strings.Contains(global, required) {
			t.Fatalf("global TypeScript contract missing %q", required)
		}
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
	legacy := read("../../tsconfig.legacy.json")
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

	if !strings.Contains(legacy, `"module": "none"`) || !strings.Contains(legacy, `"outDir": "cmd/launcher/web"`) {
		t.Fatal("legacy compatibility config must remain isolated from the product module build")
	}

	requiredImports := []string{
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
		`"tsconfig.legacy.json"`,
	} {
		if !strings.Contains(build, required) {
			t.Fatalf("Browser build missing %q", required)
		}
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
