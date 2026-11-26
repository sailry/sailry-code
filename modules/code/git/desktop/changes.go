package desktop

import (
	"cmp"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

// The Git change list's rows, as the git plugin's changes.js builds them:
// grouped (none, staged and unstaged, or tracked and untracked), sorted
// (by path, name or status) and listed flat or as folders.

// Grouping, sort and layout of the change list (repository.js navigation,
// which starts hierarchical, grouped tracked and untracked, by path).
type changeNav struct {
	hierarchical bool
	grouping     string // none, staged, tracked
	sort         string // path, name, status
}

var defaultNav = changeNav{hierarchical: true, grouping: "tracked", sort: "path"}

// changeRow is a row of the change list: a group heading (entries set), a
// folder, or a change (entry set). Its id is the group's prefix, then
// "dir/<prefix>/<folder>" or "<prefix>/<path>".
type changeRow struct {
	id, label string
	entry     *native.WorkspaceChange
	entries   []native.WorkspaceChange // a group's
	children  []*changeRow
}

// isNew is an untracked or added file.
func isNew(e native.WorkspaceChange) bool { return e.Untracked || e.Staged == "A" }

// belongs reports whether e is in group.
func belongs(e native.WorkspaceChange, group string) bool {
	switch group {
	case "staged":
		return e.Staged != ""
	case "unstaged":
		return e.Unstaged != "" || e.Untracked
	case "tracked":
		return !isNew(e)
	case "untracked":
		return isNew(e)
	}
	return true
}

func unstagePaths(entries []native.WorkspaceChange) []string {
	var out []string
	for _, e := range entries {
		if e.Staged != "" {
			out = append(out, e.Path)
		}
	}
	return out
}

// selectedID is the row of entry's diff, opened in scope.
func selectedID(e *native.WorkspaceChange, grouping, scope string) string {
	if e == nil {
		return ""
	}
	staged := e.Staged != "" && e.Unstaged == "" && !e.Untracked
	prefix := "unstaged"
	switch {
	case grouping == "none":
		prefix = "all"
	case grouping == "tracked" && isNew(*e):
		prefix = "untracked"
	case grouping == "tracked":
		prefix = "tracked"
	case scope == "staged" || scope == "all" && staged:
		prefix = "staged"
	}
	return prefix + "/" + e.Path
}

func baseName(path string) string { return path[strings.LastIndex(path, "/")+1:] }

func parentDir(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

// statusOrder sorts no change first, then by its letter.
func statusOrder(s string) string {
	if s == "" {
		return "0"
	}
	return "1" + s
}

func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}

// directories lists entries under parent as folders, sorted, then files.
func directories(prefix, parent string, entries []native.WorkspaceChange) []*changeRow {
	folders := map[string][]native.WorkspaceChange{}
	var names []string
	var files []native.WorkspaceChange
	for _, e := range entries {
		rel := e.Path[len(parent):]
		i := strings.Index(rel, "/")
		if i < 0 {
			files = append(files, e)
			continue
		}
		name := rel[:i]
		if _, ok := folders[name]; !ok {
			names = append(names, name)
		}
		folders[name] = append(folders[name], e)
	}
	slices.Sort(names)
	var out []*changeRow
	for _, name := range names {
		out = append(out, &changeRow{id: "dir/" + prefix + "/" + parent + name, label: name,
			children: directories(prefix, parent+name+"/", folders[name])})
	}
	slices.SortFunc(files, func(a, b native.WorkspaceChange) int { return cmp.Compare(a.Path, b.Path) })
	for i := range files {
		out = append(out, &changeRow{id: prefix + "/" + files[i].Path, label: baseName(files[i].Path), entry: &files[i]})
	}
	return out
}

// The navigator also edits sample status in place. Compare the fields it
// presents, without retaining an open document's hunks in a sidebar row.
func changeListRows(scroll ui.Element, changes []native.WorkspaceChange, nav changeNav) []*changeRow {
	type projection struct {
		nav     changeNav
		entries []native.WorkspaceChange
		rows    []*changeRow
	}
	cache := ui.Local(scroll, "git-status-rows", func() projection { return projection{} })
	if cache.nav == nav && slices.EqualFunc(cache.entries, changes, func(a, b native.WorkspaceChange) bool {
		return a.Path == b.Path && a.Status == b.Status && a.Staged == b.Staged && a.Unstaged == b.Unstaged && a.Added == b.Added && a.Removed == b.Removed && a.Untracked == b.Untracked
	}) {
		return cache.rows
	}
	entries := slices.Clone(changes)
	for i := range entries {
		entries[i].Hunks = nil
	}
	cache.nav, cache.entries = nav, entries
	cache.rows = changeRows(entries, nav)
	return cache.rows
}

// Group labels are translation keys; file and directory labels are names.
func changeRows(changes []native.WorkspaceChange, nav changeNav) []*changeRow {
	groups := [][2]string{{"tracked", "git_tracked"}, {"untracked", "git_untracked"}}
	switch nav.grouping {
	case "none":
		groups = [][2]string{{"all", "git_all_changes"}}
	case "staged":
		groups = [][2]string{{"staged", "git_staged"}, {"unstaged", "git_unstaged"}}
	}
	var out []*changeRow
	for _, g := range groups {
		prefix := g[0]
		var entries []native.WorkspaceChange
		for _, e := range changes {
			if belongs(e, prefix) {
				entries = append(entries, e)
			}
		}
		if len(entries) == 0 {
			continue
		}
		switch nav.sort {
		case "name":
			slices.SortStableFunc(entries, func(a, b native.WorkspaceChange) int {
				return cmp.Or(cmp.Compare(baseName(a.Path), baseName(b.Path)), cmp.Compare(a.Path, b.Path))
			})
		case "status":
			slices.SortStableFunc(entries, func(a, b native.WorkspaceChange) int {
				return cmp.Or(cmp.Compare(flag(a.Untracked), flag(b.Untracked)),
					cmp.Compare(statusOrder(a.Staged)+statusOrder(a.Unstaged), statusOrder(b.Staged)+statusOrder(b.Unstaged)),
					cmp.Compare(a.Path, b.Path))
			})
		}
		var children []*changeRow
		if nav.hierarchical {
			children = directories(prefix, "", entries)
		} else {
			for i := range entries {
				children = append(children, &changeRow{id: prefix + "/" + entries[i].Path, label: baseName(entries[i].Path), entry: &entries[i]})
			}
		}
		if nav.grouping == "none" {
			out = append(out, children...)
			continue
		}
		out = append(out, &changeRow{id: prefix, label: g[1], entries: entries, children: children})
	}
	return out
}

// visitChanges follows the tree's action order until visit returns false.
func (r *changeRow) visitChanges(visit func(native.WorkspaceChange) bool) bool {
	if r.entry != nil {
		return visit(*r.entry)
	}
	if r.entries != nil {
		for _, entry := range r.entries {
			if !visit(entry) {
				return false
			}
		}
	} else {
		for _, child := range r.children {
			if !child.visitChanges(visit) {
				return false
			}
		}
	}
	return true
}

func (r *changeRow) descendants() []native.WorkspaceChange {
	if r.entries != nil {
		return r.entries
	}
	var entries []native.WorkspaceChange
	r.visitChanges(func(e native.WorkspaceChange) bool {
		entries = append(entries, e)
		return true
	})
	return entries
}

// scope is the group a row is in: staged, unstaged or all.
func (r *changeRow) scope() string {
	id := strings.TrimPrefix(r.id, "dir/")
	prefix, _, _ := strings.Cut(id, "/")
	if prefix == "staged" || prefix == "unstaged" {
		return prefix
	}
	return "all"
}

// kind is what changed of a change in scope: its letter, A for an
// untracked file.
func kind(e native.WorkspaceChange, scope string) string {
	switch {
	case e.Untracked:
		return "A"
	case scope == "staged":
		return e.Staged
	case scope == "unstaged":
		return e.Unstaged
	case e.Unstaged != "":
		return e.Unstaged
	}
	return e.Staged
}
