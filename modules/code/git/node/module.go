// Package gitnode provides the native Node Git workspace service and read-only
// inspection tools paired with modules/code/git/desktop.
package gitnode

import (
	"context"
	_ "embed"
	"encoding/json"
	"slices"
	"sync"

	git "github.com/sailry/sailry-code/modules/code/git/service"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

//go:embed tools.json
var catalogJSON []byte

var loadCatalog = sync.OnceValues(func() (native.CatalogSpec, error) {
	return native.LoadCatalog(catalogJSON)
})

type module struct{}

var _ native.WorkspaceServiceProvider = module{}

// Module returns the native Node module providing workspace Git services and inspection tools.
func Module() native.Module { return module{} }

func (module) ID() string                { return "git" }
func (module) ReplacesPlugins() []string { return []string{"git"} }

func (module) WorkspaceServices() native.WorkspaceServices {
	return native.WorkspaceServices{Git: true, Bind: git.BindWorkspace(bindWorkspace)}
}

func (module) Catalog() []native.ToolMeta {
	catalog, err := loadCatalog()
	if err != nil {
		return nil
	}
	metas := make([]native.ToolMeta, 0, len(catalog.Tools))
	for _, spec := range catalog.Tools {
		metas = append(metas, spec.Meta())
	}
	return metas
}

func (module) ToolsForTurn(_ context.Context, host native.Host, inv *native.AgentInvocation) ([]native.Tool, error) {
	catalog, err := loadCatalog()
	if err != nil {
		return nil, err
	}
	tools := make([]native.Tool, 0, len(catalog.Tools))
	for _, spec := range catalog.Tools {
		if !spec.Available(inv) {
			continue
		}
		prepare := prepareStatus
		if spec.Name == "git_diff" {
			prepare = prepareDiff
		}
		tool, err := host.WorkspaceTool(inv, native.WorkspaceToolConfig{
			Spec:        spec,
			Instruction: catalog.Instructions,
			PrepareArgs: prepare,
		})
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func readArgs(raw json.RawMessage, allowed ...string) (map[string]json.RawMessage, error) {
	invalid := &protocol.Fault{Code: protocol.ErrorCodeInvalidRequest, Message: "Invalid Git arguments"}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil || args == nil {
		return nil, invalid
	}
	for name := range args {
		if !slices.Contains(allowed, name) {
			return nil, invalid
		}
	}
	return args, nil
}

func prepareStatus(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if _, err := readArgs(raw, "worktree"); err != nil {
		return nil, err
	}
	return raw, nil
}

func prepareDiff(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	args, err := readArgs(raw, "path", "scope", "worktree")
	if err != nil {
		return nil, err
	}
	if _, present := args["scope"]; !present {
		args["scope"] = json.RawMessage(`"all"`)
	}
	return json.Marshal(args)
}
