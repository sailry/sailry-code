package desktop_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	support "github.com/sailry/sailry-code/internal/testsupport/desktop"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/native/node"
)

type sshLayout struct {
	Width  float32 `json:"width"`
	Folded bool    `json:"folded,omitempty"`
}

// sshPreferences are a profile's rail choices and a window's workspace
// layouts as they were saved before the window starts.
type sshPreferences struct {
	order   []string
	pins    map[string]bool
	layouts map[string]sshLayout
}

// The Code SSH page takes over the rail position, pin and workspace layout
// of the retired official ssh package on the profile's own Node. A package
// on another host or a same-name package that is not that release keeps its
// own preferences, and the page's own saved choices are never replaced.
func TestCodeSSHLegacyPackagePreferences(t *testing.T) {
	page := codeSSHFeature(t)
	const layout = "/ssh-workspace"
	other := "plugin:" + "0123456789abcdef0123456789abcdef" + "/ssh"

	t.Run("VerifiedLocalPackage", func(t *testing.T) {
		var old string
		w := newCodeSSHPreferenceWindow(t, func(local string, _ *node.Node) sshPreferences {
			old = "plugin:" + local + "/ssh"
			return sshPreferences{
				order: []string{"plugin:" + local + "/files", old, "plugin:" + local + "/git"},
				pins:  map[string]bool{old: false},
			}
		})
		w.inheritVerifiedSSHPackage()
		order, pins := w.App.FeaturePreferences()
		if pins[page] || slices.Index(order, page) != slices.Index(order, old)+1 {
			t.Fatalf("the SSH page did not take the legacy package's place and pin: order %v pins %v", order, pins)
		}
		w.Await("an unpinned SSH page remained on the rail", func() bool { return !w.railShows(codeSSHCatalog().In("en", "ssh")) })
	})

	t.Run("VerifiedLocalLayout", func(t *testing.T) {
		peer := newCodeSSHPeer(t)
		var old string
		w := newCodeSSHPreferenceWindow(t, func(local string, _ *node.Node) sshPreferences {
			old = "plugin:" + local + "/ssh"
			return sshPreferences{
				pins:    map[string]bool{old: true},
				layouts: map[string]sshLayout{old + layout: {Width: 345, Folded: true}, other + layout: {Width: 390}},
			}
		})
		w.inheritVerifiedSSHPackage()
		if got := w.sshLayouts()[page+layout]; got != (sshLayout{Width: 345, Folded: true}) {
			t.Fatalf("the SSH workspace layout = %+v, want the legacy package's", got)
		}
		profile := w.trustedSSHProfile("Layout SSH connection", peer)
		w.openSSHPage()
		w.openSSHTerminal(profile)
		toggle := w.control(ui.RoleButton, sshText("ssh_assistant"))
		if w.Tester.HasText(support.Label("chat_connection_history")) {
			t.Fatal("the inherited folded details column showed the SSH assistant")
		}
		w.Click(toggle)
		w.checkSSHResourceEntry()
		if got := w.sshLayouts()[page+layout]; got != (sshLayout{Width: 345}) {
			t.Fatalf("unfolding the SSH assistant saved %+v, want the inherited width unfolded", got)
		}
		// The details toggle keeps the legacy page's localized label, never its key.
		w.Tester.SetLocale("zh-CN")
		w.Await("the SSH details toggle did not show the localized assistant label", func() bool {
			return w.hasControl(ui.RoleButton, "SSH 助手") && !w.hasControl(ui.RoleButton, "ssh_assistant")
		})
	})

	t.Run("AnotherHost", func(t *testing.T) {
		w := newCodeSSHPreferenceWindow(t, func(local string, _ *node.Node) sshPreferences {
			return sshPreferences{
				order:   []string{other, "plugin:" + local + "/git"},
				pins:    map[string]bool{other: false},
				layouts: map[string]sshLayout{other + layout: {Width: 390, Folded: true}},
			}
		})
		w.inheritVerifiedSSHPackage()
		if _, pins := w.App.FeaturePreferences(); !pins[page] || pins[other] {
			t.Fatalf("another host's ssh package changed the SSH page's pin: %v", pins)
		}
		layouts := w.sshLayouts()
		if _, inherited := layouts[page+layout]; inherited || layouts[other+layout] != (sshLayout{Width: 390, Folded: true}) {
			t.Fatalf("another host's ssh workspace was moved: %+v", layouts)
		}
		w.Await("the pinned SSH page is missing from the rail", func() bool { return w.railShows(codeSSHCatalog().In("en", "ssh")) })
	})

	t.Run("SameNameThirdParty", func(t *testing.T) {
		var old string
		w := newCodeSSHPreferenceWindow(t, func(local string, owner *node.Node) sshPreferences {
			installSSHPackageFixture(t, t.Context(), owner)
			old = "plugin:" + local + "/ssh"
			return sshPreferences{
				order:   []string{old, "plugin:" + local + "/git"},
				pins:    map[string]bool{old: false},
				layouts: map[string]sshLayout{old + layout: {Width: 345, Folded: true}},
			}
		})
		// Startup saw the package: it is not the verified release, so nothing
		// is bound, whatever its name.
		order, pins := w.App.FeaturePreferences()
		if !pins[page] || slices.Index(order, page) == slices.Index(order, old)+1 {
			t.Fatalf("a same-name third-party package lent the SSH page its rail choices: order %v pins %v", order, pins)
		}
		layouts := w.sshLayouts()
		if _, inherited := layouts[page+layout]; inherited || layouts[old+layout] != (sshLayout{Width: 345, Folded: true}) {
			t.Fatalf("a same-name third-party package's workspace was reused or changed: %+v", layouts)
		}
		w.Await("the pinned SSH page is missing from the rail", func() bool { return w.railShows(codeSSHCatalog().In("en", "ssh")) })
	})

	t.Run("ExplicitPagePreferences", func(t *testing.T) {
		var old string
		w := newCodeSSHPreferenceWindow(t, func(local string, _ *node.Node) sshPreferences {
			old = "plugin:" + local + "/ssh"
			return sshPreferences{
				order:   []string{page, "plugin:" + local + "/git", old},
				pins:    map[string]bool{old: false, page: true},
				layouts: map[string]sshLayout{old + layout: {Width: 345, Folded: true}, page + layout: {Width: 420}},
			}
		})
		w.inheritVerifiedSSHPackage()
		order, pins := w.App.FeaturePreferences()
		if !pins[page] || pins[old] || slices.Index(order, page) > slices.Index(order, old) {
			t.Fatalf("the legacy package replaced the SSH page's own rail choices: order %v pins %v", order, pins)
		}
		if layouts := w.sshLayouts(); layouts[page+layout] != (sshLayout{Width: 420}) || layouts[old+layout] != (sshLayout{Width: 345, Folded: true}) {
			t.Fatalf("the legacy package replaced the SSH page's own layout or lost its own: %+v", layouts)
		}
	})
}

