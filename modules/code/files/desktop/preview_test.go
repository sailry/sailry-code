package desktop_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
)

func TestFilesWorkspacePreviewsDocumentsAndOpensBinary(t *testing.T) {
	w := openFiles(t)
	folder := t.TempDir()
	const notebook = `{"nbformat":4,"nbformat_minor":5,"metadata":{},"cells":[{"id":"note","cell_type":"markdown","metadata":{},"source":["Notebook preview sentinel"]}]}`
	const page = `<main><h1>HTML preview sentinel</h1><p>Rendered, not source.</p></main>`
	binary := []byte{0, 0xff, 'P', 'K', 9}
	word := zipDocument(t, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Word preview sentinel</w:t></w:r></w:p></w:body></w:document>`,
	})
	for name, data := range map[string][]byte{
		"report.docx": word, "report.pdf": previewPDF(), "report.html": []byte(page), "report.ipynb": []byte(notebook),
		"opaque.go": binary,
	} {
		if err := os.WriteFile(filepath.Join(folder, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	local := w.Owner.ID()
	var opened []string
	var revealed []string
	openLocal := func(path string) { opened = append(opened, path) }
	revealLocal := func(path string) { revealed = append(revealed, path) }
	w.SetNativeFileActions(fixtures.NativeFileActions{LocalNode: local, OpenLocal: openLocal, RevealLocal: revealLocal})
	project := w.CreateProject("Previews", folder)
	session := w.OpenProjectSession(project.ID)
	w.App.Navigate(native.FilesPage)
	row := func(name string) { w.Click(w.access(ui.RoleTreeItem, name)) }
	open := func(name, text string) {
		t.Helper()
		row(name)
		w.Await("preview did not render "+name, func() bool { return w.Tester.HasText(text) })
		if _, editable := w.Tester.Access("Code editor"); editable || w.App.PanelOpen {
			t.Fatalf("%s opened as a text editor or left Files: %q", name, w.Tester.Texts())
		}
	}
	open("report.docx", "Word preview sentinel")
	row("report.pdf")
	w.access(ui.RoleImage, "Page 1")
	open("report.html", "HTML preview sentinel")
	open("report.ipynb", "Notebook preview sentinel")
	row("opaque.go")
	w.Await("binary did not open through the controller's OS service", func() bool { return len(opened) == 1 })
	// Simulate a controller on a different machine without changing the
	// Node's bytes or letting an OS callback inspect a worktree path.
	w.SetNativeFileActions(fixtures.NativeFileActions{OpenLocal: openLocal, RevealLocal: revealLocal})
	row("opaque.go")
	w.Await("remote binary did not open a controller copy", func() bool { return len(opened) == 2 })
	for _, path := range opened {
		if filepath.Clean(path) == filepath.Join(folder, "opaque.go") || filepath.Base(path) != "opaque.go" {
			t.Fatalf("controller opened the worktree path instead of its captured bytes: %q", path)
		}
		if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, binary) {
			t.Fatalf("external open received %v, %v", data, err)
		}
	}
	w.RevealWorkspaceFile(session.Worktree, "opaque.go")
	w.Await("remote reveal did not report that the source is unavailable locally", func() bool {
		return w.Tester.HasText(support.Label("files_download_failed"))
	})
	if len(revealed) != 0 {
		t.Fatalf("the OS was handed a remote worktree path: %q", revealed)
	}
	if w.App.PanelOpen || w.App.Page != native.FilesPage || !w.Tester.HasText("Notebook preview sentinel") {
		t.Fatal("external open displaced the existing document")
	}

	workspace := w.App.Hooks.WorkspaceProject()
	workspace.Actions.OpenManagedFile("report.html", 1)
	w.Await("a target line could not open the HTML source", func() bool { return w.editorContains(page) })
	editor, _ := w.Tester.Access("Code editor")
	w.Tester.ClickAt(editor.Bounds.X+60, editor.Bounds.Y+8)
	w.Await("HTML source did not take focus", func() bool { return w.Tester.Focused("Code editor") })
	w.Tester.Type("X")
	w.Await("HTML source did not retain its draft", func() bool {
		draft, dirty := workspace.FileDraft("report.html")
		return dirty && strings.Replace(draft, "X", "", 1) == page
	})
	open("report.docx", "Word preview sentinel")
	row("report.html")
	w.Await("returning to HTML replaced the dirty source with a preview", func() bool {
		draft, dirty := workspace.FileDraft("report.html")
		return dirty && w.editorContains(draft)
	})
}

func zipDocument(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	for name, part := range parts {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func previewPDF() []byte {
	const drawing = "0 0 1 rg\n30 20 160 90 re f\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 240 120] /Resources << >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(drawing), drawing),
	}
	var data bytes.Buffer
	data.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, value := range objects {
		offsets[i] = data.Len()
		fmt.Fprintf(&data, "%d 0 obj\n%s\nendobj\n", i+1, value)
	}
	xref := data.Len()
	fmt.Fprintf(&data, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&data, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&data, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return data.Bytes()
}
