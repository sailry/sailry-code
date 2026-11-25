package desktop

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func exampleTree() *fixtures.Workspace {
	file := func(name, path string) *native.FileNode { return &native.FileNode{Name: name, Path: path} }
	dir := func(name, path string, children ...*native.FileNode) *native.FileNode {
		return &native.FileNode{Name: name, Path: path, Children: children}
	}
	return &fixtures.Workspace{Files: dir("fixture", "fixture",
		dir("docs", "fixture/docs", file("example.md", "fixture/docs/example.md")),
		dir("internal", "fixture/internal", dir("kit", "fixture/internal/kit", file("floating.go", "fixture/internal/kit/floating.go")), dir("shell", "fixture/internal/shell", file("shell.go", "fixture/internal/shell/shell.go"))),
		file("go.mod", "fixture/go.mod"), file("README.md", "fixture/README.md")),
	}
}

func TestFilesTreeSelectionAndKeys(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		t.Run(map[bool]string{false: "selection", true: "keyboard"}[keyboard], func(t *testing.T) {
			project := fixtures.NewProject("fixture", exampleTree(), nil)
			tree := treeFor(project)
			window := ui.NewTesterWith(func(c *ui.Context) { theme.Apply(c, "", theme.Light); renderTree(project, c, true, project.Files.Show) }, 480, 680, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true, ReduceTransparency: true}})
			window.SetLocale("en")
			window.Frame()
			row := func(name string) ui.AccessInfo {
				t.Helper()
				role, ok := window.AccessByRole(ui.RoleTreeItem, name)
				if !ok || role.Bounds.H == 0 {
					t.Fatalf("tree row %q is missing: %q", name, window.Texts())
				}
				return role
			}
			click := func(name string, mods ui.Modifiers) {
				t.Helper()
				b := row(name).Bounds
				window.ClickAtWith(mods, b.X+b.W/2, b.Y+b.H/2)
				window.Frame()
				window.Advance(300 * time.Millisecond)
				window.Frame()
			}
			if keyboard {
				click("docs", 0)
				if *tree.folders["fixture/docs"] {
					t.Fatal("click did not close the folder")
				}
				window.Key(0, ui.KeyRight)
				window.Frame()
				if !*tree.folders["fixture/docs"] {
					t.Fatal("Right did not open the folder")
				}
				window.Key(0, ui.KeyDown)
				window.Frame()
				window.Frame()
				if !row("example.md").Selected {
					t.Fatal("Down did not select the folder's file")
				}
				window.Key(0, ui.KeyEnter)
				window.Frame()
				if project.Files.File() != "fixture/docs/example.md" {
					t.Fatalf("Enter showed %q", project.Files.File())
				}
				return
			}
			selected := func() []string { return tree.sel.paths(tree.visible) }
			click("go.mod", 0)
			click("example.md", ui.Cmd)
			if got := selected(); !slices.Equal(got, []string{"fixture/docs/example.md", "fixture/go.mod"}) || project.Files.File() != "fixture/go.mod" {
				t.Fatalf("modifier click selected %q and opened %q", got, project.Files.File())
			}
			click("kit", ui.Shift)
			if got := selected(); !slices.Equal(got, []string{"fixture/docs/example.md", "fixture/internal", "fixture/internal/kit"}) {
				t.Fatalf("Shift selected %q", got)
			}
			saveTreeShot(t, window)
			window.Key(ui.Shift, ui.KeyDown)
			window.Frame()
			if got := selected(); !slices.Contains(got, "fixture/internal/kit/floating.go") || tree.sel.current != "fixture/internal/kit/floating.go" {
				t.Fatalf("Shift+Down selected %q: %q", got, tree.sel.current)
			}
			b := row("kit").Bounds
			window.RightClickAt(b.X+b.W/2, b.Y+b.H/2)
			if menu := window.Menu(); slices.Contains(menu, i18n.In("en", "files_rename")) || !slices.Contains(menu, i18n.In("en", "files_trash")) {
				t.Fatalf("multi-selection menu: %q", menu)
			}
			if err := window.ChooseMenuItem(i18n.In("en", "files_copy_path")); err != nil {
				t.Fatal(err)
			}
			window.Frame()
			if copied := window.Clipboard(); copied != "fixture/docs/example.md\nfixture/internal" {
				t.Fatalf("selected action targets %q", copied)
			}
			click("go.mod", 0)
			window.Key(ui.Cmd, ui.KeyA)
			window.Frame()
			if len(selected()) != len(tree.visible) {
				t.Fatalf("Cmd+A selected %d of %d", len(selected()), len(tree.visible))
			}
		})
	}
}

