package main

import (
	"os/exec"
	"runtime"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcceptanceSelectedTextPreviewDoesNotFallBackToIndexHTML(t *testing.T) {
	project := t.TempDir()
	for name, body := range map[string]string{
		"index.html": "<h1>unrelated home page</h1>",
		"tl": "tlstudio is here <script>alert(1)</script>",
		"site.css": "body { color: red; }",
		"script.js": "document.title = '<b>code</b>'",
		"notes.txt": "plain text",
	} {
		if err := os.WriteFile(filepath.Join(project, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"tl", "site.css", "script.js", "notes.txt"} {
		selected := detectPreviewCapability(project, name)
		if selected.Kind != "file" || selected.Entry != name || selected.EntryMeta == nil || selected.EntryMeta.Renderer != "text" {
			t.Fatalf("selection %q fell back to another file: %#v", name, selected)
		}
		url := previewEntryURL("http://127.0.0.1:1234/", *selected.EntryMeta)
		if !strings.Contains(url, ".tl-preview/text?file=") || !strings.Contains(url, name) {
			t.Fatalf("selected source did not use escaped text renderer: %q", url)
		}
	}
	defaultCap := detectPreviewCapability(project, "")
	if defaultCap.Entry != "index.html" { t.Fatalf("no-selection default unexpectedly changed: %#v", defaultCap) }
	server := httptest.NewServer(safeStaticPreviewHandler(project))
	defer server.Close()
	resp, err := http.Get(server.URL + "/.tl-preview/text?file=tl")
	if err != nil { t.Fatal(err) }
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != 200 || !strings.Contains(string(body), "tlstudio is here") ||
		strings.Contains(string(body), "<script>alert(1)</script>") ||
		!strings.Contains(string(body), "&lt;script&gt;") ||
		strings.Contains(string(body), "unrelated home page") {
		t.Fatalf("preview leaked another file or failed to escape text: status=%d body=%q", resp.StatusCode, body)
	}
}

func TestAcceptanceExtensionlessBinaryCannotBePreviewed(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project,"index.html"),[]byte("<h1>valid</h1>"),0600);err!=nil{t.Fatal(err)}
	if err := os.WriteFile(filepath.Join(project,"binary"),[]byte{0,1,2,3},0600);err!=nil{t.Fatal(err)}
	if _,ok := validPreviewEntry(project,"binary");ok{t.Fatal("binary file misclassified as text")}
	if _,ok := validPreviewEntry(project,"../outside");ok{t.Fatal("preview escaped project")}
	server:=httptest.NewServer(safeStaticPreviewHandler(project))
	defer server.Close()
	res,err:=http.Get(server.URL+"/.tl-preview/text?file=binary")
	if err!=nil{t.Fatal(err)}
	defer res.Body.Close()
	if res.StatusCode!=http.StatusNotFound{t.Fatalf("binary preview must return 404, got %d",res.StatusCode)}
}

func TestAcceptanceExplorerAndProjectSwitchContracts(t *testing.T) {
	src:=readBrowserSource(t,"files.ts")
	for _,contract:=range []string{
		"directoryRequestEpoch", "const current = () => epoch === directoryRequestEpoch",
		"if (!current()) return;", "directoryRequestEpoch++;", "await openFiles()",
	} {
		if !strings.Contains(src,contract){t.Fatalf("Explorer missing %q",contract)}
	}
	workbench:=readBrowserSource(t,"workbench.ts")
	for _,contract:=range []string{"workspaceOpenFolder", "Open Folder", "pickProject.click()"}{
		if !strings.Contains(workbench,contract){t.Fatalf("project-opening UI missing %q",contract)}
	}
}

func TestAcceptancePreviewAlwaysTracksActiveTabContract(t *testing.T) {
	src:=readBrowserSource(t,"preview.ts")
	for _,contract:=range []string{
		"selectedEditorPath", "activePreviewEntry() || ui.entry?.value",
		"showUnpreviewableSelection", "K.state.preview.entrySwitchGeneration",
		"tl-studio:editor-render",
		"snapshot.entry !== active", "ui.frame.classList.add(\"hidden\")",
	} {
		if !strings.Contains(src,contract){t.Fatalf("active Preview missing %q",contract)}
	}
}

func TestAcceptanceExplorerPreviewEditorAsyncRegressions(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	command := exec.Command("node", filepath.Join(root, "scripts", "check-explorer-preview-feedback.cjs"))
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Explorer, Preview and Editor async regressions failed: %v\n%s", err, output)
	}
}
