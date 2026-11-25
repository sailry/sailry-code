package git

import (
	"context"

	native "github.com/sailry/sailry-harness/sdk/native/node"
)

type Options = native.GitOptions
type Runner = native.GitRunner
type CommandResult = native.GitCommandResult

var workspaceFactory = native.WorkspaceFactory[*Service]{Open: func(host native.WorkspaceOptions) (*Service, func() error, error) {
	options := host.Git
	if options.Environment == nil {
		options.Environment = host.Environment
	}
	options.Profile, options.Lock = host.Profile, host.Lock
	ctx, cancel := context.WithCancel(host.Context)
	return newService(ctx, options), func() error { cancel(); return nil }, nil
}}

// BindWorkspace shares the engine, but only binds the caller's handlers.
func BindWorkspace(bind func(*Service) native.WorkspaceBindings) native.WorkspaceBinder {
	return workspaceFactory.Bind(bind)
}
