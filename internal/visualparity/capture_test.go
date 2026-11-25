//go:build visualparity

// Package visualparity captures the Code SSH and terminal surfaces with
// MyGo's headless Tester so a pinned plugin baseline and the native pages can
// be compared frame by frame. It is opt-in through the visualparity build tag;
// run.sh prepares both trees, runs TestCapture in each and then TestCompare.
package visualparity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	"github.com/sailry/sailry-code/internal/assembly/codehost"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	fixtures "github.com/sailry/sailry-harness/sdk/desktop/testing"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/sdktest"
	protocol "github.com/sailry/sailry-harness/sdk/sdktest/protocol"
)

// labels are the SSH package's copy, which the native page must keep.
type labels struct {
	page, empty, create, connections, search, edit, editTitle, remove, removeConfirm, cancel, trust, trustTitle, host string
}

var sshLabels = map[string]labels{
	"en": {page: "SSH", empty: "Open an SSH connection", create: "New connection", connections: "SSH connections", search: "Search",
		edit: "Edit", editTitle: "Edit connection", remove: "Delete", removeConfirm: "Remove connection %s", cancel: "Cancel",
		trust: "Trust", trustTitle: "Trust host key", host: "Host"},
	"zh-CN": {page: "SSH", empty: "打开 SSH 连接", create: "新建连接", connections: "SSH 连接", search: "搜索",
		edit: "编辑", editTitle: "编辑", remove: "删除", removeConfirm: "删除连接 %s", cancel: "取消",
		trust: "信任", trustTitle: "信任主机密钥", host: "地址"},
}

const (
	projectName  = "Parity project"
	loopbackName = "Loopback fixture"
	stagingName  = "Staging web"
)

type config struct {
	out, plugins, fixture string
	locales, themes       []string
	only                  []string
	width, height         int
	scale                 float32
	port                  uint16
}

func readConfig(t *testing.T) config {
	out := os.Getenv("SAILRY_PARITY_OUT")
	if out == "" {
		t.Skip("SAILRY_PARITY_OUT is not set; run internal/visualparity/run.sh")
	}
	list := func(name, fallback string) []string {
		value := os.Getenv(name)
		if value == "" {
			value = fallback
		}
		return strings.Split(value, ",")
	}
	c := config{out: out, plugins: os.Getenv("SAILRY_PARITY_PLUGINS"), locales: list("SAILRY_PARITY_LOCALES", "zh-CN,en"),
		themes: list("SAILRY_PARITY_THEMES", "light,dark"), width: 1280, height: 820, scale: 1, port: 47622,
		fixture: filepath.Join(out, "fixture")}
	if only := os.Getenv("SAILRY_PARITY_ONLY"); only != "" {
		c.only = strings.Split(only, ",")
	}
	if size := os.Getenv("SAILRY_PARITY_SIZE"); size != "" {
		w, h, _ := strings.Cut(size, "x")
		c.width, _ = strconv.Atoi(w)
		c.height, _ = strconv.Atoi(h)
	}
	if scale := os.Getenv("SAILRY_PARITY_SCALE"); scale != "" {
		value, _ := strconv.ParseFloat(scale, 32)
		c.scale = float32(value)
	}
	if port := os.Getenv("SAILRY_PARITY_SSH_PORT"); port != "" {
		value, _ := strconv.ParseUint(port, 10, 16)
		c.port = uint16(value)
	}
	if fixture := os.Getenv("SAILRY_PARITY_FIXTURE"); fixture != "" {
		c.fixture = fixture
	}
	if c.width <= 0 || c.height <= 0 || c.scale <= 0 || c.port == 0 {
		t.Fatalf("invalid SAILRY_PARITY_SIZE, SAILRY_PARITY_SCALE or SAILRY_PARITY_SSH_PORT")
	}
	return c
}

func (c config) mode() string {
	if c.plugins != "" {
		return "plugin"
	}
	return "native"
}

