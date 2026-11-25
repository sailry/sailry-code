// Package worktreesnode provides the compile-time native Node Git worktree
// management agent tools.
package worktreesnode

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

// Module returns the native Node module providing worktree agent tools.
func Module() native.Module { return module{} }

func (module) ID() string                { return "worktrees" }
func (module) ReplacesPlugins() []string { return []string{"worktrees"} }

func (module) WorkspaceServices() native.WorkspaceServices {
	return native.WorkspaceServices{Worktrees: true, Bind: git.BindWorkspace(bindWorkspace)}
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
		tool, err := host.WorkspaceTool(inv, native.WorkspaceToolConfig{
			Spec:        spec,
			Instruction: catalog.Instructions,
			PrepareArgs: prepareFor(spec.Name),
		})
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func invalid() error {
	return &protocol.Fault{Code: protocol.ErrorCodeInvalidRequest, Message: "Invalid worktree arguments"}
}

func decodeObject(raw json.RawMessage, allowed []string) (map[string]any, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, invalid()
	}
	for k := range obj {
		if !slices.Contains(allowed, k) {
			return nil, invalid()
		}
	}
	return obj, nil
}

func prepareFor(name string) func(context.Context, json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "list_worktrees":
		return func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			if _, err := decodeObject(raw, nil); err != nil {
				return nil, err
			}
			return json.RawMessage(`{}`), nil
		}
	case "create_worktree":
		return func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			args, err := decodeObject(raw, []string{"branch", "expected_head", "expected_index", "include_changes"})
			if err != nil {
				return nil, err
			}
			branch, ok := args["branch"].(string)
			if !ok {
				return nil, invalid()
			}
			head, ok := args["expected_head"].(string)
			if !ok {
				return nil, invalid()
			}
			index, ok := args["expected_index"].(string)
			if !ok {
				return nil, invalid()
			}
			includeChanges := false
			if v, exists := args["include_changes"]; exists {
				b, ok := v.(bool)
				if !ok {
					return nil, invalid()
				}
				includeChanges = b
			}
			return json.Marshal(map[string]any{
				"branch": branch, "expected_head": head, "expected_index": index, "include_changes": includeChanges,
			})
		}
	case "register_worktree":
		return func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			args, err := decodeObject(raw, []string{"path"})
			if err != nil {
				return nil, err
			}
			path, ok := args["path"].(string)
			if !ok {
				return nil, invalid()
			}
			return json.Marshal(map[string]any{"path": path})
		}
	case "remove_worktree":
		return func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			args, err := decodeObject(raw, []string{"worktree", "expected_head", "expected_branch"})
			if err != nil {
				return nil, err
			}
			wt, ok := args["worktree"].(string)
			if !ok {
				return nil, invalid()
			}
			head, ok := args["expected_head"].(string)
			if !ok {
				return nil, invalid()
			}
			branch, ok := args["expected_branch"].(string)
			if !ok {
				return nil, invalid()
			}
			return json.Marshal(map[string]any{"worktree": wt, "expected_head": head, "expected_branch": branch})
		}
	default:
		return nil
	}
}
