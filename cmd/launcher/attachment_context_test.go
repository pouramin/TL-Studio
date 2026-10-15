package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadedAttachmentSurvivesStoreReopenAndReachesProvider(t *testing.T) {
	for _, backend := range []string{"", "1"} {
		t.Run("sqlite="+backend, func(t *testing.T) {
			t.Setenv("TL_STUDIO_SQLITE_SESSIONS", backend)
			project, state := t.TempDir(), t.TempDir()
			store := newSessionPersistenceStore(state, "test")
			session, err := store.createNativeSession(project, sessionCreateInput{})
			if err != nil {
				t.Fatal(err)
			}
			content := "TEST-ATTACHMENT-731\nUploaded content, not a project path."
			input := sessionRunInput{Text: "What marker is in the attached test.txt?", Parts: []map[string]any{{
				"type": "file", "mime": "text/plain", "filename": "test.txt",
				"url": "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte(content)),
			}}}
			if err := store.recordAcceptedRun(session.ID, project, input); err != nil {
				t.Fatal(err)
			}
			store.Close()
			store = newSessionPersistenceStore(state, "test")
			defer store.Close()
			messages, found, err := store.getMessages(session.ID)
			if err != nil || !found {
				t.Fatalf("read reopened history: %v %v", found, err)
			}
			if len(messages) != 1 || len(messages[0].Attachments) != 1 || messages[0].Attachments[0].Text != content {
				t.Fatalf("attachment lost after store reopen: %+v", messages)
			}
			if _, err := os.Stat(filepath.Join(project, "test.txt")); !os.IsNotExist(err) {
				t.Fatalf("upload wrote a workspace file: %v", err)
			}
			var captured struct {
				Messages []struct{ Role, Content string } `json:"messages"`
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"TEST-ATTACHMENT-731"},"finish_reason":"stop"}]}`))
			}))
			defer provider.Close()
			_, err = newNativeModelClient().Complete(context.Background(), nativeModelRequest{
				Provider: tlProviderDefinition{Protocol: "openai-compatible", BaseURL: provider.URL}, Model: tlProviderModel{ID: "fixture"},
				System: nativeAgentSystemPrompt(), Messages: nativeConversationFromMessages(messages),
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var userText, systemText string
			for _, message := range captured.Messages {
				if message.Role == "user" {
					userText += message.Content
				}
				if message.Role == "system" {
					systemText += message.Content
				}
			}
			if !strings.Contains(userText, "TEST-ATTACHMENT-731") || !strings.Contains(userText, "filenames are labels") || !strings.Contains(systemText, "do not require a workspace copy") {
				t.Fatal("provider lacks attachment content or upload/workspace distinction")
			}
		})
	}
}