func TestFilesSampleActions(t *testing.T) {
	data := exampleTree()
	project := fixtures.NewProject("fixture", data, nil)
	tree := treeFor(project)
	const docs = "fixture/docs"
	name := func(name string) bool { return findSample(data.Files, name) != nil }
	apply := func(action native.FileAction) {
		t.Helper()
		if !tree.sampleFile(action) {
			t.Fatalf("%s was not applied", action.Kind)
		}
	}
	caption := i18n.In("en", "settings_name")
	window := ui.NewTesterWith(func(c *ui.Context) { theme.Apply(c, "", theme.Light); project.ActionDialog(c) }, 480, 320, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true, ReduceTransparency: true}})
	window.SetLocale("en")
	submit := func(value string) {
		t.Helper()
		window.Frame()
		if err := window.Click(caption); err != nil {
			t.Fatal(err)
		}
		window.Key(ui.Cmd, ui.KeyA)
		window.Type(value)
		window.Key(0, ui.KeyEnter)
		window.Frame()
	}
	apply(native.FileAction{Kind: "files_new_file", Directory: docs})
	submit("not/a/name")
	if _, open := window.AccessByRole(ui.RoleTextField, caption); !open {
		t.Fatal("invalid input closed the form")
	}
	submit("limits.md")
	if !name(docs + "/limits.md") {
		t.Fatal("new file did not appear")
	}
	apply(native.FileAction{Kind: "files_new_directory", Directory: docs})
	submit("adr")
	if folder := findSample(data.Files, docs+"/adr"); folder == nil || folder.Children == nil || findSample(data.Files, docs).Children[0] != folder {
		t.Fatal("new directory was not sorted ahead of files")
	}
	apply(native.FileAction{Kind: "files_rename", Paths: []string{docs + "/limits.md"}})
	submit("quotas.md")
	if name(docs+"/limits.md") || !name(docs+"/quotas.md") {
		t.Fatal("rename did not replace the old entry")
	}
	apply(native.FileAction{Kind: "files_move", Directory: docs + "/adr", Paths: []string{docs + "/quotas.md"}})
	if !name(docs+"/adr/quotas.md") || name(docs+"/quotas.md") {
		t.Fatal("move did not update the directory")
	}
	apply(native.FileAction{Kind: "files_copy", Paths: []string{docs + "/example.md"}})
	apply(native.FileAction{Kind: "files_paste", Directory: docs + "/adr"})
	if !name(docs+"/adr/example.md") || !name(docs+"/example.md") {
		t.Fatal("copy and paste did not preserve the source")
	}
	apply(native.FileAction{Kind: "files_cut", Paths: []string{docs + "/example.md"}})
	apply(native.FileAction{Kind: "files_paste", Directory: "fixture"})
	if !name("fixture/example.md") || name(docs+"/example.md") {
		t.Fatal("cut and paste did not relocate the file")
	}
	apply(native.FileAction{Kind: "files_trash", Paths: []string{docs + "/adr"}})
	if name(docs+"/adr") || name(docs+"/adr/quotas.md") {
		t.Fatal("trash left the directory")
	}
	if tree.sampleFile(native.FileAction{Kind: "files_open_system", Paths: []string{docs}}) {
		t.Fatal("preview invoked the system")
	}
}

