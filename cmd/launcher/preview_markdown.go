package main

import (
	"fmt"
	stdhtml "html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownHeadingPattern = regexp.MustCompile("^(#{1,6})\\s+(.+)$")
var markdownOrderedPattern = regexp.MustCompile("^\\d+[.)]\\s+(.+)$")
var markdownStrongPattern = regexp.MustCompile("\\*\\*([^*]+)\\*\\*")
var markdownEmPattern = regexp.MustCompile("\\*([^*]+)\\*")
var markdownCodePattern = regexp.MustCompile("\\x60([^\\x60]+)\\x60")

func renderMarkdownInline(value string) string {
	escaped := stdhtml.EscapeString(strings.TrimSpace(value))
	escaped = markdownCodePattern.ReplaceAllString(escaped, "<code>$1</code>")
	escaped = markdownStrongPattern.ReplaceAllString(escaped, "<strong>$1</strong>")
	escaped = markdownEmPattern.ReplaceAllString(escaped, "<em>$1</em>")
	return escaped
}

func renderMarkdownBody(source string) string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	var body strings.Builder
	var paragraph []string
	listType := ""
	inCode := false
	var codeLines []string

	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		body.WriteString("<p>")
		body.WriteString(renderMarkdownInline(strings.Join(paragraph, " ")))
		body.WriteString("</p>\n")
		paragraph = nil
	}
	closeList := func() {
		if listType == "" {
			return
		}
		body.WriteString("</" + listType + ">\n")
		listType = ""
	}
	flushCode := func() {
		if !inCode {
			return
		}
		body.WriteString("<pre><code>")
		body.WriteString(stdhtml.EscapeString(strings.Join(codeLines, "\n")))
		body.WriteString("</code></pre>\n")
		inCode = false
		codeLines = nil
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "\x60\x60\x60") {
			flushParagraph()
			closeList()
			if inCode {
				flushCode()
			} else {
				inCode = true
				codeLines = nil
			}
			continue
		}
		if inCode {
			codeLines = append(codeLines, raw)
			continue
		}

		if trimmed == "" {
			flushParagraph()
			closeList()
			continue
		}
		if match := markdownHeadingPattern.FindStringSubmatch(trimmed); len(match) == 3 {
			flushParagraph()
			closeList()
			level := len(match[1])
			fmt.Fprintf(&body, "<h%d>%s</h%d>\n", level, renderMarkdownInline(match[2]), level)
			continue
		}
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			flushParagraph()
			closeList()
			body.WriteString("<hr>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "> ") {
			flushParagraph()
			closeList()
			body.WriteString("<blockquote><p>")
			body.WriteString(renderMarkdownInline(strings.TrimPrefix(trimmed, "> ")))
			body.WriteString("</p></blockquote>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
			flushParagraph()
			if listType != "ul" {
				closeList()
				listType = "ul"
				body.WriteString("<ul>\n")
			}
			body.WriteString("<li>")
			body.WriteString(renderMarkdownInline(strings.TrimSpace(trimmed[2:])))
			body.WriteString("</li>\n")
			continue
		}
		if match := markdownOrderedPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			flushParagraph()
			if listType != "ol" {
				closeList()
				listType = "ol"
				body.WriteString("<ol>\n")
			}
			body.WriteString("<li>")
			body.WriteString(renderMarkdownInline(match[1]))
			body.WriteString("</li>\n")
			continue
		}
		paragraph = append(paragraph, trimmed)
	}
	flushParagraph()
	closeList()
	if inCode {
		flushCode()
	}
	return body.String()
}

func markdownPreviewDocument(entry, source string) string {
	title := filepath.Base(entry)
	base := filepath.ToSlash(filepath.Dir(entry))
	baseHref := "/"
	if base != "." && base != "" {
		baseHref = "/" + strings.Trim(base, "/") + "/"
	}
	return "<!doctype html><html><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
		"<base href=\"" + stdhtml.EscapeString(baseHref) + "\">" +
		"<title>" + stdhtml.EscapeString(title) + "</title>" +
		"<style>color-scheme:light dark;:root{font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,\"Segoe UI\",sans-serif}body{max-width:900px;margin:0 auto;padding:32px 38px;line-height:1.65;background:Canvas;color:CanvasText}h1,h2,h3,h4,h5,h6{line-height:1.25;margin:1.4em 0 .55em}h1{font-size:2em;border-bottom:1px solid color-mix(in srgb,CanvasText 18%,transparent);padding-bottom:.3em}h2{font-size:1.5em;border-bottom:1px solid color-mix(in srgb,CanvasText 12%,transparent);padding-bottom:.25em}p{margin:.8em 0}pre{overflow:auto;padding:16px;border-radius:8px;background:color-mix(in srgb,CanvasText 8%,Canvas)}code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;background:color-mix(in srgb,CanvasText 8%,Canvas);padding:.12em .3em;border-radius:4px}pre code{background:transparent;padding:0}blockquote{margin:1em 0;padding:.2em 1em;border-left:4px solid color-mix(in srgb,CanvasText 28%,transparent);color:color-mix(in srgb,CanvasText 75%,transparent)}img{max-width:100%;height:auto}hr{border:0;border-top:1px solid color-mix(in srgb,CanvasText 18%,transparent);margin:2em 0}</style>" +
		"</head><body>" + renderMarkdownBody(source) + "</body></html>"
}

func serveMarkdownPreview(w http.ResponseWriter, r *http.Request, project string) bool {
	if r.URL.Path != "/.tl-preview/markdown" {
		return false
	}
	entry := strings.TrimSpace(r.URL.Query().Get("file"))
	descriptor, ok := validPreviewEntry(project, entry)
	if !ok || descriptor.Renderer != "markdown" {
		http.NotFound(w, r)
		return true
	}
	target, _, err := resolveProjectEntry(project, descriptor.Path)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	data, err := os.ReadFile(target)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(markdownPreviewDocument(descriptor.Path, string(data))))
	return true
}
