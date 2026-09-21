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
