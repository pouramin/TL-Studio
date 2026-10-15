package main

import (
	"errors"
	"io"
	"net/url"
	"strings"
)

type boundedModelStream struct {
	reader io.Reader
	read   int64
}

func (r *boundedModelStream) Read(p []byte) (int, error) {
	remaining := int64(nativeModelMaxResponseBytes) - r.read
	if remaining <= 0 {
		return 0, errors.New("model stream exceeded response byte limit")
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}
func nativeModelOutputLimit(model tlProviderModel) int {
	if model.OutputLimit > 0 {
		return model.OutputLimit
	}
	return 8192
}

func nativeChatOutputParameter(provider tlProviderDefinition) string {
	endpoint, err := url.Parse(provider.BaseURL)
	if err == nil && strings.EqualFold(endpoint.Hostname(), "api.openai.com") {
		return "max_completion_tokens"
	}
	// Most OpenAI-compatible providers, including Mistral, use max_tokens.
	return "max_tokens"
}