func TestFilesTreeCoalescesRefreshWhileReading(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{{name: "stale result"}, {name: "canceled result", err: context.Canceled}} {
		t.Run(test.name, func(t *testing.T) {
			project := fixtures.NewProject("fixture", &fixtures.Workspace{}, &fixtures.Actions{})
			var revision uint64
			var reads []func(*ui.Context, protocol.Directory, error)
			project.FileDirectory = native.FileDirectoryHost{
				Name: "fixture", Context: t.Context(), Revision: func() uint64 { return revision },
				Observe: func(context.Context) func() { return func() {} },
				Read: func(_ context.Context, name string, after *protocol.DirectoryCursor, done func(*ui.Context, protocol.Directory, error)) {
					if name != "" || after != nil {
						t.Fatalf("unexpected directory request: %q, cursor %+v", name, after)
					}
					reads = append(reads, done)
				},
			}
			tree := treeFor(project)
			tree.mount()
			defer tree.unmount()
			page := tree.ensurePage("")
			if len(reads) != 1 {
				t.Fatalf("initial expansion sent %d reads, want one", len(reads))
			}
			for range 24 {
				revision++
				tree.update()
				tree.ensurePage("")
				tree.refresh("")
			}
			if len(reads) != 1 || !page.refreshPending {
				t.Fatalf("invalidation during an in-flight read sent %d requests, pending=%t", len(reads), page.refreshPending)
			}
			reads[0](nil, protocol.Directory{Entries: []protocol.FileEntry{{Name: "stale.txt", Kind: protocol.EntryKindFile}}}, test.err)
			if len(reads) != 2 || page.loaded || page.refreshPending {
				t.Fatalf("settlement did not start exactly one follow-up: reads=%d, loaded=%t, pending=%t", len(reads), page.loaded, page.refreshPending)
			}
			reads[1](nil, protocol.Directory{Entries: []protocol.FileEntry{{Name: "current.txt", Kind: protocol.EntryKindFile}}}, nil)
			for range 24 {
				tree.update()
				tree.ensurePage("")
			}
			if len(reads) != 2 || !page.loaded || len(page.entries) != 1 || page.entries[0].name != "current.txt" {
				t.Fatalf("idle updates re-read the directory or published stale entries: reads=%d, entries=%+v", len(reads), page.entries)
			}
		})
	}
}

func BenchmarkFilesTree(b *testing.B) {
	for _, cycle := range []bool{false, true} {
		b.Run(map[bool]string{false: "unchanged", true: "open-close"}[cycle], func(b *testing.B) {
			project := fixtures.NewProject("fixture", &fixtures.Workspace{}, &fixtures.Actions{})
			var reads, owners int
			entries := make([]protocol.FileEntry, 128)
			for n := range entries {
				entries[n] = protocol.FileEntry{Name: fmt.Sprintf("entry-%03d.txt", n), Kind: protocol.EntryKindFile}
			}
			project.FileDirectory = native.FileDirectoryHost{Name: "fixture", Context: b.Context(), Revision: func() uint64 { return 0 }, Observe: func(context.Context) func() { owners++; return func() { owners-- } }, Read: func(_ context.Context, name string, _ *protocol.DirectoryCursor, done func(*ui.Context, protocol.Directory, error)) {
				reads++
				done(nil, protocol.Directory{Path: name, Entries: entries}, nil)
			}}
			open := true
			window := ui.NewTesterWith(func(c *ui.Context) {
				theme.Apply(c, "", theme.Light)
				if open {
					renderTree(project, c, true, project.Files.Show)
				}
			}, 520, 680, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true, ReduceTransparency: true}})
			window.SetLocale("en")
			window.SetScale(0.125)
			window.Frame()
			window.Frame()
			tree := treeFor(project)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if cycle {
					open = false
					window.Frame()
					open = true
				}
				window.Frame()
			}
			b.StopTimer()
			if !cycle && reads != 1 {
				b.Fatalf("unchanged frames sent %d directory reads", reads)
			}
			open = false
			window.Frame()
			if owners != 0 || tree.mounts != 0 || len(tree.pages) != 0 {
				b.Fatalf("closed tree kept owners: observers=%d mounts=%d pages=%d", owners, tree.mounts, len(tree.pages))
			}
			runtime.GC()
			runtime.ReadMemStats(&after)
			b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "live-delta-B")
			runtime.KeepAlive(window)
		})
	}
}

func saveTreeShot(t *testing.T, tester *ui.Tester) {
	t.Helper()
	if dir := os.Getenv("SAILRY_SNAPSHOTS"); dir != "" {
		file, err := os.Create(filepath.Join(dir, "files-multiselect.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, tester.Image()); err != nil {
			t.Fatal(err)
		}
	}
}
