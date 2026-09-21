package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

type questionTestAdapter struct {
	items        []questionRequestView
	replyID      string
	replyAnswers [][]string
	rejectID     string
}

func (a *questionTestAdapter) ListQuestions(context.Context, *runtimeBackend, string) ([]questionRequestView, error) {
	return append([]questionRequestView(nil), a.items...), nil
}

func (a *questionTestAdapter) ReplyQuestion(_ context.Context, _ *runtimeBackend, _ string, requestID string, answers [][]string) error {
	a.replyID = requestID
	a.replyAnswers = answers
	return nil
}

func (a *questionTestAdapter) RejectQuestion(_ context.Context, _ *runtimeBackend, _ string, requestID string) error {
	a.rejectID = requestID
	return nil
}

type questionTestEngine struct {
	adapter *questionTestAdapter
}

func (*questionTestEngine) ID() string { return "question-test" }
func (*questionTestEngine) FindBinary(override string) (string, error) { return override, nil }
func (*questionTestEngine) Command(context.Context, string, int, runtimeCredentials) *exec.Cmd { return nil }
func (*questionTestEngine) PrepareRequest(*http.Request, string, runtimeCredentials) {}
func (e *questionTestEngine) Questions() runtimeQuestionAdapter { return e.adapter }

func TestQuestionRoutesUseSemanticAdapter(t *testing.T) {
	project := t.TempDir()
	adapter := &questionTestAdapter{
		items: []questionRequestView{
			{
				ID: "q1", SessionID: "s1",
				Questions: []questionPromptView{{
					Header: "Choice", Question: "Pick one", Custom: true,
					Options: []questionOptionView{{Label: "A"}, {Label: "B", Description: "second"}},
				}},
			},
			{ID: "q2", SessionID: "s2", Questions: []questionPromptView{{Question: "Other", Custom: true}}},
		},
	}
	engine := &questionTestEngine{adapter: adapter}
	state := &appState{project: project}
	backend, err := newRuntimeBackend(state, "http://127.0.0.1:1", runtimeCredentials{}, engine)
	if err != nil {
		t.Fatal(err)
	}
	contract := newQuestionContract(state, backend)
	mux := http.NewServeMux()
	registerQuestionRoutes(mux, contract)
	server := httptest.NewServer(mux)
	defer server.Close()

	res, err := http.Get(server.URL + "/local/questions?sessionID=s1")
	if err != nil {
		t.Fatal(err)
	}
	var listed []questionRequestView
	if err := json.NewDecoder(res.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || len(listed) != 1 || listed[0].ID != "q1" {
		t.Fatalf("semantic question list mismatch: status=%d items=%#v", res.StatusCode, listed)
	}

	body := strings.NewReader(`{"sessionID":"s1","answers":[["A"],["custom value"]]}`)
	replyReq, _ := http.NewRequest(http.MethodPost, server.URL+"/local/questions/q1/reply", body)
	replyReq.Header.Set("Content-Type", "application/json")
	replyRes, err := http.DefaultClient.Do(replyReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = replyRes.Body.Close()
	if replyRes.StatusCode != http.StatusOK || adapter.replyID != "q1" || len(adapter.replyAnswers) != 2 {
		t.Fatalf("semantic question reply mismatch: status=%d id=%q answers=%#v", replyRes.StatusCode, adapter.replyID, adapter.replyAnswers)
	}

	rejectReq, _ := http.NewRequest(http.MethodPost, server.URL+"/local/questions/q1/reject", strings.NewReader(`{"sessionID":"s1"}`))
	rejectReq.Header.Set("Content-Type", "application/json")
	rejectRes, err := http.DefaultClient.Do(rejectReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = rejectRes.Body.Close()
	if rejectRes.StatusCode != http.StatusOK || adapter.rejectID != "q1" {
		t.Fatalf("semantic question reject mismatch: status=%d id=%q", rejectRes.StatusCode, adapter.rejectID)
	}
}
