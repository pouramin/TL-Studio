package main

import (
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type previewCapabilityDescriptor struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Renderer   string   `json:"renderer"`
	Extensions []string `json:"extensions"`
}

type previewEntryDescriptor struct {
	Path         string `json:"path"`
	CapabilityID string `json:"capabilityID"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Renderer     string `json:"renderer"`
	MIME         string `json:"mime,omitempty"`
}

type previewCapabilityRegistry struct {
	Version      int                           `json:"version"`
	Capabilities []previewCapabilityDescriptor `json:"capabilities"`
}

var previewFileCapabilities = []previewCapabilityDescriptor{
	{ID: "web.html", Name: "HTML", Kind: "html", Renderer: "browser", Extensions: []string{".html", ".htm"}},
	{ID: "image.svg", Name: "SVG", Kind: "svg", Renderer: "browser", Extensions: []string{".svg"}},
	{ID: "image.raster", Name: "Image", Kind: "image", Renderer: "browser", Extensions: []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".avif", ".ico", ".bmp", ".apng"}},
	{ID: "document.pdf", Name: "PDF", Kind: "pdf", Renderer: "browser", Extensions: []string{".pdf"}},
	{ID: "media.video", Name: "Video", Kind: "video", Renderer: "browser", Extensions: []string{".mp4", ".webm", ".ogv", ".m4v"}},
	{ID: "media.audio", Name: "Audio", Kind: "audio", Renderer: "browser", Extensions: []string{".mp3", ".wav", ".ogg", ".oga", ".m4a", ".aac", ".flac"}},
	{ID: "document.markdown", Name: "Markdown", Kind: "markdown", Renderer: "markdown", Extensions: []string{".md", ".markdown", ".mdown"}},
}

func previewRegistry() previewCapabilityRegistry {
	capabilities := make([]previewCapabilityDescriptor, len(previewFileCapabilities))
	copy(capabilities, previewFileCapabilities)
	return previewCapabilityRegistry{Version: 1, Capabilities: capabilities}
}

func previewCapabilityForPath(value string) (previewCapabilityDescriptor, bool) {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(value)))
	if ext == "" {
		return previewCapabilityDescriptor{}, false
	}
	for _, capability := range previewFileCapabilities {
		for _, candidate := range capability.Extensions {
			if ext == candidate {
				return capability, true
			}
		}
	}
	return previewCapabilityDescriptor{}, false
}

func previewMIMEForPath(value string, capability previewCapabilityDescriptor) string {
	if contentType := strings.TrimSpace(mime.TypeByExtension(strings.ToLower(filepath.Ext(value)))); contentType != "" {
		return contentType
	}
	switch capability.Kind {
	case "html":
		return "text/html; charset=utf-8"
	case "svg":
		return "image/svg+xml"
	case "pdf":
		return "application/pdf"
	case "markdown":
		return "text/markdown; charset=utf-8"
	case "video":
		return "video/*"
	case "audio":
		return "audio/*"
	case "image":
		return "image/*"
	default:
		return ""
	}
}

func validPreviewEntry(project, requested string) (previewEntryDescriptor, bool) {
	capability, ok := previewCapabilityForPath(requested)
	if !ok {
		return previewEntryDescriptor{}, false
	}
	target, rel, err := resolveProjectEntry(project, requested)
	if err != nil {
		return previewEntryDescriptor{}, false
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return previewEntryDescriptor{}, false
	}
	return previewEntryDescriptor{
		Path:         filepath.ToSlash(rel),
		CapabilityID: capability.ID,
		Name:         capability.Name,
		Kind:         capability.Kind,
		Renderer:     capability.Renderer,
		MIME:         previewMIMEForPath(rel, capability),
	}, true
}

func previewFileCandidates(project string) []previewEntryDescriptor {
	root, err := canonicalProjectRoot(project)
	if err != nil {
		return nil
	}
	candidates := make([]previewEntryDescriptor, 0, 16)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			name := strings.ToLower(entry.Name())
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		descriptor, ok := validPreviewEntry(root, rel)
		if !ok {
			return nil
		}
		candidates = append(candidates, descriptor)
		if len(candidates) >= 128 {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i].Path) < strings.ToLower(candidates[j].Path)
	})
	return candidates
}

func findPreviewCandidate(candidates []previewEntryDescriptor, path string) (previewEntryDescriptor, bool) {
	needle := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	for _, candidate := range candidates {
		if strings.ToLower(candidate.Path) == needle {
			return candidate, true
		}
	}
	return previewEntryDescriptor{}, false
}

func defaultHTMLPreviewEntry(candidates []previewEntryDescriptor) (previewEntryDescriptor, bool) {
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.Path, "index.html") {
			return candidate, true
		}
	}
	return previewEntryDescriptor{}, false
}
