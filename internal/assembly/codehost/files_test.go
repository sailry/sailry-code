package codehost_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sailry/sailry-code/internal/assembly/codehost"
	nodetest "github.com/sailry/sailry-code/internal/testsupport/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestCodeAgentFileDiscovery(t *testing.T) {
	assembly := codehost.Assembly()
	for _, name := range []string{"code_search", "code_outline", "code_context", "code_impact", "code_map", "code_pipeline"} {
		if _, ok := assembly.LookupTool(name); ok {
			t.Errorf("Code assembly still advertises retired tool %q", name)
		}
	}

	const path = "cmd/server.go"
	const name = "CodeFileDiscoveryFixture"
	root, _ := nodetest.GitProject(t, map[string]string{path: "package main\n\nfunc " + name + "() {}\n"})
	script := nodetest.NewScript(
		nodetest.Step{ID: "search", Tool: "search_files", Arguments: nodetest.Arguments(map[string]any{"query": name, "mode": "content"})},
		nodetest.Step{ID: "answer"},
	)
	owner := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), script, native.Options{Assembly: assembly})
	t.Cleanup(func() {
		if err := owner.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	project := nodetest.Do[protocol.Project](t, owner, protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
		Name: "Code fixture", Path: root, Source: protocol.ProjectsSource{Kind: "local"}, Appearance: protocol.DefaultProjectsAppearance(),
	}})
	session := nodetest.ProjectSession(t, owner, project.ID, protocol.PermissionAsk)
	_, results := nodetest.Turn(t, owner, session, "Find the fixture declaration", nodetest.Wait, nil)
	if result := results["search"]; result == nil || result.Name != "search_files" || result.Status != sdktest.ToolSuccess {
		t.Fatalf("search_files did not succeed: %+v", result)
	}
	value, offered := script.Observation("search")
	if !slices.Contains(offered, "search_files") {
		t.Fatalf("search_files was not offered to the model: %v", offered)
	}
	if !strings.Contains(value, path) || !strings.Contains(value, "func "+name+"()") {
		t.Fatalf("search_files did not find the fixture declaration: %q", value)
	}
}