// TestCodeSSHLegacyReleasePreferences installs a real official ssh release
// and lets the window's own startup recognize it. Extract the release with
//
//	git -C <plugins> archive <commit> ssh | tar -x -C <dir>
//
// and set SAILRY_TEST_LEGACY_SSH_DIR to <dir>/ssh. Ordinary runs skip it.
func TestCodeSSHLegacyReleasePreferences(t *testing.T) {
	release := os.Getenv("SAILRY_TEST_LEGACY_SSH_DIR")
	if release == "" {
		t.Skip("SAILRY_TEST_LEGACY_SSH_DIR names no official ssh release")
	}
	page := codeSSHFeature(t)
	const layout = "/ssh-workspace"
	install := func(t *testing.T, owner *node.Node) {
		t.Helper()
		info := installSSHPackageTree(t, t.Context(), owner, release)
		t.Logf("installed ssh with digest %s", info.Summary.Digest)
	}
	rail := codeSSHCatalog().In("en", "ssh")

	t.Run("UnpinnedInOrder", func(t *testing.T) {
		var old string
		w := newCodeSSHPreferenceWindow(t, func(local string, owner *node.Node) sshPreferences {
			install(t, owner)
			old = "plugin:" + local + "/ssh"
			return sshPreferences{order: []string{"plugin:" + local + "/files", old, "plugin:" + local + "/git"}, pins: map[string]bool{old: false}}
		})
		order, pins := w.App.FeaturePreferences()
		if pins[page] || slices.Index(order, page) != slices.Index(order, old)+1 {
			t.Fatalf("startup did not give the SSH page the release's place and pin: order %v pins %v", order, pins)
		}
		w.Await("an unpinned SSH page remained on the rail", func() bool { return w.railCount(rail) == 0 })
	})

	t.Run("PinnedLayout", func(t *testing.T) {
		peer := newCodeSSHPeer(t)
		var old string
		w := newCodeSSHPreferenceWindow(t, func(local string, owner *node.Node) sshPreferences {
			install(t, owner)
			old = "plugin:" + local + "/ssh"
			return sshPreferences{
				order:   []string{"plugin:" + local + "/files", old},
				pins:    map[string]bool{old: true},
				layouts: map[string]sshLayout{old + layout: {Width: 345, Folded: true}},
			}
		})
		if order, pins := w.App.FeaturePreferences(); !pins[page] || slices.Index(order, page) != slices.Index(order, old)+1 {
			t.Fatalf("startup did not give the SSH page the release's place and pin: order %v pins %v", order, pins)
		}
		if got := w.sshLayouts()[page+layout]; got != (sshLayout{Width: 345, Folded: true}) {
			t.Fatalf("the SSH workspace layout = %+v, want the release's", got)
		}
		// The release's own navigation is replaced, not mounted beside the page.
		w.Await("the rail did not show exactly one SSH entry", func() bool { return w.railCount(rail) == 1 })
		profile := w.trustedSSHProfile("Release SSH connection", peer)
		w.openSSHPage()
		w.openSSHTerminal(profile)
		toggle := w.control(ui.RoleButton, sshText("ssh_assistant"))
		if w.Tester.HasText(support.Label("chat_connection_history")) {
			t.Fatal("the release's folded details column showed the SSH assistant")
		}
		w.Click(toggle)
		w.checkSSHResourceEntry()
		if got := w.sshLayouts()[page+layout]; got != (sshLayout{Width: 345}) {
			t.Fatalf("unfolding the SSH assistant saved %+v, want the release's width unfolded", got)
		}
		w.Tester.SetLocale("zh-CN")
		w.Await("the SSH details toggle did not show the localized assistant label", func() bool {
			return w.hasControl(ui.RoleButton, "SSH 助手") && !w.hasControl(ui.RoleButton, "ssh_assistant")
		})
	})
}

