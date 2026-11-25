package desktop

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/sailry/sailry-harness/sdk/desktop/controls"
	"github.com/sailry/sailry-harness/sdk/desktop/i18n"
	"github.com/sailry/sailry-harness/sdk/desktop/icons"
	"github.com/sailry/sailry-harness/sdk/desktop/native"
	"github.com/sailry/sailry-harness/sdk/desktop/theme"
)

type fileSearch struct {
	query      native.FileSearchQuery
	result     *native.FileSearchResult
	running    bool
	generation uint64
}

func (t *fileTree) resetSearch() {
	t.search.generation++
	t.search.running, t.search.result = false, nil
	if t.project.Actions != nil && t.project.Actions.CancelSearch != nil {
		t.project.Actions.CancelSearch()
	}
}

func (t *fileTree) toggleSearch() {
	t.searchOpen = !t.searchOpen
	if !t.searchOpen {
		t.resetSearch()
	}
}

func (t *fileTree) runSearch() {
	t.resetSearch()
	if strings.TrimSpace(t.search.query.Query) == "" || t.project.Actions.Search == nil {
		return
	}
	generation := t.search.generation
	t.search.running = true
	t.project.Actions.Search(t.search.query, func(result native.FileSearchResult) {
		if generation == t.search.generation {
			t.search.running, t.search.result = false, &result
		}
	})
}

func (t *fileTree) renderSearch(c *ui.Context) {
	query := &t.search.query
	ui.Column(c.Key("search-fields")).Padding(8).Gap(8).Children(func() {
		submitted := false
		before := *query
		submit := func() { submitted = true; t.runSearch() }
		for _, field := range []struct {
			key   string
			value *string
		}{{"file_search_query", &query.Query}, {"file_search_filter", &query.Filter}} {
			label := i18n.T(c, field.key)
			ui.TextInput(c.Key(field.key), field.value).Placeholder(label).Label(label).FillWidth().OnSubmit(submit)
		}
		if before != *query && !submitted {
			t.resetSearch()
		}
		ui.Row(c.Key("search-controls")).Gap(4).Children(func() {
			for _, toggle := range []struct {
				key, text string
				value     *bool
			}{{"file_search_case", "Aa", &query.CaseSensitive}, {"file_search_regex", ".*", &query.Regex}} {
				controls.Ghost(c.Key(toggle.key), i18n.T(c, toggle.key), *toggle.value).Height(28).PaddingX(8).Children(func() {
					ui.Text(c, toggle.text).FontSize(13)
				}).OnClick(func() { *toggle.value = !*toggle.value; t.resetSearch() })
			}
			ui.Spacer(c)
			running := t.search.running
			icon, label, run := icons.FileSearch, "file_search", t.runSearch
			if running {
				icon, label, run = icons.CloseFileSearch, "settings_cancel", t.resetSearch
			}
			controls.IconButton(c.Key("run"), icon, i18n.T(c, label), 28, false).OnClick(run)
		})
	})
	result, running := t.search.result, t.search.running
	muted := theme.MutedForeground(c)
	if running {
		ui.Text(c, i18n.T(c, "file_search_running")).FontSize(12).PaddingX(12).TextColor(muted)
	} else if result != nil && result.Partial {
		ui.Text(c, i18n.T(c, "file_search_partial")).FontSize(12).PaddingX(12).TextColor(muted)
	}
	ui.Scroll(c.Key("search-results")).Grow(1).MinHeight(0).Padding(8).Children(func() {
		if result == nil || result.Failed {
			return
		}
		if len(result.Matches) == 0 {
			controls.Result(c, controls.ResultProps{Icon: icons.FileSearch, Title: i18n.T(c, "file_search_empty")}).Grow(1).Justify(ui.Center)
			return
		}
		for index, match := range result.Matches {
			label := fmt.Sprintf("%s:%d", match.Path, match.Line)
			controls.Ghost(c.Key(index), label, false).FillWidth().Padding(6, 8).Justify(ui.Start).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					ui.Text(c, label).FontSize(14).SingleLine()
					ui.Text(c, match.Text).FontSize(12).SingleLine().TextColor(muted)
				})
			}).OnClick(func() {
				t.project.Actions.OpenManagedFile(match.Path, match.Line)
			})
		}
	})
}
