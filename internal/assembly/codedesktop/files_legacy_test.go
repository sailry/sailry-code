package codedesktop_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	"github.com/sailry/sailry-code/internal/assembly/codehost"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	"github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

func installOfficialFiles(t *testing.T, assembly node.Assembly) (*support.Node, protocol.PluginSummary) {
	t.Helper()
	release := os.Getenv("SAILRY_TEST_LEGACY_FILES_DIR")
	if release == "" {
		t.Skip("SAILRY_TEST_LEGACY_FILES_DIR names no official Files release")
	}
	n := support.StartNode(t, assembly)
	access := sdktest.Inspect(n.Owner)
	info, err := access.InstallPlugin(n.Ctx, release, "", "files")
	if err != nil || len(info.Issues) != 0 {
		t.Fatalf("the Files package could not be read: %v; issues: %+v", err, info.Issues)
	}
	if err := access.Store().Node.Write(n.Ctx, func(db *sql.Conn) error {
		var err error
		info, err = sdktest.InstallPlugin(n.Ctx, db, info, 0, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return n, info.Summary
}

func TestLegacyFilesReleaseRoutingAndPreferences(t *testing.T) {
	n, before := installOfficialFiles(t, node.HarnessAssembly())
	old := "plugin:" + n.Owner.ID().String() + "/files"
	page := string(native.FilesPage)
	harness := n.Connect(fixtures.HarnessAssembly())
	harness.App.SetWelcome(true)
	if harness.App.NativePageEnabled(native.FilesPage) {
		t.Fatal("the Harness profile gained an uninstalled native Files page")
	}
	harness.Await("Harness did not show the installed Files rail entry", func() bool {
		_, ok := harness.Tester.AccessByRole(ui.RoleButton, support.Label("files"))
		return ok
	})
	project := harness.CreateProject("Legacy Files project", t.TempDir())
	harness.OpenProjectSession(project.ID)
	harness.App.Do(old)
	harness.Await("the installed package did not own the Harness Files route", func() bool {
		return harness.App.Page == native.Page("page:plugin") && harness.App.PluginFeature() == old
	})
	harness.Preferences.Data.FeatureOrder = new([]string{old})
	harness.Preferences.Data.FeaturePins = new(map[string]bool{old: false})
	if err := harness.Preferences.Save(); err != nil {
		t.Fatal(err)
	}
	code := n.Connect(codedesktop.Assembly())
	var order []string
	var pins map[string]bool
	code.Await("the legacy rail choice was not copied to the native page", func() bool {
		order, pins = code.App.FeaturePreferences()
		return slices.Contains(order, page)
	})
	if !code.App.NativePageEnabled(native.FilesPage) || slices.Index(order, old) < 0 || slices.Index(order, page) != slices.Index(order, old)+1 || pins[page] {
		t.Fatalf("Code did not inherit the official Files entry's place and pin: order %v, pins %v", order, pins)
	}
	legacyLayout := old + "/files-workspace"
	prefs := code.App.Prefs()
	settings := fixtures.PluginWorkspacePrefs{Width: 345, Folded: true}
	prefs.PluginWorkspaces = map[string]fixtures.PluginWorkspacePrefs{legacyLayout: settings}
	code.App.SetPrefs(prefs)
	if choices := code.App.Prefs().PluginWorkspaces; choices[page] != settings || choices[legacyLayout] != settings {
		t.Fatalf("the resource page did not inherit the legacy details width and fold state: %+v", choices)
	}
	installed := code.Controller.Snapshot().Plugins
	if len(installed) != 1 || installed[0].Digest != before.Digest || !installed[0].Enabled {
		t.Fatalf("replacement changed the installed package: %+v", installed)
	}
	code.App.SetWelcome(true)
	code.OpenSession(harness.App.FocusedConversation())
	code.App.Do(old)
	code.Tester.Frame()
	if code.App.Page != native.ConversationPage {
		t.Fatalf("the old Files page remained navigable with Code Files: %q", code.App.Page)
	}
}

func TestLegacyFilesReleaseAndNativeOpenTheSameFile(t *testing.T) {
	for _, host := range []struct {
		name     string
		assembly node.Assembly
	}{{"Harness Node", node.HarnessAssembly()}, {"Code Node", codehost.Assembly()}} {
		t.Run(host.name, func(t *testing.T) { testLegacyAndNativeFiles(t, host.assembly) })
	}
}

func testLegacyAndNativeFiles(t *testing.T, assembly node.Assembly) {
	t.Helper()
	n, before := installOfficialFiles(t, assembly)
	dir := t.TempDir()
	const name = "legacy-native.txt"
	original := []byte("One worktree for both Files workbenches\n")
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, original, 0o600); err != nil {
		t.Fatal(err)
	}
	harness := n.Connect(fixtures.HarnessAssembly())
	harnessRequests := observeFilesFixtureRequests(harness)
	harness.App.SetWelcome(true)
	if harness.App.NativePageEnabled(native.FilesPage) {
		t.Fatal("the legacy test must use the installed Files page in Harness")
	}
	harness.OpenProjectSession(harness.CreateProject("Files parity", dir).ID)
	session := harness.App.FocusedConversation()
	legacyID := "plugin:" + n.Owner.ID().String() + "/files"
	harness.App.Do(legacyID)
	harness.Await("official Files view did not mount its workspace details", func() bool {
		return harness.App.PluginFeature() == legacyID && harness.App.PluginDetails != nil
	})
	harness.App.RevealDetails()
	code := n.Connect(codedesktop.Assembly())
	codeRequests := observeFilesFixtureRequests(code)
	code.App.SetWelcome(true)
	code.OpenSession(session)
	code.App.Navigate(native.FilesPage)
	code.App.RevealDetails()
	openFilesFixtureDocument(code, native.FilesPage, "", name, original, codeRequests)
	openFilesFixtureDocument(harness, native.Page("page:plugin"), legacyID, name, original, harnessRequests)

	legacySaved := saveFilesFixtureDocument(harness, file, original, "[legacy]", harnessRequests)
	code.Await("Code did not show the file saved by the legacy page", func() bool {
		return code.App.Page == native.FilesPage && filesFixtureEditorShows(code, legacySaved)
	})
	nativeSaved := saveFilesFixtureDocument(code, file, legacySaved, "[native]", codeRequests)
	harness.Await("the legacy page did not show the file saved by Code", func() bool {
		return harness.App.PluginFeature() == legacyID && filesFixtureEditorShows(harness, nativeSaved)
	})
	assertFilesFixtureInstallation(harness, before)
	assertFilesFixtureInstallation(code, before)
}
