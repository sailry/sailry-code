//go:build integration

package codehost_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	codehost "github.com/sailry/sailry-code/internal/assembly/codehost"
	"github.com/sailry/sailry-code/internal/testsupport/node"
	codeindexapi "github.com/sailry/sailry-harness/sdk/native/codeindex"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestCodeAgentToolsWithoutIndex(t *testing.T) {
	pipeline := "code_pipeline"
	read := "read_file"
	requested := []string{pipeline, read, "code_search", "code_outline", "code_context", "code_impact", "code_map"}
	steps := []nodetest.Step{
		nodetest.Step{ID: "discover", Tool: "tool_search", Arguments: nodetest.Arguments(map[string]any{"query": "select:" + strings.Join(requested, ","), "limit": len(requested)})},
		nodetest.Step{ID: "pipeline", Tool: pipeline, Arguments: nodetest.Arguments(json.RawMessage(`{"steps":[{"operation":"files.find","arguments":{"globs":["cmd/server.go"]}},{"operation":"files.read_lines","arguments":{"limit":1},"from_previous":{"path":"/paths/0"}}],"select":"/lines/0/text"}`))},
		nodetest.Step{ID: "answer"},
	}
	script := nodetest.NewScript(steps...)
	assembly := codehost.Assembly()
	assembly.CodeIndex = func(string, codeindexapi.Watch) codeindexapi.Service { return nil }
	node := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), script, native.Options{Assembly: assembly})
	t.Cleanup(func() {
		if err := node.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	root, _ := nodetest.GitProject(t, map[string]string{"cmd/server.go": "package main\n"})
	project := nodetest.Do[protocol.Project](t, node, protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
		Name: "Code fixture", Path: root, Source: protocol.ProjectsSource{Kind: "local"}, Appearance: protocol.DefaultProjectsAppearance(),
	}})
	session := nodetest.ProjectSession(t, node, project.ID, protocol.PermissionAsk)
	frozen, results := nodetest.Turn(t, node, session, "Read the first line of the server source", nodetest.Wait, nil)
	if len(frozen) != 0 {
		t.Fatalf("the native file-only pipeline required package captures: %+v", frozen)
	}

	requests := script.Requests()
	if len(requests) != len(steps) {
		t.Fatalf("the turn made %d model requests, want %d", len(requests), len(steps))
	}
	var initial []string
	for _, tool := range requests[0] {
		name := nodetest.DeclaredToolName(tool)
		if slices.ContainsFunc(requested, func(alias string) bool { return nodetest.DeclaredToolName(alias) == name }) {
			initial = append(initial, name)
		}
	}
	slices.Sort(initial)
	if want := []string{"code_pipeline", "read_file"}; !slices.Equal(initial, want) {
		t.Fatalf("initial request without an index offered %v among the requested tools, want %v", initial, want)
	}
	discovered := nodetest.Result(t, results, "discover", "tool_search")
	if tools, ok := discovered["tools"].([]any); !ok || len(tools) != 0 {
		t.Fatalf("tool discovery without an index returned %v, want an empty tools array", discovered)
	}
	if result := nodetest.Result(t, results, "pipeline", pipeline); strings.TrimSpace(fmt.Sprint(result["value"])) != "package main" {
		t.Fatalf("file-only pipeline returned %v", result)
	}
}
