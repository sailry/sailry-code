package desktop

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
)

func sampled(c *ui.Context, run func() bool) {
	if !kit.Demo(c) || !run() {
		notice(c)
	}
}

func covered(path string, paths []string) bool {
	return len(paths) == 0 || slices.ContainsFunc(paths, func(p string) bool {
		return p == "" || path == p || strings.HasPrefix(path, p+"/")
	})
}

// stage and unstage move a change between the work tree and the index.
func stageChange(e *native.WorkspaceChange) {
	if e.Untracked {
		e.Untracked, e.Staged = false, "A"
	} else if e.Unstaged != "" {
		e.Staged = e.Unstaged
	}
	e.Unstaged = ""
}

func unstageChange(e *native.WorkspaceChange) {
	if e.Staged == "A" {
		e.Untracked = true
	} else if e.Staged != "" {
		e.Unstaged = e.Staged
	}
	e.Staged = ""
}

// sampleGit is a Git action on the demo's repository.
func (pr *repository) sampleGit(c *ui.Context, a native.GitAction) bool {
	w, v := pr.Data, &pr.view
	repo := &w.Repo
	each := func(fn func(*native.WorkspaceChange)) {
		for i := range w.Changes {
			if covered(w.Changes[i].Path, a.Paths) {
				fn(&w.Changes[i])
			}
		}
	}
	drop := func(match func(native.WorkspaceChange) bool) {
		w.Changes = slices.DeleteFunc(w.Changes, match)
		if len(w.Changes) > 0 && !slices.ContainsFunc(w.Changes, func(e native.WorkspaceChange) bool { return e.Path == pr.Git.Change() }) {
			pr.Git.Show(w.Changes[0].Path)
		}
	}
	current := func() *native.WorkspaceBranch {
		for i := range repo.Branches {
			if repo.Branches[i].Current {
				return &repo.Branches[i]
			}
		}
		return nil
	}
	// commit puts a commit at the head of the branch checked out.
	commit := func(message string) {
		v.sampleCommits++
		cm := native.WorkspaceCommit{ID: fmt.Sprintf("%016x", 0xd3a0000000000000+uint64(v.sampleCommits)), Message: message, Time: native.PreviewTime, Refs: []string{"HEAD", "heads/" + repo.Branch}}
		if len(repo.Commits) > 0 {
			head := &repo.Commits[0]
			cm.Author, cm.Parents = head.Author, []string{head.ID}
			head.Refs = slices.DeleteFunc(slices.Clone(head.Refs), func(r string) bool { return slices.Contains(cm.Refs, r) })
		}
		repo.Commits = slices.Insert(repo.Commits, 0, cm)
		if b := current(); b != nil {
			b.Commit = cm.ID[:12]
		}
	}
	// stash puts the matching changes away under a message.
	stash := func(match func(native.WorkspaceChange) bool) {
		var kept []native.WorkspaceChange
		for _, e := range w.Changes {
			if match(e) {
				kept = append(kept, e)
			}
		}
		message := "WIP on " + repo.Branch
		if len(repo.Commits) > 0 {
			message += ": " + repo.Commits[0].ID[:7] + " " + strings.SplitN(repo.Commits[0].Message, "\n", 2)[0]
		}
		repo.Stashes = slices.Insert(repo.Stashes, 0, message)
		v.sampleStashes = slices.Insert(v.sampleStashes, 0, kept)
		drop(match)
	}
	// stashAt is the stash an action names by its message, or the latest.
	stashAt := func() int {
		if a.Target == "" && len(repo.Stashes) > 0 {
			return 0
		}
		return slices.Index(repo.Stashes, a.Target)
	}
	restore := func(i int, pop bool) {
		if i < 0 {
			return
		}
		// The stashes the demo began with hold no changes of its tree.
		for len(v.sampleStashes) < len(repo.Stashes) {
			v.sampleStashes = append(v.sampleStashes, nil)
		}
		for _, e := range v.sampleStashes[i] {
			if !slices.ContainsFunc(w.Changes, func(x native.WorkspaceChange) bool { return x.Path == e.Path }) {
				w.Changes = append(w.Changes, e)
			}
		}
		if pop {
			repo.Stashes = slices.Delete(repo.Stashes, i, i+1)
			v.sampleStashes = slices.Delete(v.sampleStashes, i, i+1)
		}
	}
	synced := func() {
		if b := current(); b != nil {
			b.Tracking = ""
		}
	}
	switch a.Kind {
	case "git_stage", "git_stage_all":
		each(stageChange)
	case "git_unstage", "git_unstage_all":
		each(unstageChange)
	case "git_discard_changes", "git_discard_all":
		drop(func(e native.WorkspaceChange) bool { return covered(e.Path, a.Paths) })
	case "git_commit":
		if a.Amend && len(repo.Commits) > 0 {
			repo.Commits[0].Message = a.Message
		} else {
			commit(a.Message)
		}
		staged := slices.ContainsFunc(w.Changes, func(e native.WorkspaceChange) bool { return e.Staged != "" })
		if a.Scope == "all" || !staged {
			drop(func(native.WorkspaceChange) bool { return true })
		} else {
			drop(func(e native.WorkspaceChange) bool { return e.Staged != "" && e.Unstaged == "" })
			for i := range w.Changes {
				w.Changes[i].Staged = ""
			}
		}
		pr.commitSaved(a.Message)
		if a.After != "" && a.After != "none" {
			synced()
		}
	case "git_undo_commit":
		if len(repo.Commits) < 2 {
			return false
		}
		refs := repo.Commits[0].Refs
		repo.Commits = repo.Commits[1:]
		repo.Commits[0].Refs = append(slices.Clone(repo.Commits[0].Refs), refs...)
		if b := current(); b != nil {
			b.Commit = repo.Commits[0].ID[:12]
		}
	case "git_cherry_pick":
		at := slices.IndexFunc(repo.Commits, func(cm native.WorkspaceCommit) bool { return cm.ID == a.Target })
		if at < 0 {
			return false
		}
		commit(repo.Commits[at].Message)
	case "branch_switch", "git_checkout":
		at := slices.IndexFunc(repo.Branches, func(b native.WorkspaceBranch) bool { return b.Name == a.Target && !b.Remote })
		if at < 0 {
			return false
		}
		for i := range repo.Branches {
			repo.Branches[i].Current = i == at
		}
		repo.Branch, repo.Upstream = a.Target, ""
		if slices.ContainsFunc(repo.Branches, func(b native.WorkspaceBranch) bool { return b.Remote && b.Name == "origin/"+a.Target }) {
			repo.Upstream = "origin/" + a.Target
		}
		for i := range repo.Commits {
			cm := &repo.Commits[i]
			cm.Refs = slices.DeleteFunc(slices.Clone(cm.Refs), func(r string) bool { return r == "HEAD" })
			if slices.Contains(cm.Refs, "heads/"+a.Target) {
				cm.Refs = append([]string{"HEAD"}, cm.Refs...)
			}
		}
	case "branch_delete", "git_delete_branch", "git_delete_remote_branch":
		repo.Branches = slices.DeleteFunc(repo.Branches, func(b native.WorkspaceBranch) bool { return b.Name == a.Target && !b.Current })
		for i := range repo.Commits {
			cm := &repo.Commits[i]
			cm.Refs = slices.DeleteFunc(slices.Clone(cm.Refs), func(r string) bool { return r == "heads/"+a.Target || r == "remotes/"+a.Target })
		}
	case "branch_merge":
		commit("Merge branch '" + a.Target + "' into " + repo.Branch)
	case "branch_create", "branch_rename":
		title := "git_new_branch"
		if a.Kind == "branch_rename" {
			title = "git_rename_branch"
		}
		pr.Prompt(title, []string{"settings_name"}, []string{a.Target}, func(values []string, done func(bool)) {
			name := strings.TrimSpace(values[0])
			taken := slices.ContainsFunc(repo.Branches, func(b native.WorkspaceBranch) bool { return b.Name == name })
			if name == "" || taken {
				done(false)
				return
			}
			if at := slices.IndexFunc(repo.Branches, func(b native.WorkspaceBranch) bool { return b.Name == a.Target }); a.Kind == "branch_rename" && at >= 0 {
				repo.Branches[at].Name = name
				if repo.Branches[at].Current {
					repo.Branch = name
				}
			} else {
				at := a.Secondary
				if at == "" && len(repo.Commits) > 0 {
					at = repo.Commits[0].ID
				}
				repo.Branches = append(repo.Branches, native.WorkspaceBranch{Name: name, Commit: at[:min(12, len(at))]})
			}
			for i := range repo.Commits {
				cm := &repo.Commits[i]
				if a.Kind == "branch_rename" {
					if j := slices.Index(cm.Refs, "heads/"+a.Target); j >= 0 {
						cm.Refs = slices.Clone(cm.Refs)
						cm.Refs[j] = "heads/" + name
					}
				} else if a.Secondary == "" && i == 0 || a.Secondary != "" && strings.HasPrefix(cm.ID, a.Secondary) {
					cm.Refs = append(slices.Clone(cm.Refs), "heads/"+name)
				}
			}
			done(true)
		})
	case "git_tag_create":
		pr.Prompt("git_tag_create", []string{"settings_name"}, []string{""}, func(values []string, done func(bool)) {
			name := strings.TrimSpace(values[0])
			if name == "" || slices.Contains(repo.Tags, name) {
				done(false)
				return
			}
			repo.Tags = append(repo.Tags, name)
			for i := range repo.Commits {
				if cm := &repo.Commits[i]; a.Secondary == "" && i == 0 || a.Secondary != "" && cm.ID == a.Secondary {
					cm.Refs = append(slices.Clone(cm.Refs), "tags/"+name)
				}
			}
			done(true)
		})
	case "git_tag_delete":
		repo.Tags = slices.DeleteFunc(repo.Tags, func(t string) bool { return t == a.Target })
		for i := range repo.Commits {
			cm := &repo.Commits[i]
			cm.Refs = slices.DeleteFunc(slices.Clone(cm.Refs), func(r string) bool { return r == "tags/"+a.Target })
		}
	case "git_ignore":
		// An ignored file leaves the changes; a tracked one stays changed.
		drop(func(e native.WorkspaceChange) bool { return e.Untracked && e.Staged == "" && covered(e.Path, a.Paths) })
	case "git_add_remote":
		pr.Prompt(a.Kind, []string{"settings_name", "mcp_url"}, []string{"", ""}, func(values []string, done func(bool)) {
			name, address := strings.TrimSpace(values[0]), strings.TrimSpace(values[1])
			if name == "" || address == "" || slices.ContainsFunc(repo.Remotes, func(r native.WorkspaceRemote) bool { return r.Name == name }) {
				done(false)
				return
			}
			repo.Remotes = append(repo.Remotes, native.WorkspaceRemote{Name: name, URL: address})
			done(true)
		})
	case "git_remove_remote":
		repo.Remotes = slices.DeleteFunc(repo.Remotes, func(r native.WorkspaceRemote) bool { return r.Name == a.Target || r.Name == a.Remote })
	case "git_stash_all":
		stash(func(native.WorkspaceChange) bool { return true })
	case "git_stash_tracked":
		stash(func(e native.WorkspaceChange) bool { return !e.Untracked })
	case "git_stash_staged":
		stash(func(e native.WorkspaceChange) bool { return e.Staged != "" })
	case "git_stash_apply", "git_stash_apply_latest":
		restore(stashAt(), false)
	case "git_stash_pop", "git_stash_pop_latest":
		restore(stashAt(), true)
	case "git_stash_drop":
		if i := stashAt(); i >= 0 {
			repo.Stashes = slices.Delete(repo.Stashes, i, i+1)
			if i < len(v.sampleStashes) {
				v.sampleStashes = slices.Delete(v.sampleStashes, i, i+1)
			}
		}
	case "git_stash_drop_all":
		repo.Stashes, v.sampleStashes = nil, nil
	case "git_fetch", "git_fetch_all", "git_fetch_prune", "git_fetch_all_prune", "git_tag_push", "git_tag_delete_remote":
		// The remotes hold nothing the demo's repository lacks.
	case "git_pull", "git_pull_rebase", "git_pull_from", "git_push", "git_push_to", "git_force_push", "git_sync", "git_sync_rebase", "git_pull_push":
		synced()
	default:
		return false
	}
	return true
}

// sampleFile is a Files action on the demo's tree.
