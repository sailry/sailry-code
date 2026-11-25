package desktop_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	files "github.com/sailry/sailry-code/modules/code/files/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

type directoryFixtureRead struct {
	path  string
	after *protocol.DirectoryCursor
	ctx   context.Context
	done  func(protocol.Directory, error)
}

func (r directoryFixtureRead) reply(entries []protocol.FileEntry, next *protocol.DirectoryCursor) {
	r.done(protocol.Directory{Path: r.path, Entries: entries, Next: next}, nil)
}

func TestFilesDirectoryWatchPreservesViewport(t *testing.T) {
	for _, surface := range []string{"workspace-page", "conversation-sidebar"} {
		t.Run(surface, func(t *testing.T) { testFilesDirectoryWatchPreservesViewport(t, surface == "conversation-sidebar") })
	}
}

func testFilesDirectoryWatchPreservesViewport(t *testing.T, sidebar bool) {
	const rootName = "Listing fixture"
	project := fixtures.NewProject("listing-worktree", &fixtures.Workspace{}, &fixtures.Actions{})
	var revision uint64
	var reads []directoryFixtureRead
	var deliveries []func(*ui.Context)
	project.FileDirectory = native.FileDirectoryHost{
		Name: rootName, Context: t.Context(), Revision: func() uint64 { return revision },
		Observe: func(context.Context) func() { return func() {} },
		Read: func(ctx context.Context, path string, after *protocol.DirectoryCursor, done func(*ui.Context, protocol.Directory, error)) {
			reads = append(reads, directoryFixtureRead{path, after, ctx, func(page protocol.Directory, err error) {
				deliveries = append(deliveries, func(c *ui.Context) { done(c, page, err) })
			}})
		},
		Fault: func(error) protocol.ErrorCode { return protocol.ErrorCodePermissionDenied },
	}
	app := fixtures.NewLive(&fixtures.Data{Workspace: &fixtures.Workspace{}}, native.Assemble(files.Register))
	app.Hooks.WorkspaceProject = func() *native.Project { return project }
	app.SetWelcome(true)
	app.Effects = false
	if sidebar {
		app.RevealConversationPanel()
		app.Panel.Show(native.PanelTab{Kind: native.Files})
	} else {
		app.Navigate(native.FilesPage)
		app.RevealDetails()
	}
	view := ui.NewTesterWith(func(c *ui.Context) {
		queued := deliveries
		deliveries = nil
		for _, deliver := range queued {
			deliver(c)
		}
		app.View(c)
	}, 1280, 820, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true, ReduceTransparency: true}})
	view.SetLocale("en")
	frame := func() { view.Frame(); view.Frame() }
	read := func(path string, after *protocol.DirectoryCursor) directoryFixtureRead {
		t.Helper()
		for i, got := range reads {
			if got.path != path {
				continue
			}
			if !reflect.DeepEqual(got.after, after) {
				t.Fatalf("read %q cursor = %+v, want %+v", path, got.after, after)
			}
			reads = append(reads[:i], reads[i+1:]...)
			return got
		}
		t.Fatalf("no read for %q after %+v; visible text: %q", path, after, view.Texts())
		return directoryFixtureRead{}
	}
	row := func(name string) ui.AccessInfo {
		t.Helper()
		info, ok := view.AccessByRole(ui.RoleTreeItem, name)
		if !ok {
			t.Fatalf("no tree row %q: %q", name, view.Texts())
		}
		return info
	}
	key := func(name string) string { return i18n.In("en", name) }
	hasProgress := func() bool {
		header, ok := view.Find(key("file_tree"))
		if !ok {
			t.Fatal("the Files tree is not shown")
		}
		for _, info := range view.AccessAll() {
			b := info.Bounds
			if info.Role == ui.RoleProgress && b.W > 0 && b.H > 0 && b.X >= header.X && b.X < header.X+header.W && b.Y >= header.Y+header.H {
				return true
			}
		}
		return false
	}
	invalidate := func() { revision++; frame() }

	frame()
	first := read("", nil)
	if !hasProgress() || view.HasText(key("files_directory_empty")) {
		t.Fatal("the first listing did not show its loading state")
	}
	first.reply([]protocol.FileEntry{}, nil)
	frame()
	invalidate()
	root := read("", nil)
	if !view.HasText(key("files_directory_empty")) || hasProgress() {
		t.Fatal("refresh replaced the already loaded empty directory")
	}
	rootEntries := []protocol.FileEntry{{Name: "plans", Kind: protocol.EntryKindDirectory}}
	root.reply(rootEntries, nil)
	frame()
	plans := row("plans").Bounds
	view.ClickAt(plans.X+plans.W/2, plans.Y+plans.H/2)
	frame()
	child := read("plans", nil)
	if !hasProgress() {
		t.Fatal("the newly expanded directory did not show its first loading row")
	}

	entries := make([]protocol.FileEntry, 32)
	for i := range entries {
		entries[i] = protocol.FileEntry{Name: fmt.Sprintf("plan-%02d.md", i), Kind: protocol.EntryKindFile}
	}
	page1 := &protocol.DirectoryCursor{Revision: "old", Name: entries[3].Name}
	child.reply(entries[:4], page1)
	frame()
	if err := view.Click(key("files_load_more")); err != nil {
		t.Fatal(err)
	}
	frame()
	const selected = "plan-31.md"
	page2 := &protocol.DirectoryCursor{Revision: "old", Name: selected}
	read("plans", page1).reply(entries[4:], page2)
	frame()
	if !row("plans").Expanded {
		t.Fatal("the fixture folder did not open")
	}
	if entry := row(selected); !entry.Offscreen && entry.Bounds.H > 0 {
		t.Fatal("the fixture must require scrolling to the last row")
	}
	view.Wheel(plans.X+plans.W/2, plans.Y+plans.H/2, 0, 2000)
	view.Advance(400 * time.Millisecond)
	frame()
	anchor := row(selected).Bounds
	view.ClickAtWith(ui.Cmd, anchor.X+anchor.W/2, anchor.Y+anchor.H/2)
	frame()
	before := row(selected)
	if !before.Selected || before.Offscreen || before.Bounds.H == 0 {
		t.Fatalf("the fixture did not select and scroll to the row: %+v", before)
	}
	assertStable := func(refreshing bool) {
		t.Helper()
		after := row(selected)
		if hasProgress() || !after.Selected || after.Offscreen || after.Bounds != before.Bounds {
			t.Fatalf("refresh changed the visible selection, scroll position or loading UI: before %+v, after %+v", before, after)
		}
		if refreshing {
			if more, ok := view.AccessByRole(ui.RoleButton, key("files_load_more")); !ok || !more.Disabled {
				t.Fatal("refresh replaced the existing pagination control or allowed another request")
			}
		}
	}

	invalidate()
	root, child = read("", nil), read("plans", nil)
	assertStable(true)
	root.reply(rootEntries, nil)
	fresh := &protocol.DirectoryCursor{Revision: "new", Name: entries[3].Name}
	child.reply(entries[:4], fresh)
	frame()
	child = read("plans", fresh)
	assertStable(true)
	const added = "session-created.md"
	child.reply(append(entries[4:], protocol.FileEntry{Name: added, Kind: protocol.EntryKindFile}), nil)
	frame()
	assertStable(false)
	if found := row(added); found.Offscreen || found.Bounds.H == 0 {
		t.Fatal("the file-change observation did not display the new entry")
	}

	invalidate()
	root, child = read("", nil), read("plans", nil)
	root.done(protocol.Directory{}, fmt.Errorf("fixture directory denied"))
	frame()
	if child.ctx.Err() == nil {
		t.Fatal("a forbidden parent did not cancel its pending child read")
	}
	child.reply(entries, nil)
	frame()
	for _, item := range view.AccessAll() {
		if item.Role == ui.RoleTreeItem && item.Name != rootName {
			t.Fatalf("a forbidden listing retained a tree row: %+v", item)
		}
	}
	if view.HasText(key("files_directory_empty")) || !view.HasText(key("directory_denied")) || !view.HasText(key("refresh")) {
		t.Fatalf("the forbidden listing was not reported with a retry: %q", view.Texts())
	}
	if len(reads) != 0 {
		t.Fatalf("unexpected follow-up directory reads: %+v", reads)
	}
}
