package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserQuestionsUseTLStudioSemanticContract(t *testing.T) {
	source := readBrowserSource(t, "runtime-api.ts")
	for _, required := range []string{
		`"/local/questions"`,
		`/local/questions/${enc(requestID)}/reply`,
		`/local/questions/${enc(requestID)}/reject`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Browser question adapter is missing semantic route %q", required)
		}
	}
	for _, forbidden := range []string{
		`route("/question")`,
		`route(`/question/${enc(requestID)}`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Browser question adapter leaked raw runtime question route %q", forbidden)
		}
	}
}

func TestGenericQuestionContractContainsNoKiloDetails(t *testing.T) {
	root := releaseRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("cmd/launcher/question_contract.go")))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{"x-kilo-directory", "KILO_", `"/question"`} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("generic question contract leaked engine detail %q", forbidden)
		}
	}
}

func TestPhase2LocalOwnershipFilesExist(t *testing.T) {
	root := releaseRepoRoot(t)
	for _, relative := range []string{
		"cmd/launcher/question_contract.go",
		"cmd/launcher/runtime_kilo_questions.go",
		"cmd/launcher/credential_store.go",
		"cmd/launcher/session_persistence.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("missing Phase 2 ownership file %s: %v", relative, err)
		}
	}
}

func TestProviderRegistryStillCannotContainCredentials(t *testing.T) {
	source := readRepoText(t, "cmd/launcher/runtime_providers.go")
	if !strings.Contains(source, "credentials.Put") || !strings.Contains(source, "credentials.Get") {
		t.Fatal("provider manager does not use the TL Studio credential store as source of truth")
	}
	if !strings.Contains(source, "setCredential") {
		t.Fatal("provider manager no longer synchronizes owned credentials into the active runtime")
	}
}

func TestSessionReadContractHasTLStudioPersistenceFallback(t *testing.T) {
	source := readRepoText(t, "cmd/launcher/session_contract.go")
	for _, required := range []string{
		"*sessionPersistenceStore",
		"store.upsertSession",
		"store.getSession",
		"store.getMessages",
		"store.getChanges",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("session read contract is missing persistence behavior %q", required)
		}
	}
}

func readRepoText(t *testing.T, relative string) string {
	t.Helper()
	root := releaseRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