// TestCapture records every state of the SSH page and the terminal entry
// points for each locale and theme. A state that cannot be reached is
// recorded as failed, and the states after it in its group as skipped, so a
// missing native surface is reported instead of hiding the rest.
func TestCapture(t *testing.T) {
	c := readConfig(t)
	if err := os.MkdirAll(c.out, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := startPeer(t, c.port)
	for _, locale := range c.locales {
		for _, mode := range c.themes {
			t.Run(locale+"-"+mode, func(t *testing.T) {
				d := open(t, c, fixture, locale, mode)
				d.sshGroup()
				d.terminalGroup()
			})
		}
	}
}

type driver struct {
	t                   *testing.T
	c                   config
	ctx                 context.Context
	peer                *peer
	locale, theme, mode string
	owner               *node.Node
	window              *fixtures.Window
	tester              *ui.Tester
	project             protocol.Project
	session             protocol.Session
	failure             string
	l                   labels
}

func open(t *testing.T, c config, fixture *peer, locale, mode string) *driver {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	t.Cleanup(cancel)
	// Each window starts from the same paths; only a directory this test
	// created, as its marker shows, is ever cleared.
	marker := filepath.Join(c.fixture, ".sailry-visual-parity-fixture")
	if _, err := os.Stat(c.fixture); err == nil {
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("refusing to clear %s: it has no %s marker", c.fixture, filepath.Base(marker))
		}
		if err := os.RemoveAll(c.fixture); err != nil {
			t.Fatal(err)
		}
	}
	home, project := filepath.Join(c.fixture, "home"), filepath.Join(c.fixture, "project")
	for _, dir := range []string{home, project} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A login /bin/sh reads the system profile and shows the machine's host
	// and user names; this shell shows a fixed prompt on every machine.
	shell := filepath.Join(c.fixture, "parity-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nPS1='parity$ ' exec /bin/bash --noprofile --norc -i\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "README.md"), []byte("parity\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(t.TempDir(), "profile")
	environment := process.NewEnvironment([]string{"PATH=/usr/bin:/bin", "HOME=" + home, "SHELL=" + shell, "PS1=parity$ ", "USER=parity", "LOGNAME=parity", "HOSTNAME=parity", "LANG=en_US.UTF-8", "TERM=xterm-256color"})
	owner, err := node.StartWithOptions(ctx, profile, node.Options{Assembly: codehost.Assembly(), Environment: &environment})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Stop(context.Background()) })
	if c.plugins != "" {
		access := sdktest.Inspect(owner)
		for _, name := range []string{"commands", "ssh"} {
			info, err := access.InstallPlugin(ctx, filepath.Join(c.plugins, name), "", name)
			if err != nil || len(info.Issues) != 0 {
				t.Fatalf("pinned %s package is invalid: %+v, %v", name, info.Issues, err)
			}
			if err := access.Store().Node.Write(ctx, func(db *sql.Conn) error {
				_, err := sdktest.InstallPlugin(ctx, db, info, 0, true)
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	window, err := fixtures.Connect(ctx, owner, profile, codedesktop.Assembly())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = window.Close() })
	// Theme effects, such as the welcome wordmark's shimmer, are frozen as in
	// the acceptance suite: their phase depends on timing, not the surface.
	window.App.NavOpen, window.App.PanelOpen, window.App.Effects = true, false, false
	window.App.Mode = theme.Light
	if mode == "dark" {
		window.App.Mode = theme.Dark
	}
	tester := ui.NewTesterWith(window.App.View, c.width, c.height, ui.TesterOptions{
		Preferences: ui.Preferences{ReduceTransparency: true, ReduceMotion: true}, Dark: mode == "dark",
	})
	tester.SetScale(c.scale)
	tester.SetLocale(locale)
	tester.Frame()
	d := &driver{t: t, c: c, ctx: ctx, peer: fixture, locale: locale, theme: mode, mode: c.mode(), owner: owner, window: window, tester: tester, l: sshLabels[locale]}
	d.seedWorkspace(project)
	return d
}

func (d *driver) do(command protocol.Command) (protocol.Output, error) {
	return d.window.Controller.Do(d.ctx, command)
}

func result[T any](output protocol.Output) (T, error) {
	switch payload := output.Data.(type) {
	case T:
		return payload, nil
	case *T:
		if payload != nil {
			return *payload, nil
		}
	}
	return *new(T), fmt.Errorf("unexpected %q payload %T", output.Kind, output.Data)
}

// seedWorkspace adds the project and a session whose provider points at a
// closed loopback port: nothing is ever sent to a model.
func (d *driver) seedWorkspace(path string) {
	d.t.Helper()
	provider := protocol.DefaultConversationProvider()
	provider.ID, provider.Name, provider.Enabled = protocol.NewProviderID(), "Parity fixture", true
	provider.Api, provider.Authentication, provider.Endpoint = protocol.ConversationModelApiChatCompletions, protocol.AuthenticationApiKey, "http://127.0.0.1:9/v1"
	model := protocol.DefaultConversationModel()
	model.ID, model.Context, model.Output = "fixture", 32000, 8000
	provider.DefaultModel, provider.Models = model.ID, []protocol.ConversationModel{model}
	secret := protocol.NewSecret("parity-key")
	if _, err := d.do(protocol.Command{Kind: "save_provider", Data: protocol.CommandSaveProviderPayload{Provider: provider, Secret: &secret}}); err != nil {
		d.t.Fatal(err)
	}
	output, err := d.do(protocol.Command{Kind: "create_project", Data: protocol.ProjectsDraft{
		Name: projectName, Path: path, Appearance: protocol.DefaultProjectsAppearance(), Source: protocol.ProjectsSource{Kind: "local"},
	}})
	if err == nil {
		d.project, err = result[protocol.Project](output)
	}
	if err != nil {
		d.t.Fatal(err)
	}
	config := protocol.DefaultSessionConfig()
	config.Provider, config.Model, config.Effort = provider.ID, model.ID, model.DefaultEffort
	config.Mode, config.Permission = protocol.WorkModeCode, protocol.PermissionFull
	output, err = d.do(protocol.Command{Kind: "create_session", Data: protocol.CommandCreateSessionPayload{Project: &d.project.ID, Config: &config}})
	if err == nil {
		d.session, err = result[protocol.Session](output)
	}
	if err != nil {
		d.t.Fatal(err)
	}
}

func (d *driver) seedProfiles() error {
	profiles := []protocol.SshProfile{
		{Name: stagingName, Host: "staging.example.invalid", Port: 22, Username: "deploy"},
		{Name: "Build box", Host: "build.example.invalid", Port: 2222, Username: "ci"},
		{Name: loopbackName, Host: "127.0.0.1", Port: d.peer.port, Username: peerUser},
	}
	// Saved connections are listed in ID order: fixed IDs keep the order of
	// the cards the same in every run.
	for index, profile := range profiles {
		profile.ID, profile.Authentication = protocol.SshID{0x70, byte(index + 1)}, protocol.SshAuthenticationPassword
		credential := protocol.SshCredential{Kind: "password", Data: protocol.SshCredentialPasswordPayload{Password: protocol.NewSecret(peerPassword)}}
		if _, err := d.do(protocol.Command{Kind: "save_ssh", Data: protocol.CommandSaveSshPayload{Profile: profile, Credential: &credential}}); err != nil {
			return fmt.Errorf("save %s: %w", profile.Name, err)
		}
	}
	return nil
}

// frame pumps one frame of virtual time with a pause for Node replies and
// package callbacks completing off the UI thread.
func (d *driver) frame(step time.Duration) {
	d.tester.Advance(step)
	d.tester.Frame()
	time.Sleep(2 * time.Millisecond)
}

func (d *driver) await(what string, timeout time.Duration, ready func() bool) error {
	for end := time.Now().Add(timeout); ; {
		d.frame(17 * time.Millisecond)
		if ready() {
			return nil
		}
		if err := d.ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(end) {
			return fmt.Errorf("%s did not appear within %v", what, timeout)
		}
	}
}

// settle pumps frames until three consecutive images are identical.
func (d *driver) settle() bool {
	var previous uint64
	same := 0
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end); {
		d.frame(50 * time.Millisecond)
		hash := fnv.New64a()
		_, _ = hash.Write(d.tester.Image().Pix)
		if current := hash.Sum64(); current == previous {
			if same++; same >= 3 {
				return true
			}
		} else {
			previous, same = current, 0
		}
	}
	return false
}