func codeSSHFeature(t *testing.T) string {
	t.Helper()
	for _, page := range sshDesktop().WorkspacePages {
		if page.Feature.LegacyPackage == "ssh" {
			if page.Feature.ID != string(page.Page) {
				t.Fatalf("the SSH rail feature %q is not its page %q", page.Feature.ID, page.Page)
			}
			return page.Feature.ID
		}
	}
	t.Fatal("the SSH module has no page replacing the ssh package")
	return ""
}

type codeSSHPreferenceWindow struct {
	*codeSSHWindow
	saved sshPreferences
}

// newCodeSSHPreferenceWindow starts the Code window over a profile whose
// preferences the prepare callback wrote. Window layouts are applied after the
// window exists, in the order the application applies its saved state.
func newCodeSSHPreferenceWindow(t *testing.T, prepare func(local string, owner *node.Node) sshPreferences) *codeSSHPreferenceWindow {
	t.Helper()
	n := support.StartNode(t, sshHost())
	// The profile's preference document is the one Desktop itself saves;
	// only its rail choices are the ones under test.
	first, err := fixtures.Connect(n.Ctx, n.Owner, n.Profile, sshDesktop())
	if err != nil {
		t.Fatal(err)
	}
	saveErr := first.Preferences.Save()
	if err := errors.Join(saveErr, first.Close()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(n.Profile, "desktop", "preferences.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	saved := prepare(n.Owner.ID().String(), n.Owner)
	for key, value := range map[string]any{"feature_order": saved.order, "feature_pins": saved.pins} {
		if document[key], err = json.Marshal(value); err != nil {
			t.Fatal(err)
		}
	}
	if body, err = json.Marshal(document); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	window := n.Connect(sshDesktop())
	if !window.Preferences.Readable {
		t.Fatalf("the saved profile preferences were not read: %v", window.Preferences.Err)
	}
	w := &codeSSHPreferenceWindow{codeSSHWindow: &codeSSHWindow{window}, saved: saved}
	w.applyLayouts()
	return w
}

// applyLayouts restores the window's saved workspace layouts.
func (w *codeSSHPreferenceWindow) applyLayouts() {
	w.T.Helper()
	prefs := w.App.Prefs()
	body, err := json.Marshal(map[string]any{"pluginWorkspaces": w.saved.layouts})
	if err != nil {
		w.T.Fatal(err)
	}
	if err := json.Unmarshal(body, &prefs); err != nil {
		w.T.Fatal(err)
	}
	w.App.SetPrefs(prefs)
}

// inheritVerifiedSSHPackage replays startup for a profile whose own Node has
// the verified official ssh release: the Harness binds that installation
// before it applies the profile's rail choices and the window's layouts. The
// release's digest check is the Harness's and is tested there.
func (w *codeSSHPreferenceWindow) inheritVerifiedSSHPackage() {
	w.T.Helper()
	w.App.InheritPackagePreferences(w.Owner.ID().String(), "ssh")
	w.App.SetFeaturePreferences(w.saved.order, w.saved.pins)
	w.applyLayouts()
}

func (w *codeSSHPreferenceWindow) sshLayouts() map[string]sshLayout {
	w.T.Helper()
	body, err := json.Marshal(w.App.Prefs())
	if err != nil {
		w.T.Fatal(err)
	}
	var saved struct {
		Layouts map[string]sshLayout `json:"pluginWorkspaces"`
	}
	if err := json.Unmarshal(body, &saved); err != nil {
		w.T.Fatal(err)
	}
	return saved.Layouts
}

func (w *codeSSHPreferenceWindow) railShows(name string) bool { return w.railCount(name) != 0 }

func (w *codeSSHPreferenceWindow) railCount(name string) int {
	count := 0
	for _, entry := range w.Tester.AccessAll() {
		if entry.Role == ui.RoleButton && entry.Name == name && entry.Bounds.W > 0 && entry.Bounds.X+entry.Bounds.W <= native.RailWidth {
			count++
		}
	}
	return count
}
