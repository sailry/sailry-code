package desktop

import (
	"context"
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/view"
)

const PageID native.Page = "page:ssh"

// preferenceKey keeps this page's column widths and folds apart from any
// package workspace, including a third-party package that is named ssh.
const preferenceKey = string(PageID) + "/ssh-workspace"

//go:embed view/*.js
var viewSource embed.FS

var source = sync.OnceValue(func() view.Template {
	files := make(map[string]string)
	entries, err := viewSource.ReadDir("view")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		name := "view/" + entry.Name()
		body, err := viewSource.ReadFile(name)
		if err != nil {
			panic(err)
		}
		files[name] = string(body)
	}
	return view.Template{Entry: "view/main.js", Files: files}
})

// builtins are the shared controls the template uses. Requests, transfers
// and resource components are this module's own exports.
var builtins = map[string][]string{
	"sailry":             {"theme", "header_action"},
	"sailry/forms":       {"createText", "readText", "releaseText", "setText", "focusText", "nextTextEvent"},
	"sailry/credentials": {"createSecret", "releaseSecret", "describeSecret", "chooseSecretFile"},
	"sailry/ui": {"nextTreeEvent", "nextContextMenuEvent", "nextNavigationTabEvent", "nextPickerEvent",
		"nextControlEvent", "modal_closed", "writeClipboard", "toast"},
}

func Register(assembly *native.Assembly) {
	assembly.WorkspacePages = append(assembly.WorkspacePages, Page())
	assembly.Locales = append(assembly.Locales, Locales)
}

func Page() native.NativePage {
	return native.NativePage{
		Page: PageID,
		// The page takes over the navigation of the retired official ssh package.
		Feature:    native.Feature{ID: string(PageID), Icon: icons.Command, Label: "ssh", DefaultPinned: true, LegacyPackage: "ssh"},
		BindWindow: bindWindow,
	}
}

// window is the page in one live window. The template exists while the page
// is shown; transfers belong to the window and continue after it is hidden.
type window struct {
	ctx       context.Context
	visible   func() bool
	services  view.Services
	transfers *transferStore

	mu     sync.Mutex
	mount  *view.Mount
	locale string
	failed bool
}

func bindWindow(binding native.WindowContext) native.WindowBinding {
	services, ok := view.WindowServices(binding.Context)
	if !ok || services.Prepare == nil || services.Execute == nil || services.Post == nil {
		return native.WindowBinding{}
	}
	p := &window{ctx: binding.Context, visible: binding.Visible, services: services, transfers: newTransferStore()}
	return native.WindowBinding{Render: p.render, Slots: p.slots, Sync: p.sync, Close: p.close}
}

func (p *window) render(c *ui.Context) {
	mount := p.current(c)
	if mount == nil || mount.Failed() {
		unavailable(c)
		return
	}
	mount.Render(c)
}

// current opens the template for the shown page and reopens it when the
// window's language changes. A failed mount is retried after leaving.
func (p *window) current(c *ui.Context) *view.Mount {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil || !p.visible() {
		return nil
	}
	if p.locale == c.Locale() && (p.mount != nil || p.failed) {
		return p.mount
	}
	p.release()
	p.locale = c.Locale()
	mount, err := view.Open(p.ctx, c, view.Options{
		Services: p.services, Template: source(), Preferences: preferenceKey, Builtins: builtins, Register: p.register,
	})
	if err != nil {
		p.failed = true
		return nil
	}
	p.mount = mount
	return mount
}

func (p *window) slots() native.WorkspaceSlots {
	p.mu.Lock()
	mount := p.mount
	p.mu.Unlock()
	if mount == nil {
		return native.WorkspaceSlots{PreferenceKey: preferenceKey}
	}
	slots := mount.Slots()
	slots.PreferenceKey = preferenceKey
	return slots
}

// sync releases the template, its drafts and its resource views when the
// page is no longer shown.
func (p *window) sync() {
	if p.visible() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.release()
}

func (p *window) close() {
	p.mu.Lock()
	p.release()
	p.mu.Unlock()
	p.transfers.close()
}

func (p *window) release() {
	if p.mount != nil {
		p.mount.Close()
	}
	p.mount, p.locale, p.failed = nil, "", false
}

func unavailable(c *ui.Context) {
	kit.Result(c, kit.ResultProps{Icon: icons.Terminal, Title: i18n.T(c, "unavailable")}).Grow(1).Justify(ui.Center)
}
