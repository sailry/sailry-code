package terminalnode

import (
	"context"

	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

type module struct{}

var _ node.TerminalServiceProvider = module{}

func Module() node.Module { return module{} }

func (module) ID() string                { return "terminal" }
func (module) ReplacesPlugins() []string { return nil }
func (module) Catalog() []node.ToolMeta  { return nil }
func (module) NewTerminalService(ctx context.Context, id protocol.NodeID, changed func(context.Context, protocol.TerminalInfo)) *node.TerminalService {
	return node.NewTerminalService(ctx, id, changed)
}

func (module) ToolsForTurn(context.Context, node.Host, *node.AgentInvocation) ([]node.Tool, error) {
	return nil, nil
}
