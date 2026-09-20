package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestTLStudioBrandAssets(t *testing.T) {
	mark, err := webFS.ReadFile("web/tl-studio-mark.svg")
	if err != nil {
		t.Fatalf("read embedded TL Studio mark: %v", err)
	}
	if !bytes.Contains(mark, []byte("<svg")) || !bytes.Contains(mark, []byte("#212E4E")) || !bytes.Contains(mark, []byte("#008036")) {
		t.Fatal("embedded TL Studio mark is missing expected SVG structure or brand colors")
	}

	index, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	indexText := string(index)
	if strings.Contains(indexText, "tunnellab-mark.png") {
		t.Fatal("index still references the legacy TunnelLab mark")
	}
	if got := strings.Count(indexText, "/tl-studio-mark.svg"); got < 4 {
		t.Fatalf("expected TL Studio mark to be used for favicon and product surfaces, got %d references", got)
	}

	for _, path := range []string{"../../README.md", "../../README.fa_IR.md"} {
		readme, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(readme)
		if !strings.Contains(text, "./media/tl-studio-logo.svg") {
			t.Fatalf("%s does not reference the canonical TL Studio SVG logo", path)
		}
		if strings.Contains(text, "./media/tunnellab-logo.jpg") {
			t.Fatalf("%s still references the legacy TunnelLab logo", path)
		}
	}
}