func (d *driver) find(match func(ui.AccessInfo) bool) (ui.AccessInfo, bool) {
	for _, info := range d.tester.AccessAll() {
		if info.Bounds.W > 0 && info.Bounds.H > 0 && match(info) {
			return info, true
		}
	}
	return ui.AccessInfo{}, false
}

func (d *driver) clickAt(r ui.Rect) { d.tester.ClickAt(r.X+r.W/2, r.Y+r.H/2) }

func (d *driver) button(name string) error {
	var target ui.AccessInfo
	if err := d.await("enabled control "+strconv.Quote(name), 15*time.Second, func() bool {
		var ok bool
		target, ok = d.find(func(info ui.AccessInfo) bool {
			return info.Name == name && !info.Disabled && (info.Role == ui.RoleButton || info.Role == ui.RoleMenuButton || info.Role == ui.RoleLink)
		})
		return ok
	}); err != nil {
		return err
	}
	d.clickAt(target.Bounds)
	d.tester.Frame()
	return nil
}

func (d *driver) dismiss(gone func() bool) {
	d.tester.Key(0, ui.KeyEscape)
	if d.await("dismissal", 3*time.Second, gone) == nil {
		return
	}
	if d.button(d.l.cancel) == nil {
		_ = d.await("dismissal", 3*time.Second, gone)
	}
}

