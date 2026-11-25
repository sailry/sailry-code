package desktop

import (
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
)

func renderTree(project *native.Project, c *ui.Context, _ bool, open func(string)) {
	tree := treeFor(project)
	box := ui.Column(c.Key(project)).Grow(1).MinHeight(0)
	ui.Local(box, "mounted", func() bool { tree.mount(); return true })
	box.OnUnmount(tree.unmount).Children(func() {
		tree.update()
		if project.Actions != nil {
			tree.toolbar(c)
			if tree.searchOpen {
				tree.renderSearch(c)
				return
			}
			open = func(path string) { project.Actions.OpenManagedFile(path, 0) }
		}
		tree.draw(c, open)
	})
}

func (t *fileTree) toolbar(c *ui.Context) {
	p := theme.Of(c)
	ui.Row(c.Key("file-header")).Height(40).Shrink(0).PaddingX(8).Gap(4).BorderWidth(0, 0, 1, 0).BorderColor(p.Border).Children(func() {
		ui.Text(c, i18n.T(c, "file_tree")).FontSize(14).SingleLine().Grow(1).Shrink(1)
		controls.MenuButton(c.Key("create"), "file-create-"+t.project.ID, func(b ui.Element) {
			controls.IconLook(c, b, icons.Plus, i18n.T(c, "files_create"), "", 28, false)
		}, func(m *controls.Menu) { t.createMenu(c, m) }, controls.MenuOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
		controls.IconButton(c.Key("search"), icons.FileSearch, i18n.T(c, "file_search"), 28, t.searchOpen).OnClick(t.toggleSearch)
	})
}

func (t *fileTree) createMenu(c *ui.Context, m *controls.Menu) {
	project := t.project
	directory := t.directory(t.sel.current)
	for _, key := range []string{"files_new_file", "files_new_directory"} {
		if m.Item(i18n.T(c, key)).Chosen() {
			t.action(c, native.FileAction{Kind: key, Directory: directory})
		}
	}
	if m.Item(i18n.T(c, "files_upload")).Disabled(project.Actions.Upload == nil).Chosen() {
		project.Actions.Upload(directory)
	}
	if m.Item(i18n.T(c, "files_paste")).Disabled(!t.pasteReady(c)).Chosen() {
		t.action(c, native.FileAction{Kind: "files_paste", Directory: directory})
	}
	if project.Actions.Pending != nil {
		pending := project.Actions.Pending()
		if len(pending) > 0 {
			m.Separator()
		}
		for _, entry := range pending {
			if m.Item(i18n.T(c, "files_check_result")+" · "+entry.Label).Chosen() && entry.Run != nil {
				entry.Run(c)
			}
		}
	}
}

func (t *fileTree) treeMenu(c *ui.Context, m *ui.Menu, selected treeSelection) {
	item := func(key string) {
		if m.Item(i18n.T(c, key)).Chosen() {
			t.action(c, native.FileAction{Kind: key, Paths: selected.Paths, Directory: selected.Directory})
		}
	}
	live := t.project.Actions != nil
	paths := selected.Paths
	if len(paths) > 0 {
		if live {
			item("files_open_system")
			m.Separator()
		}
		item("files_copy")
		item("files_cut")
	}
	if m.Item(i18n.T(c, "files_paste")).Disabled(!t.pasteReady(c)).Chosen() {
		t.action(c, native.FileAction{Kind: "files_paste", Directory: selected.Directory})
	}
	if len(paths) > 0 {
		if m.Item(i18n.T(c, "files_copy_path")).Chosen() {
			c.WriteClipboard(strings.Join(paths, "\n"))
		}
		if live {
			item("files_insert_conversation")
		}
		m.Separator()
		if len(paths) == 1 {
			if live {
				reveal := "files_reveal_manager"
				switch runtime.GOOS {
				case "darwin":
					reveal = "files_reveal_finder"
				case "windows":
					reveal = "files_reveal_explorer"
				}
				item(reveal)
			}
			item("files_rename")
			if live && selected.File {
				item("files_download")
			}
		}
		item("files_trash")
		m.Separator()
	}
	item("files_new_file")
	item("files_new_directory")
}

func (t *fileTree) treeKeys(c *ui.Context, root ui.Element) {
	extend := func(step int) {
		if len(t.visible) == 0 {
			return
		}
		i := max(slices.Index(t.visible, t.sel.current), 0)
		next := t.visible[min(max(i+step, 0), len(t.visible)-1)]
		t.sel.pick(t.visible, next, true, false)
		t.focus = &next
	}
	switch {
	case root.Shortcut(ui.Shift, ui.KeyDown):
		extend(1)
		return
	case root.Shortcut(ui.Shift, ui.KeyUp):
		extend(-1)
		return
	case root.Shortcut(ui.Cmd, ui.KeyA):
		t.sel.all(t.visible)
		return
	}

	var kind string
	switch {
	case root.Shortcut(0, ui.KeyF2):
		kind = "files_rename"
	case root.Shortcut(ui.Cmd, ui.KeyC):
		kind = "files_copy"
	case root.Shortcut(ui.Cmd, ui.KeyX):
		kind = "files_cut"
	case root.Shortcut(ui.Cmd, ui.KeyV):
		kind = "files_paste"
	case root.Shortcut(0, ui.KeyDelete):
		kind = "files_trash"
	default:
		return
	}
	selected := t.selection(t.sel.current)
	if kind == "files_paste" {
		if t.pasteReady(c) {
			t.action(c, native.FileAction{Kind: kind, Directory: selected.Directory})
		}
	} else if len(selected.Paths) > 0 && (kind != "files_rename" || len(selected.Paths) == 1) {
		t.action(c, native.FileAction{Kind: kind, Paths: selected.Paths, Directory: selected.Directory})
	}
}
