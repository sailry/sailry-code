package desktop

import (
	"path"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
)

type treeSelection struct {
	Paths     []string
	Directory string
	File      bool
}

type fileDrag struct{ path string }

func (t *fileTree) rebuild() {
	if !t.dirty {
		return
	}
	t.dirty = false
	clear(t.entries)
	if t.entries == nil {
		t.entries = map[string]treeEntry{}
	}
	t.visible = t.visible[:0]
	if t.host.Read != nil {
		clear(t.drafts)
		for _, draft := range t.project.Unsaved() {
			if t.drafts == nil {
				t.drafts = map[string][]treeEntry{}
			}
			for name, folder := draft, false; name != ""; name, folder = parentDir(name), true {
				parent := parentDir(name)
				if !slices.ContainsFunc(t.drafts[parent], func(entry treeEntry) bool { return entry.path == name }) {
					t.drafts[parent] = append(t.drafts[parent], treeEntry{name, path.Base(name), folder})
				}
			}
		}
	}
	var walk func(treeEntry, *native.FileNode)
	walk = func(entry treeEntry, sample *native.FileNode) {
		t.visible = append(t.visible, entry.path)
		t.entries[entry.path] = entry
		if entry.directory && *t.folder(entry.path) {
			t.children(entry, sample, walk)
		}
	}
	walk(t.root(), t.project.Data.Files)
	if t.reveal != nil {
		if _, present := t.entries[*t.reveal]; present {
			t.sel.pick(t.visible, *t.reveal, false, false)
			t.reveal = nil
		}
	}
}

func (t *fileTree) children(entry treeEntry, sample *native.FileNode, each func(treeEntry, *native.FileNode)) {
	if t.host.Read == nil {
		if sample != nil {
			for _, child := range sample.Children {
				each(sampleEntry(child), child)
			}
		}
		return
	}
	var children []treeEntry
	if page := t.pages[entry.path]; page != nil {
		if page.denied {
			return
		}
		children = page.entries
	}
	for _, child := range children {
		each(child, nil)
	}
	for _, draft := range t.drafts[entry.path] {
		if !slices.ContainsFunc(children, func(child treeEntry) bool { return child.path == draft.path }) {
			each(draft, nil)
		}
	}
}

func (t *fileTree) directory(name string) string {
	if entry, exists := t.entries[name]; exists && entry.directory {
		return entry.path
	}
	return parentDir(name)
}

func (t *fileTree) selection(path string) treeSelection {
	targets := t.sel.targets(t.visible, path)
	file := false
	if len(targets) == 1 {
		entry, exists := t.entries[targets[0]]
		file = exists && !entry.directory
	}
	return treeSelection{targets, t.directory(path), file}
}

