package terminalnode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sailry/sailry-code/internal/testsupport/node"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestNativePythonSkill(t *testing.T) {
	const pkg, skill, skillPath = "commands", "python", "skills/python/SKILL.md"
	const key = pkg + ":" + skill
	document, err := skillsFS.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(executable), "python", "python-runtime.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	wantPython := err == nil
	command := `if [ -n "$SAILRY_PYTHON" ]; then printf '%s\n' "$SAILRY_PYTHON"; "$SAILRY_PYTHON" --version; else echo SAILRY_PYTHON-unset; fi`
	if runtime.GOOS == "windows" {
		command = `if defined SAILRY_PYTHON (echo %SAILRY_PYTHON%& "%SAILRY_PYTHON%" --version) else (echo SAILRY_PYTHON-unset)`
	}
	for _, scenario := range []struct {
		name      string
		installed bool
	}{{"fresh", false}, {"same-name installed package", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			script := nodetest.NewScript(
				nodetest.Step{ID: "read", Tool: "read_skill_resource", Arguments: nodetest.Arguments(map[string]string{"skill": key, "path": "SKILL.md"})},
				nodetest.Step{ID: "load", Tool: "load_skill", Arguments: nodetest.Arguments(map[string]string{"skill": key})},
				nodetest.Step{ID: "probe", Tool: "run_command", Arguments: nodetest.Arguments(map[string]string{"command": command, "access": "unrestricted"})},
				nodetest.Step{ID: "answer"},
			)
			assembly := native.Assembly{Modules: []native.Module{Module()}}
			owner := nodetest.Start(t, filepath.Join(t.TempDir(), "profile"), script, native.Options{ProductID: "code", Assembly: assembly})
			t.Cleanup(func() {
				if err := owner.Stop(context.Background()); err != nil {
					t.Error(err)
				}
			})
			want := string(document)
			var installed []protocol.PluginReference
			if scenario.installed {
				want = "---\nname: python\ndescription: Third-party Python skill\n---\nInstalled Python instructions.\n"
				info := nodetest.InstallPlugin(t, owner, pkg, map[string]string{
					"plugin.json": `{"$schema":"` + sdktest.ManifestSchema + `","name":"commands","version":"1.0.0"}`,
					skillPath:     want,
				})
				installed = []protocol.PluginReference{sdktest.PackageReference(info)}
			}
			snapshot := nodetest.Snapshot(t, owner)
			if skills := snapshot.NativeSkills; len(skills) != 1 || skills[0].Package != pkg || skills[0].Skill.Name != skill || skills[0].Skill.Path != skillPath || skills[0].Skill.DisplayLocales["zh-CN"] != "Python" {
				t.Fatalf("native skill discovery: %+v", skills)
			}
			if !scenario.installed {
				value := nodetest.Do[protocol.PluginSkillsDocument](t, owner, protocol.Command{Kind: "read_skill_document", Data: protocol.CommandReadSkillDocumentPayload{
					Source: protocol.PluginSkillsDocumentSource{Kind: "bundled", Data: protocol.PluginSkillsDocumentSourceBundledPayload{Package: pkg, Skill: skill}},
				}})
				if value.Skill.Name != skill || value.Content != want || len(snapshot.Plugins) != 0 {
					t.Fatal("native skill was not readable without installing a plugin")
				}
			}

			project := nodetest.Do[protocol.Project](t, owner, protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
				Name: "Python fixture", Path: t.TempDir(), Source: protocol.ProjectsSource{Kind: "local"}, Appearance: protocol.DefaultProjectsAppearance(),
			}})
			session := nodetest.ProjectSession(t, owner, project.ID, protocol.PermissionAsk)
			approvals := 0
			frozen, results := nodetest.Turn(t, owner, session, "Inspect the Python skill", nodetest.Wait, func(approval protocol.ConversationApproval) {
				approvals++
				if approval.Access == nil || string(*approval.Access) != "unrestricted" {
					t.Fatalf("unexpected approval: %+v", approval)
				}
			})
			if approvals != 1 || !slices.Equal(frozen, installed) {
				t.Fatalf("turn had %d approvals and captures %+v, want one command approval and captures %+v", approvals, frozen, installed)
			}
			read := nodetest.Result(t, results, "read", "read_skill_resource")
			load := nodetest.Result(t, results, "load", "load_skill")
			directory, _ := load["directory"].(string)
			if read["content"] != want || load["content"] != want || read["skill"] != key || load["skill"] != key || read["directory"] != directory || !filepath.IsAbs(directory) {
				t.Fatal("skill tools did not return the expected captured document and directory")
			}
			if _, exists := read["python"]; exists {
				t.Fatal("read_skill_resource unexpectedly prepared Python")
			}
			interpreter, exists := load["python"]
			wantOutput := "SAILRY_PYTHON-unset"
			if wantPython {
				path, _ := interpreter.(string)
				if !filepath.IsAbs(path) {
					t.Fatalf("bundled interpreter was not prepared: %v", interpreter)
				}
				wantOutput = path + "\nPython 3.12.13"
			} else if !exists || interpreter != nil {
				t.Fatalf("load_skill returned %v, want explicit null for missing bundled Python", interpreter)
			}
			probe := nodetest.Result(t, results, "probe", "run_command")
			data, _ := probe["data"].(map[string]any)
			stdout, _ := data["stdout"].(map[string]any)
			text, _ := stdout["text"].(string)
			if strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")) != wantOutput {
				t.Fatalf("run_command returned %+v, want stdout %q", probe, wantOutput)
			}
		})
	}
}
