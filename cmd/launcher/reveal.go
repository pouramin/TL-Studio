package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type revealCommand struct {
	Name string
	Args []string
}

var launchRevealCommand = func(command revealCommand) error {
	if command.Name == "" {
		return fmt.Errorf("no reveal command is available")
	}
	return exec.Command(command.Name, command.Args...).Start()
}

func revealCommandForPath(goos, target string, isDir bool) (revealCommand, error) {
	target = filepath.Clean(target)
	switch goos {
	case "windows":
		if isDir {
			return revealCommand{Name: "explorer.exe", Args: []string{target}}, nil
		}
		return revealCommand{Name: "explorer.exe", Args: []string{"/select," + target}}, nil
	case "darwin":
		if isDir {
			return revealCommand{Name: "open", Args: []string{target}}, nil
		}
		return revealCommand{Name: "open", Args: []string{"-R", target}}, nil
	case "linux":
		folder := target
		if !isDir {
			folder = filepath.Dir(target)
		}
		return revealCommand{Name: "xdg-open", Args: []string{folder}}, nil
	default:
		return revealCommand{}, fmt.Errorf("show in folder is not supported on %s", goos)
	}
}

func registerRevealRoute(mux *http.ServeMux, state *appState) {
	mux.HandleFunc("POST /local/reveal", func(w http.ResponseWriter, r *http.Request) {
		target, _, err := resolveProjectEntry(state.projectPath(), r.URL.Query().Get("path"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		info, err := os.Stat(target)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, jsonError{Error: err.Error()})
			} else {
				writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			}
			return
		}
		command, err := revealCommandForPath(runtime.GOOS, target, info.IsDir())
		if err != nil {
			writeJSON(w, http.StatusNotImplemented, jsonError{Error: err.Error()})
			return
		}
		if err := launchRevealCommand(command); err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
