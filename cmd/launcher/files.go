package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxLocalPreviewBytes int64 = 2 * 1024 * 1024
const maxLocalWriteBytes int64 = 2 * 1024 * 1024

type localFileEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Size     int64  `json:"size,omitempty"`
	Modified string `json:"modified,omitempty"`
}

type localFileList struct {
	Path    string           `json:"path"`
	Entries []localFileEntry `json:"entries"`
}

type localFilePreview struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Mime     string `json:"mime"`
	Binary   bool   `json:"binary"`
	Content  string `json:"content,omitempty"`
	Modified string `json:"modified,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

type localFileWriteRequest struct {
	Path           string `json:"path"`
	Content        string `json:"content"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
	Force          bool   `json:"force,omitempty"`
}

type localEntryCreateRequest struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type localEntryRenameRequest struct {
	Path    string `json:"path"`
	NewPath string `json:"newPath"`
}

func registerLocalFileRoutes(mux *http.ServeMux, state *appState) {
	registerProjectHistoryRoute(mux, state)

	mux.HandleFunc("GET /local/files", func(w http.ResponseWriter, r *http.Request) {
		project := state.projectPath()
		target, rel, err := resolveProjectEntry(project, r.URL.Query().Get("path"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		info, err := os.Stat(target)
		if err != nil {
			writeJSON(w, http.StatusNotFound, jsonError{Error: err.Error()})
			return
		}
		if !info.IsDir() {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "requested path is not a directory"})
			return
		}
		entries, err := os.ReadDir(target)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		result := localFileList{Path: rel, Entries: make([]localFileEntry, 0, len(entries))}
		for _, entry := range entries {
			if entry.Name() == ".git" && entry.IsDir() {
				continue
			}
			item, itemErr := localFileEntryFromDirEntry(rel, entry)
			if itemErr == nil {
				result.Entries = append(result.Entries, item)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("GET /local/file", func(w http.ResponseWriter, r *http.Request) {
		preview, status, err := readLocalFilePreview(state.projectPath(), r.URL.Query().Get("path"))
		if err != nil {
			writeJSON(w, status, jsonError{Error: err.Error()})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, preview)
	})

	mux.HandleFunc("PUT /local/file", func(w http.ResponseWriter, r *http.Request) {
		var body localFileWriteRequest
		if err := decodeLocalJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		if int64(len([]byte(body.Content))) > maxLocalWriteBytes {
			writeJSON(w, http.StatusRequestEntityTooLarge, jsonError{Error: fmt.Sprintf("file is too large to edit (max %d MiB)", maxLocalWriteBytes/(1024*1024))})
			return
		}

		target, _, err := resolveProjectMutationPath(state.projectPath(), body.Path)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		info, err := os.Lstat(target)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, jsonError{Error: err.Error()})
			} else {
				writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			}
			return
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "requested path is not an editable regular file"})
			return
		}
		current, err := os.ReadFile(target)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		if !body.Force && body.ExpectedSHA256 != "" && !strings.EqualFold(body.ExpectedSHA256, fileSHA256(current)) {
			writeJSON(w, http.StatusConflict, jsonError{Error: "file changed on disk since it was opened"})
			return
		}
		if err := os.WriteFile(target, []byte(body.Content), info.Mode().Perm()); err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		preview, status, err := readLocalFilePreview(state.projectPath(), body.Path)
		if err != nil {
			writeJSON(w, status, jsonError{Error: err.Error()})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, preview)
	})

	mux.HandleFunc("POST /local/entry", func(w http.ResponseWriter, r *http.Request) {
		var body localEntryCreateRequest
		if err := decodeLocalJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		target, rel, err := resolveProjectMutationPath(state.projectPath(), body.Path)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		if _, err := os.Lstat(target); err == nil {
			writeJSON(w, http.StatusConflict, jsonError{Error: "an entry already exists at that path"})
			return
		} else if !os.IsNotExist(err) {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		switch strings.ToLower(strings.TrimSpace(body.Type)) {
		case "file":
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
				return
			}
			if err := file.Close(); err != nil {
				writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
				return
			}
		case "directory":
			if err := os.Mkdir(target, 0o755); err != nil {
				writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
				return
			}
		default:
			writeJSON(w, http.StatusBadRequest, jsonError{Error: "entry type must be file or directory"})
			return
		}
		entry, err := localFileEntryFromPath(target, rel)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, entry)
	})

	mux.HandleFunc("PATCH /local/entry", func(w http.ResponseWriter, r *http.Request) {
		var body localEntryRenameRequest
		if err := decodeLocalJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		source, sourceRel, err := resolveProjectMutationPath(state.projectPath(), body.Path)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		target, targetRel, err := resolveProjectMutationPath(state.projectPath(), body.NewPath)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		if sourceRel == targetRel {
			entry, entryErr := localFileEntryFromPath(source, sourceRel)
			if entryErr != nil {
				writeJSON(w, http.StatusNotFound, jsonError{Error: entryErr.Error()})
				return
			}
			writeJSON(w, http.StatusOK, entry)
			return
		}
		if _, err := os.Lstat(source); err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, jsonError{Error: err.Error()})
			} else {
				writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			}
			return
		}
		if _, err := os.Lstat(target); err == nil {
			writeJSON(w, http.StatusConflict, jsonError{Error: "an entry already exists at the destination"})
			return
		} else if !os.IsNotExist(err) {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		if err := os.Rename(source, target); err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		entry, err := localFileEntryFromPath(target, targetRel)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, entry)
	})

	mux.HandleFunc("DELETE /local/entry", func(w http.ResponseWriter, r *http.Request) {
		target, rel, err := resolveProjectMutationPath(state.projectPath(), r.URL.Query().Get("path"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{Error: err.Error()})
			return
		}
		info, err := os.Lstat(target)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, jsonError{Error: err.Error()})
			} else {
				writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			}
			return
		}
		recursive := strings.EqualFold(r.URL.Query().Get("recursive"), "true") || r.URL.Query().Get("recursive") == "1"
		if info.IsDir() && recursive {
			err = os.RemoveAll(target)
		} else {
			err = os.Remove(target)
		}
		if err != nil {
			writeJSON(w, http.StatusConflict, jsonError{Error: err.Error()})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-TL-Studio-Deleted-Path", rel)
		w.WriteHeader(http.StatusNoContent)
	})
}

func decodeLocalJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxLocalWriteBytes+64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func readLocalFilePreview(project, requested string) (localFilePreview, int, error) {
	target, rel, err := resolveProjectEntry(project, requested)
	if err != nil {
		return localFilePreview{}, http.StatusBadRequest, err
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return localFilePreview{}, http.StatusNotFound, err
		}
		return localFilePreview{}, http.StatusInternalServerError, err
	}
	if !info.Mode().IsRegular() {
		return localFilePreview{}, http.StatusBadRequest, fmt.Errorf("requested path is not a regular file")
	}
	if info.Size() > maxLocalPreviewBytes {
		return localFilePreview{}, http.StatusRequestEntityTooLarge, fmt.Errorf("file is too large to preview (max %d MiB)", maxLocalPreviewBytes/(1024*1024))
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return localFilePreview{}, http.StatusInternalServerError, err
	}
	binary := bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(target)))
	if mimeType == "" {
		if binary {
			mimeType = "application/octet-stream"
		} else {
			mimeType = "text/plain; charset=utf-8"
		}
	}
	preview := localFilePreview{
		Name:     filepath.Base(target),
		Path:     rel,
		Size:     info.Size(),
		Mime:     mimeType,
		Binary:   binary,
		Modified: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		SHA256:   fileSHA256(data),
	}
	if !binary {
		preview.Content = string(data)
	}
	return preview, http.StatusOK, nil
}

