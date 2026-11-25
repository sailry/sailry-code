package terminalnode

import (
	"context"

	"github.com/sailry/sailry-harness/sdk/native/node"
)

type module struct{}

func Module() node.Module { return module{} }

func (module) ID() string                { return "terminal" }
func (module) ReplacesPlugins() []string { return nil }
func (module) Catalog() []node.ToolMeta  { return nil }
func (module) ToolsForTurn(context.Context, node.Host, *node.AgentInvocation) ([]node.Tool, error) {
	return nil, nil
}
