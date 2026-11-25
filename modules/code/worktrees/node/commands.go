package worktreesnode

import (
	"context"
	"errors"

	git "github.com/sailry/sailry-code/modules/code/git/service"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

func bindWorkspace(repo *git.Service) native.WorkspaceBindings {
	return native.WorkspaceBindings{
		CaptureBoundary: repo.CaptureManaged,
		RemoveUnusedWorktree: func(ctx context.Context, capture native.ProjectWorktree, beforeRemove func()) (bool, error) {
			tree := capture.Worktree
			if tree.Path != native.ManagedWorktreePath(capture.Project.Path, tree.ID) {
				return false, nil
			}
			status, err := repo.Status(ctx, tree.Path)
			if err != nil {
				return false, err
			}
			if status.Head == nil || status.Branch == nil {
				return false, errors.New("worktree has no branch or HEAD")
			}
			beforeRemove()
			err = repo.RemoveWorktree(ctx, tree.Path, *status.Head, *status.Branch)
			return err == nil, err
		},
		Commands: map[string]native.WorkspaceCommand{
			"list_worktrees": {Run: func(ctx context.Context, scope native.WorkspaceCommandScope, _ protocol.Command) (protocol.Output, error) {
				value, err := repo.ListWorktrees(ctx, scope.Root())
				return protocol.Output{Kind: "git_worktrees", Data: value}, err
			}},
			"create_worktree": {
				Lock: native.WorkspaceLockChange,
				ProjectRoot: func(command protocol.Command) protocol.ProjectID {
					return command.Data.(*protocol.CommandCreateWorktreePayload).Project
				},
				Run: func(ctx context.Context, scope native.WorkspaceCommandScope, command protocol.Command) (protocol.Output, error) {
					input := command.Data.(*protocol.CommandCreateWorktreePayload)
					path, err := repo.CreateWorktree(ctx, scope.Root(), input.Path, input.Branch, input.Commit)
					if err != nil {
						return protocol.Output{}, err
					}
					return scope.RegisterWorktree(protocol.Worktree{ID: protocol.NewWorktreeID(), Project: &input.Project, Path: path}, false)
				},
			},
			"create_managed_worktree": {Lock: native.WorkspaceLockChange, Run: func(ctx context.Context, scope native.WorkspaceCommandScope, command protocol.Command) (protocol.Output, error) {
				input := command.Data.(*protocol.CommandCreateManagedWorktreePayload)
				capture, err := scope.CaptureWorktree(input.Source)
				if err != nil {
					return protocol.Output{}, err
				}
				if capture.Project.ID != input.Project {
					return protocol.Output{}, &protocol.Fault{Code: protocol.ErrorCodeWrongTarget, Message: "source worktree belongs to another project"}
				}
				id := protocol.NewWorktreeID()
				path, err := repo.CreateManagedWorktree(ctx, scope.Root(), capture.Project.Path, id, *input)
				if err != nil {
					return protocol.Output{}, err
				}
				return scope.RegisterWorktree(protocol.Worktree{ID: id, Project: &input.Project, Path: path}, false)
			}},
			"register_worktree": {
				Lock: native.WorkspaceLockChange,
				ProjectRoot: func(command protocol.Command) protocol.ProjectID {
					return command.Data.(*protocol.CommandRegisterWorktreePayload).Project
				},
				Run: func(ctx context.Context, scope native.WorkspaceCommandScope, command protocol.Command) (protocol.Output, error) {
					input := command.Data.(*protocol.CommandRegisterWorktreePayload)
					path, err := native.WorkspaceDirectory(input.Path)
					if err != nil {
						return protocol.Output{}, err
					}
					if err := native.VerifyWorktreeMembership(ctx, scope.Root(), path); err != nil {
						return protocol.Output{}, err
					}
					return scope.RegisterWorktree(protocol.Worktree{ID: protocol.NewWorktreeID(), Project: &input.Project, Path: path}, true)
				},
			},
			"remove_worktree": {Lock: native.WorkspaceLockChange, Run: func(ctx context.Context, scope native.WorkspaceCommandScope, command protocol.Command) (protocol.Output, error) {
				input := command.Data.(*protocol.CommandRemoveWorktreePayload)
				return scope.RemoveWorktree(input.Worktree, func(path string) error {
					return repo.RemoveWorktree(ctx, path, input.ExpectedHead, input.ExpectedBranch)
				})
			}},
		},
	}
}
