package desktop

import (
	"context"
	"embed"
	"io/fs"
	"sync"

	"github.com/egoist/mygo/ui"
	kit "github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/view"
)

const PageID native.Page = "page:databases"
const preferenceKey = string(PageID) + "/database-workspace"

//go:embed view/*.js
var viewSource embed.FS

//go:embed locales/*.yml
var workspaceLocales embed.FS

var source = sync.OnceValue(func() view.Template {
	files := make(map[string]string)
	entries, err := fs.Glob(viewSource, "view/*.js")
	if err != nil {
		panic(err)
	}
	for _, name := range entries {
		body, err := viewSource.ReadFile(name)
		if err != nil {
			panic(err)
		}
		files[name] = string(body)
	}
	return view.Template{Entry: "view/main.js", Files: files}
})

var builtins = map[string][]string{
	"sailry":             {"theme", "header_action"},
	"sailry/forms":       {"createText", "readText", "releaseText", "setText", "focusText", "nextTextEvent"},
	"sailry/credentials": {"createSecret", "releaseSecret", "describeSecret"},
	"sailry/ui": {"nextTableEvent", "nextTreeEvent", "nextContextMenuEvent", "nextNavigationTabEvent",
		"nextPickerEvent", "nextControlEvent", "modal_closed", "writeClipboard", "toast"},
}

func Register(assembly *native.Assembly) {
	assembly.WorkspacePages = append(assembly.WorkspacePages, Page())
	assembly.Locales = append(assembly.Locales, workspaceLocales)
	registerLinks(assembly)
}

func Page() native.NativePage {
	return native.NativePage{
		Page:       PageID,
		Feature:    native.Feature{ID: string(PageID), Icon: icons.Database, Label: "code_databases", DefaultPinned: true, LegacyPackage: "databases"},
		BindWindow: bindWindow,
	}
}

type window struct {
	ctx      context.Context
	visible  func() bool
	services view.Services
	entries  visibility

	mu     sync.Mutex
	mount  *view.Mount
	views  *assistantView
	locale string
	failed bool
}

func bindWindow(binding native.WindowContext) native.WindowBinding {
	services, ok := view.WindowServices(binding.Context)
	if !ok || services.Prepare == nil || services.Execute == nil || services.Post == nil {
		return native.WindowBinding{}
	}
	p := &window{ctx: binding.Context, visible: binding.Visible, services: services, entries: visibility{changed: make(chan struct{})}}
	return native.WindowBinding{Render: p.render, Slots: p.slots, Sync: p.sync, Close: p.close}
}

func (p *window) render(c *ui.Context) {
	mount := p.current(c)
	if mount == nil || mount.Failed() {
		kit.Result(c, kit.ResultProps{Icon: icons.Database, Title: i18n.T(c, "unavailable")}).Grow(1).Justify(ui.Center)
		return
	}
	mount.Render(c)
}

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

func (p *window) sync() {
	shown := p.visible()
	p.entries.set(p.ctx, shown)
	if !shown {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.views != nil {
			p.views.releaseCurrent()
		}
		if p.failed || p.mount != nil && p.mount.Failed() {
			p.release()
		}
	}
}

func (p *window) close() {
	p.entries.set(p.ctx, false)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.release()
}

func (p *window) release() {
	if p.mount != nil {
		p.mount.Close()
	}
	p.mount, p.views, p.locale, p.failed = nil, nil, "", false
}
