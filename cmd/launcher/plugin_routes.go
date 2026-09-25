package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
)

func writePluginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		writeJSON(w, http.StatusNotFound, jsonError{Error: "plugin not found"})
	case strings.Contains(err.Error(), "required"),
		strings.Contains(err.Error(), "plugin ID is reserved"),
		strings.Contains(err.Error(), "bundled plugins cannot be removed"),
		strings.Contains(err.Error(), "must use"),
		strings.Contains(err.Error(), "only stdio"),
		strings.Contains(err.Error(), "only MCP"),
		strings.Contains(err.Error(), "scope must"),
		strings.Contains(err.Error(), "changing plugin scope"),
		strings.Contains(err.Error(), "invalid environment"),
		strings.Contains(err.Error(), "Graphify graph is missing"),
		strings.Contains(err.Error(), "working directory"):
		writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
	}
}

func decodePluginUpsert(w http.ResponseWriter, r *http.Request) (pluginUpsertRequest, error) {
	var input pluginUpsertRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&input); err != nil {
		return pluginUpsertRequest{}, errors.New("invalid JSON body")
	}
	return input, nil
}

func registerPluginRoutes(mux *http.ServeMux, state *appState, manager *pluginManager) {
	mux.HandleFunc("GET /local/plugins", func(w http.ResponseWriter, r *http.Request) {
		views, err := manager.List(state.projectPath(), true)
		if err != nil {
			writePluginError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, views)
	})

	mux.HandleFunc("POST /local/plugins", func(w http.ResponseWriter, r *http.Request) {
		input, err := decodePluginUpsert(w, r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		view, err := manager.Upsert(state.projectPath(), input)
		if err != nil {
			writePluginError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, view)
	})

	mux.HandleFunc("PUT /local/plugins/{pluginID}", func(w http.ResponseWriter, r *http.Request) {
		input, err := decodePluginUpsert(w, r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		input.Plugin.ID = strings.TrimSpace(r.PathValue("pluginID"))
		view, err := manager.Upsert(state.projectPath(), input)
		if err != nil {
			writePluginError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})

	mux.HandleFunc("DELETE /local/plugins/{pluginID}", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.Remove(state.projectPath(), r.PathValue("pluginID")); err != nil {
			writePluginError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /local/plugins/test", func(w http.ResponseWriter, r *http.Request) {
		input, err := decodePluginUpsert(w, r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		view, err := manager.TestConfig(r.Context(), state.projectPath(), input)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "plugin": view})
			return
		}
		writeJSON(w, http.StatusOK, view)
	})

	mux.HandleFunc("POST /local/plugins/{pluginID}/test", func(w http.ResponseWriter, r *http.Request) {
		view, err := manager.TestSaved(r.Context(), state.projectPath(), r.PathValue("pluginID"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "plugin": view})
			return
		}
		writeJSON(w, http.StatusOK, view)
	})

	mux.HandleFunc("POST /local/plugins/{pluginID}/enabled", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled   bool `json:"enabled"`
			Confirmed bool `json:"confirmed,omitempty"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid JSON body"})
			return
		}
		if body.Enabled && !body.Confirmed {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "enabling a plugin requires explicit confirmation"})
			return
		}
		view, err := manager.SetEnabled(state.projectPath(), r.PathValue("pluginID"), body.Enabled)
		if err != nil {
			writePluginError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})

	mux.HandleFunc("POST /local/plugins/{pluginID}/actions/{actionID}", func(w http.ResponseWriter, r *http.Request) {
		var body struct { Confirmed bool `json:"confirmed,omitempty"` }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "invalid JSON body"})
			return
		}
		result, err := manager.RunIntegrationAction(
			r.Context(),
			state.projectPath(),
			r.PathValue("pluginID"),
			r.PathValue("actionID"),
			body.Confirmed,
		)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]any{"error": err.Error(), "result": result})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
