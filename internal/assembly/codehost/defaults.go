package codehost

import "github.com/sailry/sailry-harness/sdk/native/node"

func defaultPlugins() []node.DefaultPlugin {
	var selected []node.DefaultPlugin
	for _, name := range []string{
		"reminders", "scheduled-tasks", "goals", "commands", "brief", "files", "statistics", "provider-status",
		"openai-auth", "claude-auth", "copilot-auth", "workbuddy", "databases", "ssh", "worktrees", "git", "context7", "github", "office",
	} {
		selected = append(selected, node.DefaultPlugin{Name: name, Enabled: true})
	}
	return selected
}
