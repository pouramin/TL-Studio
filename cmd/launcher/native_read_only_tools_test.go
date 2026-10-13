package main

import "testing"

func TestNativePromptExecutionDetectionSupportsPersianAndLeastPrivilege(t *testing.T) {
	if !nativePromptRequiresExecution("داخل پوشه سه ناسازگاری واقعی را اصلاح کن و فایل‌ها را دوباره بخوان") {
		t.Fatal("Persian mutation request should require execution")
	}
	if !nativePromptRequiresExecution("فایل ANALYSIS.md را بنویس و بعد تست کن") {
		t.Fatal("Persian write request should require execution")
	}
	if nativePromptRequiresExecution("همه فایل‌ها را واقعاً بخوان و تحلیل کن و نتیجه را بگو") {
		t.Fatal("Persian read-only analysis should not require execution")
	}
	if nativePromptRequiresExecution("فقط بررسی کن و هیچ فایلی را تغییر نده") {
		t.Fatal("explicit Persian read-only request must remain read-only")
	}
}

func TestNativeReadOnlyTurnWithholdsWriteAndExecuteTools(t *testing.T) {
	executor := newNativeToolExecutor(nil, nativeAllowAuthorizer{})
	definitions := nativeToolDefinitionsForPrompt(executor, t.TempDir(), "Read every file and analyze the relationships.")
	seen := map[string]bool{}
	for _, definition := range definitions {
		seen[definition.ID] = true
	}
	for _, required := range []string{"files.read", "files.list", "search.content"} {
		if !seen[required] {
			t.Fatalf("read-only turn is missing %s: %#v", required, seen)
		}
	}
	for _, forbidden := range []string{"files.write", "files.edit", "terminal.command"} {
		if seen[forbidden] {
			t.Fatalf("read-only turn exposed mutating/execute tool %s", forbidden)
		}
	}
}

func TestNativeExecutionTurnKeepsMutationTools(t *testing.T) {
	executor := newNativeToolExecutor(nil, nativeAllowAuthorizer{})
	definitions := nativeToolDefinitionsForPrompt(executor, t.TempDir(), "اصلاح کن و تست‌ها را اجرا کن")
	seen := map[string]bool{}
	for _, definition := range definitions {
		seen[definition.ID] = true
	}
	for _, required := range []string{"files.write", "files.edit", "terminal.command"} {
		if !seen[required] {
			t.Fatalf("execution turn is missing %s: %#v", required, seen)
		}
	}
}
