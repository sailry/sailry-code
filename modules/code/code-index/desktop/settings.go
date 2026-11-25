package codeindexdesktop

import (
	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/settings"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

var messages = settings.Messages{
	Done: "code_index_settings_saved", Failed: "code_index_settings_failed",
	Conflict: "code_index_settings_conflict", Uncertain: "code_index_settings_unknown",
}

// Register contributes the Code Index switches to Conversation settings.
func Register(assembly *native.Assembly) {
	assembly.ExecutionSettings = append(assembly.ExecutionSettings, func(host settings.ExecutionSettingHost) settings.ExecutionSetting {
		return &Setting{host: host, Stored: settings.Stored[bool]{Saved: host.Preview, Draft: host.Preview}}
	})
}

type Setting struct {
	settings.Stored[bool]
	Persistent *bool
	host       settings.ExecutionSettingHost
}

func (s *Setting) Read() {
	if s.Pending || s.host.Execute == nil {
		return
	}
	s.Pending = true
	s.host.Execute(protocol.Command{Kind: "read_code_index_settings"}, func(c *ui.Context, out protocol.Output, err error) {
		s.Pending = false
		if err != nil {
			s.host.Report(c, err, messages)
			return
		}
		if value, ok := outputSettings(out); ok {
			if s.Draft == s.Saved {
				s.Draft = value.Enabled
			}
			s.load(value)
		}
	})
}

func (s *Setting) SaveEnabled(enabled bool) {
	if s.Pending || s.host.Save == nil {
		return
	}
	s.Draft = enabled
	s.save(nil)
}

func (s *Setting) SavePersistent(enabled bool) {
	if s.Pending || s.host.Save == nil || !s.Saved || s.Persistent == nil || *s.Persistent == enabled {
		return
	}
	s.save(new(enabled))
}

func (s *Setting) save(persistent *bool) {
	s.Pending = true
	command := protocol.Command{Kind: "save_code_index_settings", Data: protocol.CommandSaveCodeIndexSettingsPayload{
		Settings:         protocol.CodeIndexSettings{Revision: s.Revision, Enabled: s.Draft, Persistent: persistent},
		ExpectedRevision: s.Revision,
	}}
	s.host.Save(command, messages, func(out protocol.Output, ok bool) {
		s.Pending = false
		if !ok {
			s.Draft = s.Saved
			s.Read()
			return
		}
		if value, valid := outputSettings(out); valid {
			s.load(value)
		}
	})
}

func (s *Setting) load(value protocol.CodeIndexSettings) {
	s.Saved, s.Revision, s.Persistent = value.Enabled, value.Revision, value.Persistent
}

func (s *Setting) Render(c *ui.Context) {
	disabled := s.Pending || !s.host.Preview && s.host.Save == nil
	if settings.Switch(c, "code-index", i18n.T(c, "settings_code_index"), i18n.T(c, "settings_code_index_description"), &s.Draft, disabled) {
		if s.host.Preview {
			settings.Report(c, s.Commit(), messages)
		} else {
			s.SaveEnabled(s.Draft)
		}
	}
	if s.Persistent != nil {
		enabled := *s.Persistent
		if settings.Switch(c, "persistent-code-index", i18n.T(c, "settings_code_index_persistent"), i18n.T(c, "settings_code_index_persistent_description"), &enabled, s.Pending || !s.Saved || s.host.Save == nil) {
			s.SavePersistent(enabled)
		}
	}
}

func outputSettings(out protocol.Output) (protocol.CodeIndexSettings, bool) {
	switch value := out.Data.(type) {
	case protocol.CodeIndexSettings:
		return value, true
	case *protocol.CodeIndexSettings:
		if value != nil {
			return *value, true
		}
	}
	return protocol.CodeIndexSettings{}, false
}
