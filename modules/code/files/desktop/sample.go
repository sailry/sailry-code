package desktop

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"slices"
	"strings"
)

func (t *fileTree) action(c *ui.Context, action native.FileAction) {
	if t.project.Actions != nil {
		t.project.ApplyFileAction(c, action)
		return
	}
	if !controls.Demo(c) || !t.sampleFile(action) {
		controls.PreviewNotice(c)
	}
}

func (t *fileTree) pasteReady(c *ui.Context) bool {
	if t.project.Actions != nil {
		return t.project.Actions.PasteReady != nil && t.project.Actions.PasteReady()
	}
	return controls.Demo(c) && len(t.clip) > 0
}

func findSample(root *native.FileNode, path string) *native.FileNode {
	if root == nil {
		return nil
	}
	if root.Path == path {
		return root
	}
	for _, child := range root.Children {
		if node := findSample(child, path); node != nil {
			return node
		}
	}
	return nil
}

func (t *fileTree) sampleFile(a native.FileAction) bool {
	pr := t.project
	root := pr.Data.Files
	if root == nil {
		return false
	}
	parent := func(path string) *native.FileNode {
		if f := findSample(root, parentDir(path)); f != nil && f.Children != nil {
			return f
		}
		return nil
	}
	remove := func(path string) *native.FileNode {
		dir := parent(path)
		if dir == nil {
			return nil
		}
		i := slices.IndexFunc(dir.Children, func(f *native.FileNode) bool { return f.Path == path })
		if i < 0 {
			return nil
		}
		f := dir.Children[i]
		dir.Children = slices.Delete(dir.Children, i, i+1)
		t.forget(path)
		t.dirty = true
		return f
	}
	// place puts a node in a folder under a name no sibling has.
	place := func(dir *native.FileNode, f *native.FileNode, name string) {
		base, n := name, 1
		for slices.ContainsFunc(dir.Children, func(x *native.FileNode) bool { return x.Name == name }) {
			n++
			name = fmt.Sprintf("%s %d", base, n)
		}
		f.Name = name
		repath(f, dir.Path+"/"+name)
		dir.Children = append(dir.Children, f)
		slices.SortStableFunc(dir.Children, func(x, y *native.FileNode) int {
			if (x.Children != nil) != (y.Children != nil) {
				if x.Children != nil {
					return -1
				}
				return 1
			}
			return strings.Compare(x.Name, y.Name)
		})
		t.dirty = true
	}
	folder := func() *native.FileNode {
		if f := findSample(root, a.Directory); f != nil && f.Children != nil {
			return f
		}
		return root
	}
	switch a.Kind {
	case "files_new_file", "files_new_directory":
		label := "files_new_file"
		if a.Kind == "files_new_directory" {
			label = "files_new_directory"
		}
		dir := folder()
		pr.Prompt(label, []string{"settings_name"}, []string{""}, func(values []string, done func(bool)) {
			name := strings.TrimSpace(values[0])
			if name == "" || strings.Contains(name, "/") {
				done(false)
				return
			}
			f := &native.FileNode{}
			if a.Kind == "files_new_directory" {
				f.Children = []*native.FileNode{}
			}
			place(dir, f, name)
			done(true)
		})
	case "files_rename":
		if len(a.Paths) != 1 {
			return false
		}
		f := findSample(root, a.Paths[0])
		dir := parent(a.Paths[0])
		if f == nil || dir == nil {
			return false
		}
		pr.Prompt("files_rename", []string{"settings_name"}, []string{f.Name}, func(values []string, done func(bool)) {
			name := strings.TrimSpace(values[0])
			if name == "" || strings.Contains(name, "/") {
				done(false)
				return
			}
			if name != f.Name {
				remove(f.Path)
				place(dir, f, name)
			}
			done(true)
		})
	case "files_trash":
		for _, path := range a.Paths {
			remove(path)
			pr.Files.Close(path)
		}
	case "files_move":
		dir := folder()
		for _, path := range a.Paths {
			if dir.Path == path || strings.HasPrefix(dir.Path, path+"/") || parentDir(path) == dir.Path {
				continue
			}
			if f := remove(path); f != nil {
				place(dir, f, f.Name)
			}
		}
	case "files_copy", "files_cut":
		t.clip, t.cut = slices.Clone(a.Paths), a.Kind == "files_cut"
	case "files_paste":
		dir := folder()
		for _, path := range t.clip {
			if t.cut {
				if dir.Path == path || strings.HasPrefix(dir.Path, path+"/") {
					continue
				}
				if f := remove(path); f != nil {
					place(dir, f, f.Name)
				}
			} else if f := findSample(root, path); f != nil {
				place(dir, cloneFile(f), f.Name)
			}
		}
		if t.cut {
			t.clip = nil
		}
	default:
		return false
	}
	return true
}

func repath(f *native.FileNode, path string) {
	f.Path = path
	for _, k := range f.Children {
		repath(k, path+"/"+k.Name)
	}
}

func cloneFile(f *native.FileNode) *native.FileNode {
	out := *f
	if f.Children != nil {
		out.Children = make([]*native.FileNode, 0, len(f.Children))
		for _, k := range f.Children {
			out.Children = append(out.Children, cloneFile(k))
		}
	}
	return &out
}
