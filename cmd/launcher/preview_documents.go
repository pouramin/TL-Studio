package main

import (
	stdhtml "html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func previewEscapedPath(value string) string {
	parts := strings.Split(filepath.ToSlash(strings.TrimSpace(value)), "/")
	for index, part := range parts {
		parts[index] = urlPathEscape(part)
	}
	return "/" + strings.Join(parts, "/")
}

func urlPathEscape(value string) string {
	var b strings.Builder
	for _, r := range []byte(value) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-._~", rune(r)) {
			b.WriteByte(r)
			continue
		}
		const hex = "0123456789ABCDEF"
		b.WriteByte('%')
		b.WriteByte(hex[r>>4])
		b.WriteByte(hex[r&15])
	}
	return b.String()
}

func previewDocumentShell(title, body string) string {
	return "<!doctype html><html><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
		"<title>" + stdhtml.EscapeString(title) + "</title>" +
		"<style>html,body{margin:0;min-height:100%;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,\"Segoe UI\",sans-serif;background:#fff;color:#111}body{box-sizing:border-box}</style>" +
		"</head><body>" + body + "</body></html>"
}

func serveTextPreview(w http.ResponseWriter, r *http.Request, project string) bool {
	if r.URL.Path != "/.tl-preview/text" {
		return false
	}
	entry := strings.TrimSpace(r.URL.Query().Get("file"))
	descriptor, ok := validPreviewEntry(project, entry)
	if !ok || descriptor.Renderer != "text" {
		http.NotFound(w, r)
		return true
	}
	target, _, err := resolveProjectEntry(project, descriptor.Path)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return true
	}
	if info.Size() > maxLocalPreviewBytes {
		http.Error(w, "text preview is too large", http.StatusRequestEntityTooLarge)
		return true
	}
	data, err := os.ReadFile(target)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	body := "<pre style=\"box-sizing:border-box;white-space:pre-wrap;overflow-wrap:anywhere;margin:0;padding:24px;min-height:100vh;font:14px/1.6 ui-monospace,SFMono-Regular,Consolas,monospace;background:#fff;color:#111\">" +
		stdhtml.EscapeString(string(data)) + "</pre>"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(previewDocumentShell(filepath.Base(descriptor.Path), body)))
	return true
}

func servePDFPreview(w http.ResponseWriter, r *http.Request, project string) bool {
	if r.URL.Path != "/.tl-preview/pdf" {
		return false
	}
	entry := strings.TrimSpace(r.URL.Query().Get("file"))
	descriptor, ok := validPreviewEntry(project, entry)
	if !ok || descriptor.Renderer != "pdf" {
		http.NotFound(w, r)
		return true
	}
	source := previewEscapedPath(descriptor.Path)
	title := stdhtml.EscapeString(filepath.Base(descriptor.Path))
	body := "<object data=\"" + source + "#toolbar=1&navpanes=0\" type=\"application/pdf\" width=\"100%\" height=\"100%\" style=\"position:fixed;inset:0;border:0;width:100%;height:100%\">" +
		"<embed src=\"" + source + "#toolbar=1&navpanes=0\" type=\"application/pdf\" style=\"position:fixed;inset:0;border:0;width:100%;height:100%\">" +
		"<div style=\"padding:24px\">PDF preview is unavailable in this browser. <a href=\"" + source + "\" target=\"_blank\" rel=\"noreferrer\">Open " + title + "</a></div>" +
		"</object>"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; object-src 'self'; frame-src 'self'; style-src 'unsafe-inline';")
	_, _ = w.Write([]byte(previewDocumentShell(filepath.Base(descriptor.Path), body)))
	return true
}
