//go:build !sailry_no_code_index

package codeindex

import (
	"testing"
	"time"

	"github.com/odvcencio/gotreesitter/grammars"
)

func TestGrammarCacheBoundedAndPurgedOnIdle(t *testing.T) {
	f := newFixture(t)
	// Write files across more distinct languages than grammarCacheLimit.
	f.write(
		"a.go", "package a\nfunc A() {}\n",
		"b.rs", "pub fn b() {}\n",
		"c.py", "def c():\n    return 1\n",
		"d.js", "export function d() {}\n",
		"e.ts", "export function e(): number { return 1 }\n",
		"f.c", "int f(void) { return 1; }\n",
		"g.cpp", "int g() { return 1; }\n",
		"h.java", "class H { void h() {} }\n",
		"i.rb", "def i; 1; end\n",
		"j.kt", "fun j() {}\n",
	)
	page := f.search("")
	if len(page.Symbols) == 0 {
		t.Fatal("expected symbols from polyglot fixture")
	}
	loaded, limit := grammars.EmbeddedLanguageCacheStats()
	if limit <= 0 || limit > grammarCacheLimit {
		t.Fatalf("grammar cache limit = %d, want 1..%d", limit, grammarCacheLimit)
	}
	if loaded > limit {
		t.Fatalf("loaded grammars = %d exceeds cache limit %d", loaded, limit)
	}

	// Idle release after idleRelease purges cached grammars once all worktrees are idle.
	f.service.releaseIdle(time.Now().Add(idleRelease + time.Second))
	loaded, _ = grammars.EmbeddedLanguageCacheStats()
	if loaded != 0 {
		t.Fatalf("idle release left %d decoded grammars cached", loaded)
	}

	// A subsequent query seamlessly re-decodes and returns symbols.
	again := f.search("A")
	if len(again.Symbols) == 0 {
		t.Fatal("subsequent query after idle grammar purge returned no symbols")
	}
}
