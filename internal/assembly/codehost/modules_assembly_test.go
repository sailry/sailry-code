package codehost_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/sailry/sailry-code/internal/testsupport/node"
	filesnode "github.com/sailry/sailry-harness/modules/files/node"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	codehost "github.com/sailry/sailry-code/internal/assembly/codehost"
	native "github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func TestNativeHostAssemblyLifecycleAndOmission(t *testing.T) {
	t.Parallel()
	t.Run("official assembly catalog and retired code package", func(t *testing.T) {
		assembly := codehost.Assembly()
		ids := make([]string, 0, len(assembly.Modules))
		totalTools := 0
		for _, m := range assembly.Modules {
			ids = append(ids, m.ID())
			totalTools += len(m.Catalog())
		}
		for _, want := range []string{"files", "ocr", "git", "worktrees", "web-search", "databases", "ssh", "memory", "browser", "external-browser", "computer"} {
			if !slices.Contains(ids, want) {
				t.Errorf("official host assembly missing module %q (got %v)", want, ids)
			}
		}
		if totalTools < 120 {
			t.Fatalf("unexpected catalog tool count = %d", totalTools)
		}
		// Legacy alias lookup resolves bare names and former plugin_<hash>_* aliases.
		if meta, ok := assembly.LookupTool("read_file"); !ok || meta.Display == nil || meta.Display.Locales["zh-CN"] == "" {
			t.Fatalf("bare tool presentation missing for read_file: %+v %v", meta, ok)
		}
		if meta, ok := assembly.LookupTool(sdktest.Alias("files", "read_file")); !ok || meta.Name != "read_file" {
			t.Fatalf("legacy alias lookup failed for files/read_file: %+v %v", meta, ok)
		}
		if meta, ok := assembly.LookupTool(sdktest.Alias("external-browser", "browser_navigate")); !ok || meta.Name != "external_browser_navigate" {
			t.Fatalf("legacy alias lookup failed for external-browser/browser_navigate: %+v %v", meta, ok)
		}
		if meta, ok := assembly.LookupTool("desktop_snapshot"); !ok || meta.Display == nil || meta.Display.Locales["zh-CN"] == "" {
			t.Fatalf("bare tool presentation missing for desktop_snapshot: %+v %v", meta, ok)
		}
		// Core-adjacent packages stay retired; office remains an optional skills package.
		for _, retired := range []string{"code", "web-search", "external-browser", "browser", "computer", "memory"} {
			if !slices.Contains(sdktest.RetiredBundled(), retired) {
				t.Fatalf("%s is not in RetiredBundled: %v", retired, sdktest.RetiredBundled())
			}
		}
		if slices.Contains(sdktest.RetiredBundled(), "office") {
			t.Fatalf("office must not be retired (it provides Office skills): %v", sdktest.RetiredBundled())
		}
	})

	t.Run("native tools run without UI plugins and honor legacy aliases", func(t *testing.T) {
		legacyRead := sdktest.Alias("files", "read_file")
		legacyGit := sdktest.Alias("git", "git_status")
		legacyMemSave := sdktest.Alias("memory", "save_memory")
		legacyMemSearch := sdktest.Alias("memory", "search_memory")
		script := &mediaScript{
			results: map[string]string{},
			steps: [][][3]string{
				{
					{"discover", "tool_search", `{"query":"select:` + legacyGit + `,list_ocr_models,database_connections,ssh_connections"}`},
				},
				{
					{"read-bare", "read_file", `{"path":"hello.txt"}`},
					{"read-legacy", legacyRead, `{"path":"second.txt"}`},
					{"git", "git_status", `{}`},
					{"ocr", "list_ocr_models", `{}`},
					{"db", "database_connections", `{}`},
					{"ssh", "ssh_connections", `{}`},
					{"mem-save", legacyMemSave, `{"kind":"user","title":"Editor style","body":"Prefer compact tables in summaries"}`},
				},
				{
					{"mem-search", legacyMemSearch, `{"query":"compact tables"}`},
				},
			},
		}
		env := process.NewEnvironment([]string{"PATH=/usr/bin:/bin"})
		profileDir := filepath.Join(t.TempDir(), "profile")
		n, err := native.StartWithOptions(t.Context(), profileDir, native.Options{
			Assembly:    codehost.Assembly(),
			Environment: &env,
			Model: func(context.Context, sdktest.Binding) (fantasy.LanguageModel, error) {
				return script, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer n.Stop(context.Background())

		installed := nodetest.Command(t, n, protocol.Command{Kind: "list_plugins"}).Data.([]protocol.PluginSummary)
		if len(installed) != 0 {
			t.Fatalf("Code preinstalled packages: %+v", installed)
		}
		snap := nodetest.Snapshot(t, n)
		if !slices.ContainsFunc(snap.NativeSkills, func(s protocol.NativeSkill) bool { return s.Package == "web-search" && s.Skill.Name == "research" }) {
			t.Fatalf("native web-search:research skill missing from snapshot: %+v", snap.NativeSkills)
		}
		// Native skill document reads serve the embedded SKILL.md via read_skill_document.
		skillDoc := nodetest.Command(t, n, protocol.Command{
			Kind: "read_skill_document",
			Data: protocol.CommandReadSkillDocumentPayload{
				Source: protocol.PluginSkillsDocumentSource{
					Kind: "bundled",
					Data: protocol.PluginSkillsDocumentSourceBundledPayload{Package: "web-search", Skill: "research"},
				},
			},
		}).Data.(protocol.PluginSkillsDocument)
		if skillDoc.Skill.Name != "research" || strings.TrimSpace(skillDoc.Content) == "" {
			t.Fatalf("read_skill_document returned unexpected native skill doc: %+v", skillDoc)
		}
		// Remote callers cannot spoof the internal native memory reference on read_plugin_value.
		remoteCaller := protocol.NodeID{99}
		spoofReq := protocol.NewRequest(n.ID(), protocol.Command{
			Kind: "read_plugin_value",
			Data: &protocol.CommandReadPluginValuePayload{Key: "memory/settings", Scope: protocol.PluginStorageScopeNode},
		})
		spoofReq.Plugin = &protocol.PluginContext{Surface: protocol.PluginDesktopSurface{Kind: "settings"}, Package: sdktest.NativeMemoryReference()}
		var spoofFault *protocol.Fault
		if _, err := n.Dispatch(t.Context(), remoteCaller, spoofReq); !errors.As(err, &spoofFault) || spoofFault.Code != protocol.ErrorCodePermissionDenied {
			t.Fatalf("remote caller spoofing NativeMemoryReference got err = %v, want permission_denied", err)
		}
		compCfg := nodetest.Command(t, n, protocol.Command{Kind: "read_computer_settings"}).Data.(protocol.ComputerSettings)
		if compCfg.Enabled {
			t.Fatalf("native computer should default disabled: %+v", compCfg)
		}
		savedComp := nodetest.Command(t, n, protocol.Command{Kind: "save_computer_settings", Data: protocol.ComputerSettings{Revision: compCfg.Revision, Enabled: true}}).Data.(protocol.ComputerSettings)
		if !savedComp.Enabled || savedComp.Revision <= compCfg.Revision {
			t.Fatalf("save_computer_settings failed: %+v", savedComp)
		}
		nodetest.Command(t, n, protocol.Command{Kind: "save_computer_settings", Data: protocol.ComputerSettings{Revision: savedComp.Revision, Enabled: false}})

		extCfg := nodetest.Command(t, n, protocol.Command{Kind: "read_external_browser_settings"}).Data.(protocol.ExternalBrowserSettings)
		if extCfg.Enabled {
			t.Fatalf("native external browser should default disabled: %+v", extCfg)
		}
		savedExt := nodetest.Command(t, n, protocol.Command{Kind: "save_external_browser_settings", Data: protocol.ExternalBrowserSettings{Revision: extCfg.Revision, Enabled: true}}).Data.(protocol.ExternalBrowserSettings)
		if !savedExt.Enabled || savedExt.Revision <= extCfg.Revision {
			t.Fatalf("save_external_browser_settings failed: %+v", savedExt)
		}
		nodetest.Command(t, n, protocol.Command{Kind: "save_external_browser_settings", Data: protocol.ExternalBrowserSettings{Revision: savedExt.Revision, Enabled: false}})

		memEntry := nodetest.Command(t, n, protocol.Command{Kind: "save_memory_entry", Data: protocol.CommandSaveMemoryEntryPayload{
			Title: "Native note", Kind: "user", Body: "Saved directly via native module command",
		}}).Data.(protocol.MemoryEntry)
		if memEntry.Summary.ID == "" || memEntry.Body != "Saved directly via native module command" {
			t.Fatalf("save_memory_entry returned unexpected entry: %+v", memEntry)
		}
		if afterMemSnap := nodetest.Snapshot(t, n); afterMemSnap.MemoryRevision == 0 {
			t.Fatalf("Snapshot.MemoryRevision stayed 0 after save_memory_entry")
		}
		memCat := nodetest.Command(t, n, protocol.Command{Kind: "read_memory_catalog", Data: protocol.CommandReadMemoryCatalogPayload{View: "active"}}).Data.(protocol.MemoryCatalog)
		if !slices.ContainsFunc(memCat.Entries, func(e protocol.MemorySummary) bool { return e.ID == memEntry.Summary.ID }) {
			t.Fatalf("read_memory_catalog missing saved entry: %+v", memCat.Entries)
		}
		// Add search_memory -> save_memory(id, expected_revision) steps to verify BeforeModel read-authority capture for native memory tools.
		script.steps = append(script.steps,
			[][3]string{{"mem-read-id", "search_memory", `{"query":"Native note"}`}},
			[][3]string{{
				"mem-update", "save_memory",
				`{"id":"` + memEntry.Summary.ID + `","expected_revision":1,"kind":"user","title":"Native note updated","body":"Updated inside turn after BeforeModel read authority"}`,
			}},
		)

		// Disable explicitly installed files and git UI fixtures: native modules must keep serving tools.
		for _, pkg := range []string{"files", "git"} {
			info := nodetest.InstallPlugin(t, n, pkg, map[string]string{
				"plugin.json": `{"$schema":"` + sdktest.ManifestSchema + `","name":"` + pkg + `","version":"0.1.0","extensions":{"dev.sailry.platform":{"api_version":"v1","requires_modules":["` + pkg + `"]}}}`,
			})
			nodetest.Command(t, n, protocol.Command{Kind: "set_plugin_enabled", Data: protocol.CommandSetPluginEnabledPayload{
				Name: pkg, ExpectedRevision: info.Summary.Revision, Enabled: false,
			}})
		}

		secret := protocol.NewSecret("key")
		provider := protocol.DefaultConversationProvider()
		provider.ID, provider.Name, provider.Enabled = protocol.NewProviderID(), "Module fixture", true
		provider.Api, provider.Authentication, provider.Endpoint, provider.DefaultModel = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, "http://127.0.0.1:1/v1", "worker"
		m := protocol.DefaultConversationModel()
		m.ID, m.Context, m.Output, m.Tools, m.CustomCapabilities = "worker", 32000, 8000, true, true
		provider.Models = []protocol.ConversationModel{m}
		nodetest.Command(t, n, protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}})

		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := writeFixtureFile(filepath.Join(root, "hello.txt"), "hello native modules\n"); err != nil {
			t.Fatal(err)
		}
		if err := writeFixtureFile(filepath.Join(root, "second.txt"), "legacy alias content\n"); err != nil {
			t.Fatal(err)
		}
		project := nodetest.Command(t, n, protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
			Name: "Modules", Path: root, Source: protocol.ProjectsSource{Kind: "local"}, Appearance: protocol.DefaultProjectsAppearance(),
		}}).Data.(protocol.Project)
		cfg := protocol.DefaultSessionConfig()
		cfg.Provider, cfg.Model, cfg.Mode, cfg.Permission = provider.ID, "worker", protocol.WorkModeCode, protocol.PermissionFull
		session := nodetest.Command(t, n, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{
			Project: &project.ID, Config: &cfg,
		}}).Data.(protocol.Session)

		changes, err := n.Subscribe(t.Context(), n.ID(), protocol.Topic{Kind: "node"})
		if err != nil {
			t.Fatal(err)
		}
		defer changes.Close()
		nodetest.Command(t, n, protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
			Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Inspect workspace"},
		}})
		deadline := time.Now().Add(30 * time.Second)
		for completed := false; !completed; {
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for native module turn")
			}
			var history protocol.ConversationHistory
			if err := sdktest.Inspect(n).Store().Node.Read(t.Context(), func(db *sql.Conn) error {
				var err error
				history, err = sdktest.ReadConversation(t.Context(), db, session.ID, nil, 100)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for _, run := range history.Page.Runs {
				switch run.Status {
				case protocol.ConversationStatusCompleted:
					completed = true
				case protocol.ConversationStatusFailed, protocol.ConversationStatusInterrupted, protocol.ConversationStatusCancelled:
					t.Fatalf("turn failed: %+v", run)
				}
			}
			if !completed {
				if _, err := changes.Next(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
		}

		script.mu.Lock()
		results := script.results
		offered := script.offered
		script.mu.Unlock()
		if len(offered) == 0 || !slices.Contains(offered[0], "read_file") || !slices.Contains(offered[0], "apply_patch") || !slices.Contains(offered[0], "git_status") || !slices.Contains(offered[0], "git_diff") {
			t.Fatalf("core native coding tools not offered on step 1: %v", offered)
		}
		if !strings.Contains(results["read-bare"], "hello native modules") {
			t.Fatalf("read-bare = %q", results["read-bare"])
		}
		if !strings.Contains(results["read-legacy"], "legacy alias content") {
			t.Fatalf("read-legacy (alias) = %q", results["read-legacy"])
		}
		if !strings.Contains(results["git"], `"kind":"git_status"`) {
			t.Fatalf("git_status = %q", results["git"])
		}
		if !strings.Contains(results["ocr"], `"models"`) {
			t.Fatalf("list_ocr_models = %q", results["ocr"])
		}
		if !strings.Contains(results["db"], `"kind":"database_profiles"`) {
			t.Fatalf("database_connections = %q", results["db"])
		}
		if !strings.Contains(results["ssh"], `"kind":"ssh_profiles"`) {
			t.Fatalf("ssh_connections = %q", results["ssh"])
		}
		if !strings.Contains(results["mem-save"], `"receipt"`) {
			t.Fatalf("save_memory = %q", results["mem-save"])
		}
		if !strings.Contains(results["mem-search"], "Prefer compact tables") {
			t.Fatalf("search_memory = %q", results["mem-search"])
		}
		if !strings.Contains(results["mem-update"], `"revision":2`) {
			t.Fatalf("save_memory update (requires BeforeModel read authority) = %q", results["mem-update"])
		}
		nodetest.Command(t, n, protocol.Command{Kind: "delete_memory_entry", Data: protocol.CommandDeleteMemoryEntryPayload{ID: memEntry.Summary.ID, ExpectedRevision: 2}})

		// Canonical history carries native tool names and localized presentations.
		var state sdktest.AgentState
		if err := sdktest.Inspect(n).Store().Node.Read(t.Context(), func(db *sql.Conn) error {
			var err error
			state, err = sdktest.LoadAgentState(t.Context(), db, session.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		sawReadPresentation := false
		for _, rec := range state.Records {
			if rec.Metadata.Presentation != nil {
				if disp, ok := rec.Metadata.Presentation.Displays["read_file"]; ok && disp.Locales["zh-CN"] != "" {
					sawReadPresentation = true
				}
			}
			if msg := rec.Message(); msg != nil {
				for _, blk := range msg.Content {
					if req, ok := blk.Data.(*native.ToolRequest); ok && req.ID == "read-legacy" && req.Name != "read_file" {
						t.Errorf("legacy alias call was not normalized to read_file: %q", req.Name)
					}
				}
			}
		}
		if !sawReadPresentation {
			t.Error("canonical record did not capture localized presentation for native read_file")
		}
	})

	t.Run("omitting modules removes their native tools", func(t *testing.T) {
		script := &mediaScript{
			results: map[string]string{},
			steps: [][][3]string{
				{{"discover", "tool_search", `{"query":"select:database_connections,ssh_connections,git_status"}`}},
			},
		}
		env := process.NewEnvironment([]string{"PATH=/usr/bin:/bin"})
		profileDir := filepath.Join(t.TempDir(), "profile")
		prepared, err := native.StartWithOptions(t.Context(), profileDir, native.Options{Assembly: codehost.Assembly(), Environment: &env})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Stop(context.Background())

		before := map[string]protocol.PluginInfo{}
		settings := map[string]map[string]any{}
		const content = "---\nname: retained-fixture\ndescription: Preserve explicitly installed package content.\n---\nKeep this local fixture across restarts.\n"
		for _, pkg := range []string{"git", "databases", "ssh"} {
			info := nodetest.InstallPlugin(t, prepared, pkg, map[string]string{
				"plugin.json":                       `{"$schema":"` + sdktest.ManifestSchema + `","name":"` + pkg + `","version":"0.1.0","extensions":{"dev.sailry.platform":{"api_version":"v1","requires_modules":["` + pkg + `"],"settings_schema":"dev.sailry.platform/settings.json","settings_page":{"navigation":{"label":"Settings"}}}}}`,
				"dev.sailry.platform/settings.json": `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"label":{"type":"string"}},"additionalProperties":false}`,
				"skills/retained-fixture/SKILL.md":  content,
			})
			value := "saved " + pkg + " setting"
			saved := nodetest.Do[struct {
				Values map[string]string `json:"values"`
			}](t, prepared, nodetest.DecodeCommand(t, "save_plugin_settings", map[string]any{
				"package": sdktest.PackageReference(info), "values": map[string]string{"label": value}, "secrets": map[string]any{},
			}))
			if saved.Values["label"] != value {
				t.Fatalf("%s setting was not saved: %+v", pkg, saved)
			}
			info = nodetest.Command(t, prepared, protocol.Command{Kind: "read_plugin", Data: protocol.CommandReadPluginPayload{Name: pkg}}).Data.(protocol.PluginInfo)
			nodetest.Command(t, prepared, protocol.Command{Kind: "set_plugin_enabled", Data: protocol.CommandSetPluginEnabledPayload{
				Name: pkg, ExpectedRevision: info.Summary.Revision, Enabled: false,
			}})
			before[pkg] = nodetest.Command(t, prepared, protocol.Command{Kind: "read_plugin", Data: protocol.CommandReadPluginPayload{Name: pkg}}).Data.(protocol.PluginInfo)
			settings[pkg] = nodetest.Do[map[string]any](t, prepared, nodetest.DecodeCommand(t, "read_plugin_settings", map[string]any{
				"package": sdktest.PackageReference(before[pkg]),
			}))
		}
		verifyRetained := func(owner *native.Node, haveModules bool) {
			t.Helper()
			for pkg, original := range before {
				info := nodetest.Command(t, owner, protocol.Command{Kind: "read_plugin", Data: protocol.CommandReadPluginPayload{Name: pkg}}).Data.(protocol.PluginInfo)
				if info.Summary.Enabled || info.Summary.Revision != original.Summary.Revision || info.Summary.Digest != original.Summary.Digest || !reflect.DeepEqual(info.Summary.Version, original.Summary.Version) {
					t.Fatalf("restart changed disabled %s package: %+v, want %+v", pkg, info.Summary, original.Summary)
				}
				reference := sdktest.PackageReference(info)
				state, failure := nodetest.Call[map[string]any](t, owner, nodetest.DecodeCommand(t, "read_plugin_settings", map[string]any{"package": reference}))
				if haveModules {
					if failure != nil {
						t.Fatal(failure)
					}
					if !reflect.DeepEqual(state, settings[pkg]) {
						t.Fatalf("restart changed %s settings: %+v, want %+v", pkg, state, settings[pkg])
					}
				} else {
					nodetest.RequireFault(t, pkg+" settings without its module", failure, protocol.ErrorCodeNotConfigured, "plugin version is incompatible")
				}
				document := nodetest.Do[protocol.PluginSkillsDocument](t, owner, nodetest.DecodeCommand(t, "read_skill_document", map[string]any{
					"source": map[string]any{"kind": "installed", "data": map[string]any{"package": reference, "skill": "retained-fixture"}},
				}))
				if document.Content != content {
					t.Fatalf("restart changed %s content: %q", pkg, document.Content)
				}
			}
		}
		verifyRetained(prepared, true)
		if err := prepared.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}

		n, err := native.StartWithOptions(t.Context(), profileDir, native.Options{
			Assembly:    native.Assembly{Modules: []native.Module{filesnode.Module()}, BundledDefaults: []native.DefaultPlugin{}},
			Environment: &env,
			Model:       func(context.Context, sdktest.Binding) (fantasy.LanguageModel, error) { return script, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		defer n.Stop(context.Background())
		verifyRetained(n, false)

		secret := protocol.NewSecret("key")
		provider := protocol.DefaultConversationProvider()
		provider.ID, provider.Name, provider.Enabled = protocol.NewProviderID(), "Omission fixture", true
		provider.Api, provider.Authentication, provider.Endpoint, provider.DefaultModel = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, "http://127.0.0.1:1/v1", "worker"
		m := protocol.DefaultConversationModel()
		m.ID, m.Context, m.Output, m.Tools, m.CustomCapabilities = "worker", 32000, 8000, true, true
		provider.Models = []protocol.ConversationModel{m}
		nodetest.Command(t, n, protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}})
		root, _ := filepath.EvalSymlinks(t.TempDir())
		project := nodetest.Command(t, n, protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
			Name: "Omission", Path: root, Source: protocol.ProjectsSource{Kind: "local"}, Appearance: protocol.DefaultProjectsAppearance(),
		}}).Data.(protocol.Project)
		cfg := protocol.DefaultSessionConfig()
		cfg.Provider, cfg.Model, cfg.Mode, cfg.Permission = provider.ID, "worker", protocol.WorkModeCode, protocol.PermissionFull
		session := nodetest.Command(t, n, protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{
			Project: &project.ID, Config: &cfg,
		}}).Data.(protocol.Session)
		changes, err := n.Subscribe(t.Context(), n.ID(), protocol.Topic{Kind: "node"})
		if err != nil {
			t.Fatal(err)
		}
		defer changes.Close()
		nodetest.Command(t, n, protocol.Command{Kind: "submit_turn", Data: protocol.CommandSubmitTurnPayload{
			Session: session.ID, ExpectedRevision: session.Revision, Message: protocol.ConversationInput{Text: "Check tools"},
		}})
		for completed := false; !completed; {
			var history protocol.ConversationHistory
			_ = sdktest.Inspect(n).Store().Node.Read(t.Context(), func(db *sql.Conn) error {
				var err error
				history, err = sdktest.ReadConversation(t.Context(), db, session.ID, nil, 100)
				return err
			})
			for _, run := range history.Page.Runs {
				if run.Status == protocol.ConversationStatusCompleted {
					completed = true
				}
			}
			if !completed {
				_, _ = changes.Next(t.Context())
			}
		}
		script.mu.Lock()
		offered, discover := script.offered, script.results["discover"]
		script.mu.Unlock()
		if len(offered) == 0 || !slices.Contains(offered[0], "read_file") {
			t.Fatalf("assembled files module tool read_file missing: %v", offered)
		}
		var found struct {
			Tools []any `json:"tools"`
		}
		if json.Unmarshal([]byte(discover), &found) == nil && len(found.Tools) != 0 {
			t.Fatalf("omitted modules still exposed tools: %s", discover)
		}
		if err := n.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		restored, err := native.StartWithOptions(t.Context(), profileDir, native.Options{Assembly: codehost.Assembly(), Environment: &env})
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Stop(context.Background())
		verifyRetained(restored, true)
	})
}

func writeFixtureFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