func localFileEntryFromDirEntry(parent string, entry os.DirEntry) (localFileEntry, error) {
	entryType := "file"
	if entry.IsDir() {
		entryType = "directory"
	} else if entry.Type()&os.ModeSymlink != 0 {
		entryType = "symlink"
	}
	item := localFileEntry{
		Name: entry.Name(),
		Path: slashJoin(parent, entry.Name()),
		Type: entryType,
	}
	if fileInfo, err := entry.Info(); err == nil {
		if !fileInfo.IsDir() {
			item.Size = fileInfo.Size()
		}
		item.Modified = fileInfo.ModTime().UTC().Format("2006-01-02T15:04:05Z")
	}
	return item, nil
}

func localFileEntryFromPath(target, rel string) (localFileEntry, error) {
	info, err := os.Lstat(target)
	if err != nil {
		return localFileEntry{}, err
	}
	entryType := "file"
	if info.IsDir() {
		entryType = "directory"
	} else if info.Mode()&os.ModeSymlink != 0 {
		entryType = "symlink"
	}
	item := localFileEntry{
		Name:     filepath.Base(target),
		Path:     filepath.ToSlash(rel),
		Type:     entryType,
		Modified: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
	}
	if !info.IsDir() {
		item.Size = info.Size()
	}
	return item, nil
}

func fileSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func slashJoin(parent, name string) string {
	parent = strings.Trim(strings.ReplaceAll(parent, "\\", "/"), "/")
	name = strings.Trim(strings.ReplaceAll(name, "\\", "/"), "/")
	if parent == "" {
		return name
	}
	if name == "" {
		return parent
	}
	return parent + "/" + name
}

func resolveProjectEntry(project, requested string) (string, string, error) {
	root, err := canonicalProjectRoot(project)
	if err != nil {
		return "", "", err
	}

	raw := strings.TrimSpace(requested)
	if raw == "" || raw == "." {
		return root, "", nil
	}
	candidate, err := cleanRelativeProjectPath(raw)
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(root, candidate)
	evaluated, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", "", err
	}
	rel, err := relativeToProjectRoot(root, evaluated)
	if err != nil {
		return "", "", err
	}
	return evaluated, filepath.ToSlash(rel), nil
}

func resolveProjectMutationPath(project, requested string) (string, string, error) {
	root, err := canonicalProjectRoot(project)
	if err != nil {
		return "", "", err
	}
	raw := strings.TrimSpace(requested)
	if raw == "" || raw == "." {
		return "", "", fmt.Errorf("project root cannot be modified")
	}
	clean, err := cleanRelativeProjectPath(raw)
	if err != nil {
		return "", "", err
	}
	cleanSlash := filepath.ToSlash(clean)
	first := strings.Split(cleanSlash, "/")[0]
	if strings.EqualFold(first, ".git") {
		return "", "", fmt.Errorf(".git is protected from workspace mutations")
	}

	lexicalTarget := filepath.Join(root, clean)
	parent := filepath.Dir(lexicalTarget)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", "", err
	}
	if _, err := relativeToProjectRoot(root, resolvedParent); err != nil {
		return "", "", err
	}
	target := filepath.Join(resolvedParent, filepath.Base(lexicalTarget))
	rel, err := relativeToProjectRoot(root, target)
	if err != nil {
		return "", "", err
	}
	if rel == "" {
		return "", "", fmt.Errorf("project root cannot be modified")
	}
	return target, filepath.ToSlash(rel), nil
}

func canonicalProjectRoot(project string) (string, error) {
	if strings.TrimSpace(project) == "" {
		return "", fmt.Errorf("no project is selected")
	}
	root, err := filepath.Abs(project)
	if err != nil {
		return "", err
	}
	if evaluated, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root = evaluated
	}
	return filepath.Clean(root), nil
}

func cleanRelativeProjectPath(raw string) (string, error) {
	candidate := filepath.FromSlash(raw)
	if filepath.IsAbs(candidate) || filepath.VolumeName(candidate) != "" {
		return "", fmt.Errorf("path must be relative to the selected project")
	}
	clean := filepath.Clean(candidate)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("path must identify an entry inside the selected project")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes the selected project")
	}
	return clean, nil
}

func relativeToProjectRoot(root, target string) (string, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes the selected project")
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
}
