package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

var errQuestionsUnsupported = errors.New("runtime engine does not provide interactive question capability")

type questionOptionView struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type questionPromptView struct {
	Header   string               `json:"header,omitempty"`
	Question string               `json:"question"`
	Options  []questionOptionView `json:"options"`
	Multiple bool                 `json:"multiple,omitempty"`
	Custom   bool                 `json:"custom"`
	Default  string               `json:"default,omitempty"`
}

type questionRequestView struct {
	ID        string               `json:"id"`
	SessionID string               `json:"sessionID"`
	Questions []questionPromptView `json:"questions"`
}

type questionReplyInput struct {
	SessionID string     `json:"sessionID,omitempty"`
	Answers   [][]string `json:"answers"`
}

type questionRejectInput struct {
	SessionID string `json:"sessionID,omitempty"`
}

type runtimeQuestionAdapter interface {
	ListQuestions(ctx context.Context, backend *runtimeBackend, directory string) ([]questionRequestView, error)
	ReplyQuestion(ctx context.Context, backend *runtimeBackend, directory, requestID string, answers [][]string) error
	RejectQuestion(ctx context.Context, backend *runtimeBackend, directory, requestID string) error
}

type runtimeQuestionProvider interface {
	Questions() runtimeQuestionAdapter
}

type questionContract struct {
	state   *appState
	backend *runtimeBackend
	adapter runtimeQuestionAdapter
}

func newQuestionContract(state *appState, backend *runtimeBackend) *questionContract {
	var adapter runtimeQuestionAdapter
	if provider, ok := backend.engine.(runtimeQuestionProvider); ok {
		adapter = provider.Questions()
	}
	return &questionContract{state: state, backend: backend, adapter: adapter}
}

func (c *questionContract) requireAdapter() (runtimeQuestionAdapter, error) {
	if c.adapter == nil {
		return nil, errQuestionsUnsupported
	}
	return c.adapter, nil
}

func (c *questionContract) directory() string {
	if c.state == nil {
		return ""
	}
	return c.state.projectPath()
}

func (c *questionContract) list(ctx context.Context, sessionID string) ([]questionRequestView, error) {
	adapter, err := c.requireAdapter()
	if err != nil {
		return nil, err
	}
	items, err := adapter.ListQuestions(ctx, c.backend, c.directory())
	if err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	result := make([]questionRequestView, 0, len(items))
	for _, item := range items {
		if sessionID != "" && item.SessionID != sessionID {
			continue
		}
		if item.ID == "" || item.SessionID == "" || len(item.Questions) == 0 {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func (c *questionContract) validateRequest(ctx context.Context, sessionID, requestID string) error {
	sessionID = strings.TrimSpace(sessionID)
	requestID = strings.TrimSpace(requestID)
	if sessionID == "" || requestID == "" {
		return errors.New("session id and question request id are required")
	}
	items, err := c.list(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID == requestID {
			return nil
		}
	}
	return errors.New("question request is not active for this session")
}

func normalizeQuestionAnswers(answers [][]string) ([][]string, error) {
	if len(answers) == 0 || len(answers) > 20 {
		return nil, errors.New("question answers are required")
	}
	result := make([][]string, len(answers))
	for index, answer := range answers {
		if len(answer) == 0 || len(answer) > 50 {
			return nil, errors.New("every question requires at least one answer")
		}
		for _, value := range answer {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if len(value) > 8000 {
				return nil, errors.New("question answer is too long")
			}
			result[index] = append(result[index], value)
		}
		if len(result[index]) == 0 {
			return nil, errors.New("every question requires at least one answer")
		}
	}
	return result, nil
}

func writeQuestionError(w http.ResponseWriter, err error) {
	if errors.Is(err, errQuestionsUnsupported) {
		writeJSON(w, http.StatusNotImplemented, jsonError{Error: err.Error()})
		return
	}
	var runtimeErr *sessionRuntimeError
	if errors.As(err, &runtimeErr) {
		writeSessionContractError(w, err)
		return
	}
	writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
}

func registerQuestionRoutes(mux *http.ServeMux, contract *questionContract) {
	mux.HandleFunc("GET /local/questions", func(w http.ResponseWriter, r *http.Request) {
		items, err := contract.list(r.Context(), r.URL.Query().Get("sessionID"))
		if err != nil {
			writeQuestionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	})

	mux.HandleFunc("POST /local/questions/{requestID}/reply", func(w http.ResponseWriter, r *http.Request) {
		adapter, err := contract.requireAdapter()
		if err != nil {
			writeQuestionError(w, err)
			return
		}
		var input questionReplyInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid question reply body"})
			return
		}
		answers, err := normalizeQuestionAnswers(input.Answers)
		if err != nil {
			writeQuestionError(w, err)
			return
		}
		requestID := strings.TrimSpace(r.PathValue("requestID"))
		if err := contract.validateRequest(r.Context(), input.SessionID, requestID); err != nil {
			writeQuestionError(w, err)
			return
		}
		if err := adapter.ReplyQuestion(r.Context(), contract.backend, contract.directory(), requestID, answers); err != nil {
			writeQuestionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"resolved": true})
	})

	mux.HandleFunc("POST /local/questions/{requestID}/reject", func(w http.ResponseWriter, r *http.Request) {
		adapter, err := contract.requireAdapter()
		if err != nil {
			writeQuestionError(w, err)
			return
		}
		var input questionRejectInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid question reject body"})
			return
		}
		requestID := strings.TrimSpace(r.PathValue("requestID"))
		if err := contract.validateRequest(r.Context(), input.SessionID, requestID); err != nil {
			writeQuestionError(w, err)
			return
		}
		if err := adapter.RejectQuestion(r.Context(), contract.backend, contract.directory(), requestID); err != nil {
			writeQuestionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"resolved": true})
	})
}