func (t *fileTree) draw(c *ui.Context, open func(string)) {
	t.rebuild()
	root := t.root()
	if state := t.ensurePage(root.path); state != nil {
		if state.read != nil && !state.loaded {
			ui.Column(c.Key("files-loading")).Grow(1).Center().Children(func() { controls.FileTreeBusy(c) })
			return
		}
		if state.loaded && !state.failed && len(state.entries) == 0 && len(t.drafts[root.path]) == 0 {
			controls.Result(c, controls.ResultProps{Icon: native.FilesIcon, Title: i18n.T(c, "files_directory_empty")}).Grow(1).Justify(ui.Center)
			t.tail(c, root.path)
			return
		}
	}
	p := theme.Of(c)
	box := ui.Scroll(c.Key("tree")).Grow(1).Padding(8)
	scroll := ui.Local(box, "scroll", func() ui.ScrollState { return ui.ScrollState{} })
	box.TrackScroll(scroll).ContextMenu(func(m *ui.Menu) { t.treeMenu(c, m, t.selection("")) }).Children(func() {
		if root.name == "" {
			return
		}
		tree := ui.TreeBase(c)
		t.treeKeys(c, tree.Root)
		tree.Root.Children(func() {
			var draw func(treeEntry, *native.FileNode)
			draw = func(entry treeEntry, sample *native.FileNode) {
				c.Key(entry.path)
				var folder *bool
				if entry.directory {
					folder = t.folder(entry.path)
				}
				item := tree.Item(folder)
				row := item.Row.Height(28).PaddingStart(8 + 16*float32(item.Depth())).PaddingEnd(8).Gap(8).
					Radius(p.Radius).Cursor(ui.CursorPointer).Label(entry.name)
				if t.focus != nil && *t.focus == entry.path {
					row.Focus()
					t.focus = nil
				}
				if row.Clicked() {
					mods := row.ClickModifiers()
					shift, add := mods&ui.Shift != 0, mods&(ui.Super|ui.Ctrl) != 0
					t.sel.pick(t.visible, entry.path, shift, add)
					if !shift && !add {
						if folder != nil {
							*folder = !*folder
						} else {
							open(entry.path)
						}
					}
				} else if row.FocusVisible() && t.sel.current != entry.path {
					t.sel.pick(t.visible, entry.path, false, false)
				}
				row.ContextMenu(func(m *ui.Menu) { t.sel.context(t.visible, entry.path); t.treeMenu(c, m, t.selection(entry.path)) })
				if entry.path != root.path {
					row.Drag(fileDrag{entry.path})
				}
				dir := t.directory(entry.path)
				movable := func(d fileDrag) bool { return parentDir(d.path) != dir && !within(dir, d.path) }
				if d, ok := ui.Drop[fileDrag](row); ok && movable(d) {
					t.action(c, native.FileAction{Kind: "files_move", Paths: []string{d.path}, Directory: dir})
				}
				if d, ok := ui.DragOver[fileDrag](row); ok && movable(d) {
					row.Background(p.Hover)
				}
				text := p.MutedForeground
				if t.sel.selected[entry.path] {
					row.Background(p.Hover).AccessSelected(true)
					text = p.HoverForeground
				} else if row.Hovered() {
					text = p.Foreground
				}
				row.TextColor(text).Children(func() {
					icon := icons.FileTreeDocument
					if folder != nil {
						icon = native.FilesIcon
						if *folder {
							icon = icons.FileTreeFolderOpen
						}
					}
					ui.Icon(c, icon).FontSize(16).TextColor(p.MutedForeground)
					ui.Text(c, entry.name).FontSize(14).SingleLine().Shrink(1)
				})
				if folder != nil {
					item.Group(func() {
						t.ensurePage(entry.path)
						t.children(entry, sample, draw)
						t.tail(c, entry.path)
					})
					t.recordExpansion(entry.path, *folder)
				}
			}
			draw(root, t.project.Data.Files)
		})
	})
}

func (t *fileTree) tail(c *ui.Context, name string) {
	state := t.pages[name]
	if state == nil {
		return
	}
	if state.read != nil && !state.loaded {
		ui.Row(c.Key("loading")).Height(28).PaddingX(12).Children(func() { controls.FileTreeBusy(c) })
		return
	}
	button := func(key string, disabled bool, choose func()) {
		label := i18n.T(c, key)
		controls.Ghost(c.Key(key), label, false).Disabled(disabled).Height(28).PaddingX(12).
			Children(func() { ui.Text(c, label).FontSize(12) }).OnClick(choose)
	}
	if state.failed {
		button("refresh", false, func() { t.refresh(name) })
	}
	if state.next != nil {
		button("files_load_more", state.read != nil, func() { t.more(name) })
	}
	if state.partial {
		ui.Text(c.Key("partial"), i18n.T(c, "files_listing_partial")).FontSize(12).PaddingX(12).TextColor(theme.Of(c).MutedForeground)
	}
}

func (t *fileTree) recordExpansion(path string, open bool) {
	if t.expansions == nil {
		t.expansions = map[string]bool{}
	}
	if old, seen := t.expansions[path]; !seen || old != open {
		t.expansions[path] = open
		t.dirty = true
		if !open && path != "" {
			t.removePages(path)
		}
	}
}
