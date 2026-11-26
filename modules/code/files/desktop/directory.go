package desktop

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

type treeEntry struct {
	path, name string
	directory  bool
}

type directoryPage struct {
	entries                                 []treeEntry
	next                                    *protocol.DirectoryCursor
	read                                    context.CancelFunc
	loaded, failed, partial, refreshPending bool
	denied                                  bool
}

type fileTree struct {
	project       *native.Project
	host          native.FileDirectoryHost
	ctx           context.Context
	detach        func()
	mounts        int
	revision      uint64
	pages         map[string]*directoryPage
	folders       map[string]*bool
	expansions    map[string]bool
	reveal, focus *string
	sel           selection
	visible       []string
	entries       map[string]treeEntry
	drafts        map[string][]treeEntry
	reported      string
	dirty         bool
	search        fileSearch
	searchOpen    bool
	clip          []string
	cut           bool
}

func treeFor(project *native.Project) *fileTree {
	return project.FileBrowserState(func() any {
		return &fileTree{project: project, host: project.FileDirectory, folders: map[string]*bool{}, sel: selection{selected: map[string]bool{}}, dirty: true}
	}).(*fileTree)
}

func (t *fileTree) mount() {
	t.mounts++
	if t.mounts != 1 || t.host.Read == nil {
		return
	}
	ctx, cancel := context.WithCancel(t.host.Context)
	release := t.host.Observe(ctx)
	t.ctx = ctx
	t.detach = func() { cancel(); release() }
	t.pages = map[string]*directoryPage{}
	t.revision = t.host.Revision()
	t.dirty = true
}

func (t *fileTree) unmount() {
	t.mounts--
	if t.mounts != 0 {
		return
	}
	t.resetSearch()
	if t.host.CancelOpen != nil {
		t.host.CancelOpen()
	}
	if t.detach != nil {
		t.detach()
		t.detach, t.ctx = nil, nil
	}
	t.pages, t.entries, t.visible, t.drafts = nil, nil, nil, nil
	t.reported = ""
	t.dirty = true
}

func (t *fileTree) update() {
	if reveal := t.project.TakeFileDirectoryReveal(); reveal != nil {
		name := *reveal
		if name == "." {
			name = ""
		}
		t.reveal = &name
		t.searchOpen = false
		t.resetSearch()
		for dir := name; ; dir = parentDir(dir) {
			*t.folder(dir) = true
			if dir == "" {
				break
			}
		}
		t.dirty = true
	}
	if t.ctx == nil || t.revision == t.host.Revision() {
		return
	}
	t.revision = t.host.Revision()
	for name, page := range t.pages {
		if !t.expanded(name) {
			t.removePages(name)
			continue
		}
		if page.read != nil {
			page.refreshPending = true
		} else {
			t.refresh(name)
		}
	}
	t.dirty = true
}

func (t *fileTree) expanded(name string) bool {
	for dir := name; ; dir = parentDir(dir) {
		if !*t.folder(dir) {
			return false
		}
		if dir == "" {
			return true
		}
	}
}

func (t *fileTree) folder(name string) *bool {
	if expanded := t.folders[name]; expanded != nil {
		return expanded
	}
	open := name == "" || t.host.Read == nil
	t.folders[name] = &open
	return &open
}

func (t *fileTree) ensurePage(name string) *directoryPage {
	if t.ctx == nil || t.ctx.Err() != nil {
		return nil
	}
	if expanded := t.folders[name]; expanded != nil && !*expanded {
		return nil
	}
	if state := t.pages[name]; state != nil {
		return state
	}
	state := &directoryPage{}
	t.pages[name] = state
	t.request(name, state, nil, directoryRead{})
	return state
}

func (t *fileTree) refresh(name string) {
	if state := t.pages[name]; state != nil && state.read == nil {
		var through *treeEntry
		if n := len(state.entries); n > 0 {
			through = &state.entries[n-1]
		}
		t.request(name, state, nil, directoryRead{through: through})
	}
}

func (t *fileTree) more(name string) {
	if state := t.pages[name]; state != nil && state.next != nil && state.read == nil {
		t.request(name, state, state.next, directoryRead{entries: append([]treeEntry(nil), state.entries...), partial: state.partial})
	}
}

type directoryRead struct {
	entries []treeEntry
	seen    map[string]bool
	through *treeEntry
	partial bool
}