// rest puts the pointer at the same blank point of the header. Hover state,
// such as the welcome wordmark's beam, follows the pointer, so every frame
// is taken with it there.
func (d *driver) rest() { d.tester.Move(float32(d.c.width)/2, native.HeaderHeight/2) }

// refocus gives the focus back to the button that opened a dismissed dialog.
// MyGo returns the focus to an overlay's opener only if the opener is built
// in the frame the overlay goes, and drops the opener either way. A package
// page is not built behind its modal dialog and returns in that frame or a
// later one, as its render arrives, so the restored focus was a race. A press
// the system cancels focuses the button as the restore does, without
// activating it.
func (d *driver) refocus(name string) error {
	var target ui.AccessInfo
	if err := d.await("the dialog's opener "+strconv.Quote(name), 10*time.Second, func() bool {
		var ok bool
		target, ok = d.find(func(info ui.AccessInfo) bool {
			return info.Name == name && !info.Disabled && info.Role == ui.RoleButton
		})
		return ok
	}); err != nil {
		return err
	}
	if !target.Focused {
		d.tester.Press(target.Bounds.X+target.Bounds.W/2, target.Bounds.Y+target.Bounds.H/2)
		d.tester.CancelPress()
		d.rest()
	}
	if !d.tester.Focused(name) {
		return fmt.Errorf("%q did not take the focus back", name)
	}
	return nil
}

func (d *driver) rightClick(text string) ([]string, error) {
	d.tester.CloseMenu()
	box, ok := d.tester.Find(text)
	if !ok {
		return nil, fmt.Errorf("%q is not shown", text)
	}
	d.tester.RightClickAt(box.X+box.W/2, box.Y+box.H/2)
	var menu []string
	err := d.await("context menu on "+strconv.Quote(text), 5*time.Second, func() bool {
		menu = slices.Clone(d.tester.Menu())
		return len(menu) > 0
	})
	return menu, err
}

type accessNode struct {
	Role        string  `json:"role"`
	Name        string  `json:"name"`
	Value       string  `json:"value,omitempty"`
	Placeholder string  `json:"placeholder,omitempty"`
	X           float32 `json:"x"`
	Y           float32 `json:"y"`
	W           float32 `json:"w"`
	H           float32 `json:"h"`
	Disabled    bool    `json:"disabled,omitempty"`
	Focused     bool    `json:"focused,omitempty"`
	Checked     bool    `json:"checked,omitempty"`
	Expanded    bool    `json:"expanded,omitempty"`
}

type textBox struct {
	Text string  `json:"text"`
	X    float32 `json:"x"`
	Y    float32 `json:"y"`
	W    float32 `json:"w"`
	H    float32 `json:"h"`
}

