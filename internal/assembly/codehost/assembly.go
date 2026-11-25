package codehost

import (
	codeindex "github.com/sailry/sailry-code/modules/code/code-index/node"
	databasesnode "github.com/sailry/sailry-code/modules/code/databases/node"
	databasesservice "github.com/sailry/sailry-code/modules/code/databases/service"
	gitnode "github.com/sailry/sailry-code/modules/code/git/node"
	sshnode "github.com/sailry/sailry-code/modules/code/ssh/node"
	sshservice "github.com/sailry/sailry-code/modules/code/ssh/service"
	terminalnode "github.com/sailry/sailry-code/modules/code/terminal/node"
	worktreesnode "github.com/sailry/sailry-code/modules/code/worktrees/node"
	"github.com/sailry/sailry-harness/sdk/native/node"
)

func Assembly() node.Assembly {
	assembly := node.HarnessAssembly()
	assembly.BundledDefaults = defaultPlugins()
	assembly.CodeIndex = codeindex.NewService
	assembly.Databases = databasesservice.ExecuteOperation
	assembly.SSH = sshservice.New
	assembly.Terminal = node.NewTerminalService
	assembly.Modules = append(assembly.Modules,
		gitnode.Module(),
		worktreesnode.Module(),
		codeindex.Module(),
		databasesnode.Module(),
		sshnode.Module(),
		terminalnode.Module(),
	)
	return assembly
}