func (t *fileTree) request(name string, state *directoryPage, after *protocol.DirectoryCursor, read directoryRead) {
	ctx, cancel := context.WithCancel(t.ctx)
	state.read, state.failed = cancel, false
	t.host.Read(ctx, name, after, func(c *ui.Context, page protocol.Directory, err error) {
		if ctx.Err() != nil || t.pages[name] != state {
			return
		}
		cancel()
		state.read = nil
		if state.refreshPending {
			state.refreshPending = false
			t.refresh(name)
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			code := t.host.Fault(err)
			switch code {
			case protocol.ErrorCodeRevisionConflict:
				if after != nil {
					t.refresh(name)
					return
				}
			case protocol.ErrorCodePermissionDenied, protocol.ErrorCodeNotFound:
				t.removePages(name)
				for _, old := range state.entries {
					t.forget(old.path)
				}
				t.pages[name] = state
				state.entries, state.loaded = nil, false
				state.denied = code == protocol.ErrorCodePermissionDenied
				t.dirty = true
			}
			state.next, state.failed = nil, true
			t.report(c, code, err)
			return
		}
		previous := len(read.entries)
		if read.seen == nil {
			read.seen = make(map[string]bool, previous+len(page.Entries))
			for _, entry := range read.entries {
				read.seen[entry.name] = entry.directory
			}
		}
		for _, raw := range page.Entries {
			if _, duplicate := read.seen[raw.Name]; duplicate || raw.Name == "" || raw.Name == "." || raw.Name == ".." || strings.ContainsAny(raw.Name, "/\\\x00") {
				continue
			}
			dir := raw.Kind == protocol.EntryKindDirectory
			read.entries = append(read.entries, treeEntry{path.Join(name, raw.Name), raw.Name, dir})
			read.seen[raw.Name] = dir
		}
		read.partial = read.partial || page.UnsupportedNames > 0 || page.Truncated && page.Next == nil
		// Refresh through the last previously loaded key. Insertions before that
		// key may move selected or expanded entries onto a later page.
		if through, next := read.through, page.Next; through != nil && next != nil && len(read.entries) > previous &&
			(next.Directory && !through.directory || next.Directory == through.directory && next.Name < through.name) {
			t.request(name, state, next, read)
			return
		}
		for _, old := range state.entries {
			if dir, found := read.seen[old.name]; !found || dir != old.directory {
				t.removePages(old.path)
				t.forget(old.path)
			}
		}
		state.entries, state.next = read.entries, page.Next
		state.partial, state.loaded, state.denied = read.partial, true, false
		t.reported = ""
		t.dirty = true
	})
}

func (t *fileTree) forget(name string) {
	for folder := range t.folders {
		if within(folder, name) {
			delete(t.folders, folder)
			delete(t.expansions, folder)
		}
	}
	for target := range t.sel.selected {
		if within(target, name) {
			delete(t.sel.selected, target)
		}
	}
	if within(t.sel.current, name) {
		t.sel.current = ""
	}
	if within(t.sel.anchor, name) {
		t.sel.anchor = ""
	}
}

func (t *fileTree) report(c *ui.Context, code protocol.ErrorCode, err error) {
	key, outcome := "directory_failed", controls.Failed
	switch code {
	case protocol.ErrorCodePermissionDenied:
		key = "directory_denied"
	case protocol.ErrorCodeNotFound:
		key = "directory_missing"
	case protocol.ErrorCodeConflict, protocol.ErrorCodeRevisionConflict:
		key, outcome = "workspace_changed", controls.Conflict
	case protocol.ErrorCodeOutcomeUnknown:
		key, outcome = "workspace_action_unknown", controls.Uncertain
	}
	if key != t.reported {
		t.reported = key
		controls.RecordFailure("list_directory", outcome, err)
		controls.Toast(c, outcome, i18n.T(c, key))
	}
}

func (t *fileTree) removePages(name string) {
	for dir, state := range t.pages {
		if name == "" || within(dir, name) {
			if state.read != nil {
				state.read()
			}
			delete(t.pages, dir)
		}
	}
}

func (t *fileTree) root() treeEntry {
	if t.host.Read == nil && t.project.Data.Files != nil {
		return sampleEntry(t.project.Data.Files)
	}
	return treeEntry{name: t.host.Name, directory: true}
}

func sampleEntry(node *native.FileNode) treeEntry {
	return treeEntry{node.Path, node.Name, node.Children != nil}
}

func parentDir(name string) string {
	if dir := path.Dir(name); dir != "." {
		return dir
	}
	return ""
}