type record struct {
	Scenario string       `json:"scenario"`
	Mode     string       `json:"mode"`
	Locale   string       `json:"locale"`
	Theme    string       `json:"theme"`
	Width    int          `json:"width"`
	Height   int          `json:"height"`
	Scale    float32      `json:"scale"`
	Status   string       `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Stable   bool         `json:"stable"`
	Menu     []string     `json:"menu,omitempty"`
	Access   []accessNode `json:"access,omitempty"`
	Texts    []textBox    `json:"texts,omitempty"`
}

func (d *driver) name(scenario string) string {
	return filepath.Join(d.c.out, scenario+"-"+d.locale+"-"+d.theme)
}

func (d *driver) write(r record, image bool) {
	d.t.Helper()
	r.Mode, r.Locale, r.Theme, r.Width, r.Height, r.Scale = d.mode, d.locale, d.theme, d.c.width, d.c.height, d.c.scale
	if r.Status != "skipped" {
		for _, info := range d.tester.AccessAll() {
			b := info.Bounds
			r.Access = append(r.Access, accessNode{Role: fmt.Sprint(info.Role), Name: info.Name, Value: info.Value, Placeholder: info.Placeholder,
				X: b.X, Y: b.Y, W: b.W, H: b.H, Disabled: info.Disabled, Focused: info.Focused, Checked: info.Checked, Expanded: info.Expanded})
		}
		seen := map[string]bool{}
		for _, text := range d.tester.Texts() {
			if seen[text] {
				continue
			}
			seen[text] = true
			if box, ok := d.tester.Find(text); ok {
				r.Texts = append(r.Texts, textBox{Text: text, X: box.X, Y: box.Y, W: box.W, H: box.H})
			}
		}
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(d.name(r.Scenario)+".json", body, 0o644); err != nil {
		d.t.Fatal(err)
	}
	if !image {
		return
	}
	file, err := os.Create(d.name(r.Scenario) + ".png")
	if err != nil {
		d.t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, d.tester.Image()); err != nil {
		d.t.Fatal(err)
	}
}

// step runs one transition and records the state it reaches. The first
// failure in a group is recorded with the frame it left; later steps of the
// group depend on it and are recorded as skipped.
func (d *driver) step(scenario string, action func() ([]string, string, error)) {
	d.t.Helper()
	if len(d.c.only) > 0 && !slices.ContainsFunc(d.c.only, func(prefix string) bool { return strings.HasPrefix(scenario, prefix) }) {
		// Unselected states still run, since later states depend on them,
		// but are not recorded.
		if d.failure == "" {
			if _, _, err := action(); err != nil {
				d.failure = scenario + ": " + err.Error()
			}
		}
		return
	}
	if d.failure != "" {
		d.write(record{Scenario: scenario, Status: "skipped", Detail: "after " + d.failure}, false)
		return
	}
	menu, note, err := action()
	d.rest()
	stable := d.settle()
	if err != nil {
		d.failure = scenario + ": " + err.Error()
		d.write(record{Scenario: scenario, Status: "failed", Detail: err.Error(), Stable: stable, Menu: menu}, true)
		d.t.Errorf("%s %s-%s: %v", scenario, d.locale, d.theme, err)
		return
	}
	d.write(record{Scenario: scenario, Status: "captured", Detail: note, Stable: stable, Menu: menu}, true)
}

func (d *driver) sshGroup() {
	d.failure = ""
	l := d.l
	d.step("ssh-01-empty", func() ([]string, string, error) {
		d.window.App.Navigate(native.ConversationPage)
		var rail ui.AccessInfo
		if err := d.await("the SSH rail entry", 30*time.Second, func() bool {
			var ok bool
			// The label is compared case-insensitively so a wrong label is
			// reported as a difference instead of hiding the page behind it.
			rail, ok = d.find(func(info ui.AccessInfo) bool {
				return strings.EqualFold(info.Name, l.page) && info.Bounds.X+info.Bounds.W <= native.RailWidth+1
			})
			return ok
		}); err != nil {
			return nil, "", err
		}
		d.clickAt(rail.Bounds)
		return nil, "", d.await("the empty SSH landing", 30*time.Second, func() bool { return d.tester.HasText(l.empty) })
	})
	d.step("ssh-02-new-dialog", func() ([]string, string, error) {
		if err := d.button(l.create); err != nil {
			return nil, "", err
		}
		return nil, "", d.await("the new connection form", 10*time.Second, func() bool { return d.tester.HasText(l.host) })
	})
	if d.failure == "" {
		d.dismiss(func() bool { return !d.tester.HasText(l.host) })
		if err := d.refocus(l.create); err != nil {
			d.failure = "ssh-02-new-dialog: " + err.Error()
			d.t.Errorf("%s-%s %s", d.locale, d.theme, d.failure)
		}
	}
	d.step("ssh-03-list", func() ([]string, string, error) {
		if err := d.seedProfiles(); err != nil {
			return nil, "", err
		}
		return nil, "", d.await("the saved connections", 15*time.Second, func() bool {
			return d.tester.HasText(stagingName) && d.tester.HasText("Build box") && d.tester.HasText(loopbackName)
		})
	})
	d.step("ssh-04-connections-picker", func() ([]string, string, error) {
		if err := d.button(l.connections); err != nil {
			return nil, "", err
		}
		return nil, "", d.await("the connections picker", 10*time.Second, func() bool {
			_, ok := d.find(func(info ui.AccessInfo) bool {
				return info.Placeholder == l.search || (info.Role == ui.RoleTextField && info.Name == l.search)
			})
			return ok || d.tester.HasText(l.search)
		})
	})
	if d.failure == "" {
		d.dismiss(func() bool {
			_, ok := d.find(func(info ui.AccessInfo) bool { return info.Placeholder == l.search })
			return !ok && !d.tester.HasText(l.search)
		})
	}
	d.step("ssh-05-card-menu", func() ([]string, string, error) {
		menu, err := d.rightClick(stagingName)
		return menu, "native context menu: items only, not drawn in the frame", err
	})
	d.step("ssh-06-edit-dialog", func() ([]string, string, error) {
		if err := d.tester.ChooseMenuItem(l.edit); err != nil {
			return nil, "", err
		}
		return nil, "", d.await("the edit connection form", 10*time.Second, func() bool { return d.tester.HasText(l.host) })
	})
	if d.failure == "" {
		d.dismiss(func() bool { return !d.tester.HasText(l.host) })
	}
	d.step("ssh-07-delete-confirm", func() ([]string, string, error) {
		menu, err := d.rightClick(stagingName)
		if err != nil {
			return menu, "", err
		}
		if err := d.tester.ChooseMenuItem(l.remove); err != nil {
			return menu, "", err
		}
		text := fmt.Sprintf(l.removeConfirm, stagingName)
		if d.await("the in-window delete confirmation", 5*time.Second, func() bool { return d.tester.HasText(text) }) != nil {
			return menu, "no in-window confirmation: an OS-native confirmation is not drawn by the headless Tester", nil
		}
		return menu, "in-window confirmation", nil
	})
	if d.failure == "" {
		d.dismiss(func() bool { return !d.tester.HasText(fmt.Sprintf(l.removeConfirm, stagingName)) })
		if !d.tester.HasText(stagingName) {
			d.failure = "ssh-07-delete-confirm: dismissing the confirmation removed the connection"
			d.t.Error(d.failure)
		}
	}
	d.step("ssh-08-trust-dialog", func() ([]string, string, error) {
		box, ok := d.tester.Find(loopbackName)
		if !ok {
			return nil, "", errors.New("the loopback connection is not shown")
		}
		d.tester.ClickAt(box.X+box.W/2, box.Y+box.H/2)
		return nil, "", d.await("the host key prompt", 30*time.Second, func() bool { return d.tester.HasText(l.trustTitle) })
	})
	d.step("ssh-09-terminal", func() ([]string, string, error) {
		if err := d.button(l.trust); err != nil {
			return nil, "", err
		}
		return nil, "", d.await("the SSH terminal with its remote files", 45*time.Second, func() bool {
			running := false
			for _, info := range d.window.Controller.Snapshot().Terminals {
				running = running || (info.Ssh != nil && info.Status.Kind == "running")
			}
			return running && d.tester.HasText("README.md") && !d.tester.HasText(l.trustTitle)
		})
	})
}

func (d *driver) terminalGroup() {
	d.failure = ""
	app := d.window.App
	d.step("terminal-01-project-menu", func() ([]string, string, error) {
		app.Navigate(native.ConversationPage)
		app.NavOpen = true
		var row ui.AccessInfo
		if err := d.await("the project row", 15*time.Second, func() bool {
			var ok bool
			row, ok = d.find(func(info ui.AccessInfo) bool {
				b := info.Bounds
				return info.Name == projectName && b.Y >= native.HeaderHeight && b.X >= native.RailWidth && b.X+b.W <= native.RailWidth+app.NavWidth
			})
			return ok
		}); err != nil {
			return nil, "", err
		}
		d.tester.RightClickAt(row.Bounds.X+row.Bounds.W/2, row.Bounds.Y+row.Bounds.H/2)
		var menu []string
		err := d.await("the project menu", 5*time.Second, func() bool {
			menu = slices.Clone(d.tester.Menu())
			return len(menu) > 0
		})
		return menu, "native context menu: items only, not drawn in the frame", err
	})
	d.step("terminal-02-project-terminal", func() ([]string, string, error) {
		if err := d.tester.ChooseMenuItem(i18n.In(d.locale, "workspace_new_terminal")); err != nil {
			return nil, "", err
		}
		return nil, "", d.await("the project terminal", 30*time.Second, func() bool {
			for _, info := range d.window.Controller.Snapshot().Terminals {
				if info.Ssh == nil && info.Worktree != nil && info.Status.Kind == "running" {
					return true
				}
			}
			return false
		})
	})
	d.failure = ""
	d.step("terminal-03-session-details", func() ([]string, string, error) {
		d.tester.CloseMenu()
		d.window.OpenSession(d.session.ID.String())
		if err := d.await("the session", 15*time.Second, func() bool { return app.FocusedConversation() == d.session.ID.String() }); err != nil {
			return nil, "", err
		}
		if !app.PanelOpen {
			toggle, ok := d.find(func(info ui.AccessInfo) bool {
				return info.Role == ui.RoleButton && info.Name == i18n.In(d.locale, "details") && !info.Disabled && info.Bounds.Y+info.Bounds.H <= native.HeaderHeight
			})
			if !ok {
				return nil, "", errors.New("no enabled conversation details toggle in the header")
			}
			d.clickAt(toggle.Bounds)
		}
		return nil, "", d.await("the session's right panel", 10*time.Second, func() bool { return app.PanelOpen })
	})
	d.step("terminal-04-session-terminal", func() ([]string, string, error) {
		label := i18n.In(d.locale, "terminal")
		inside := func(b ui.Rect) bool {
			return b.X >= float32(d.c.width)-app.PanelWidth && b.X+b.W <= float32(d.c.width) && b.Y >= native.HeaderHeight
		}
		if len(app.Panel.Tabs) > 0 {
			if err := d.button(i18n.In(d.locale, "details")); err != nil {
				return nil, "", err
			}
			if err := d.tester.ChooseMenuItem(label); err != nil {
				item, ok := d.find(func(info ui.AccessInfo) bool { return info.Name == label && !info.Disabled && inside(info.Bounds) })
				if !ok {
					return nil, "", err
				}
				d.clickAt(item.Bounds)
			}
		} else {
			var item ui.AccessInfo
			if err := d.await("the panel terminal launcher", 10*time.Second, func() bool {
				var ok bool
				item, ok = d.find(func(info ui.AccessInfo) bool {
					return info.Role == ui.RoleButton && info.Name == label && !info.Disabled && inside(info.Bounds)
				})
				return ok
			}); err != nil {
				return nil, "", err
			}
			d.clickAt(item.Bounds)
		}
		return nil, "", d.await("the panel terminal", 30*time.Second, func() bool { return len(app.Panel.Tabs) > 0 })
	})
}
