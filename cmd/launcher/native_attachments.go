package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const nativeAttachmentMaxBytes = 256 << 10
const nativeAttachmentTotalBytes = 1 << 20

// Native transports currently support text attachments. Never silently accept
// image/PDF bytes when no provider-neutral multimodal representation exists.
func nativeTextAttachments(parts []map[string]any) ([]sessionAttachmentView, error) {
	result := []sessionAttachmentView{}
	total := 0
	for _, part := range parts {
		if sessionString(part["type"]) != "file" {
			continue
		}
		if len(result) >= 8 {
			return nil, errors.New("at most 8 text attachments are supported")
		}
		mime := sessionString(part["mime"])
		if !strings.HasPrefix(mime, "text/") && mime != "application/json" {
			return nil, errors.New("only text attachments are supported; images and PDFs are not sent to this model")
		}
		raw := sessionString(part["url"])
		header, encoded, ok := strings.Cut(raw, ",")
		if !ok || header != "data:"+mime+";base64" {
			return nil, errors.New("text attachment must use a base64 data URL")
		}
		if len(encoded) > base64.StdEncoding.EncodedLen(nativeAttachmentMaxBytes) {
			return nil, errors.New("text attachment exceeds 256 KiB")
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if len(data) > nativeAttachmentMaxBytes {
			return nil, errors.New("text attachment exceeds 256 KiB")
		}
		if err != nil || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return nil, errors.New("text attachment must contain valid UTF-8 text")
		}
		total += len(data)
		if total > nativeAttachmentTotalBytes {
			return nil, errors.New("text attachments exceed 1 MiB total")
		}
		name := firstSessionString(part["filename"], part["name"])
		if len(name) > 300 {
			return nil, fmt.Errorf("attachment filename is too long")
		}
		result = append(result, sessionAttachmentView{Name: name, MIME: mime, Text: string(data)})
	}
	return result, nil
}
