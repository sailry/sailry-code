package codehost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/process"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/sdktest"
)

func TestLegacyIndexCleanupWithCodeProfileOwnership(t *testing.T) {
	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		t.Setenv(key, home)
	}
	profile := filepath.Join(t.TempDir(), "profile")
	lock, err := sdktest.AcquireProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })

	index := filepath.Join(profile, "cache", "code-index")
	worktree := filepath.Join(index, (protocol.WorktreeID{1}).String())
	// Model an interrupted cleanup that removed the database but not its sidecars.
	partial := filepath.Join(index, (protocol.WorktreeID{2}).String())
	remove := []string{
		filepath.Join(worktree, "index.turso"),
		filepath.Join(worktree, "index.db-log"),
		filepath.Join(worktree, "index.turso-wal"),
		filepath.Join(worktree, "index.turso-tshm"),
		filepath.Join(partial, "index.db-log"),
		filepath.Join(partial, "index.turso-wal"),
		filepath.Join(partial, "index.turso-tshm"),
	}
	keep := []string{
		filepath.Join(worktree, "unknown.txt"),
		filepath.Join(worktree, "nested", "index.turso"),
		filepath.Join(partial, "index.turso-backup"),
		filepath.Join(index, "not-a-worktree-id", "index.turso"),
		filepath.Join(index, (protocol.WorktreeID{3}).String(), "Index.turso"),
		filepath.Join(index, "index.turso"),
		filepath.Join(profile, "cache", "other", (protocol.WorktreeID{4}).String(), "index.turso"),
	}
	for _, paths := range [][]string{remove, keep} {
		for _, path := range paths {
			writeLegacyIndexFixture(t, path)
		}
	}
	options := node.Options{
		ProductID: "code", Headless: true, Assembly: Assembly(),
		Environment: new(process.NewEnvironment([]string{"PATH=/usr/bin:/bin"})), MCP: sdktest.MCPOptions(),
	}
	if owner, err := node.StartWithOptions(t.Context(), profile, options); err == nil || !strings.Contains(err.Error(), "already owns profile") {
		if owner != nil {
			_ = owner.Stop(context.Background())
		}
		t.Fatalf("start with a locked profile: %v", err)
	}
	for _, path := range remove {
		assertLegacyIndexFixture(t, path)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []string{"start", "restart"} {
		t.Run(attempt, func(t *testing.T) {
			owner, err := node.StartWithOptions(t.Context(), profile, options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := owner.Stop(context.Background()); err != nil {
					t.Error(err)
				}
			})
			for _, path := range remove {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("retired cache file %s was not removed: %v", path, err)
				}
			}
			for _, path := range keep {
				assertLegacyIndexFixture(t, path)
			}
		})
	}
}

func TestLegacyIndexCleanupDoesNotFollowSymlinks(t *testing.T) {
	id := (protocol.WorktreeID{1}).String()
	member := filepath.Join("cache", "code-index", id, "index.turso")
	for _, name := range []string{".", "cache", filepath.Join("cache", "code-index"), filepath.Dir(member), member} {
		t.Run(filepath.Base(name), func(t *testing.T) {
			profile := filepath.Join(t.TempDir(), "profile")
			rest, err := filepath.Rel(name, member)
			if err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(profile, name)
			target := filepath.Join(filepath.Dir(link), "unrelated", rest)
			writeLegacyIndexFixture(t, target)
			if err := os.Symlink("unrelated", link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if err := cleanLegacyIndex(profile); err != nil {
				t.Fatal(err)
			}
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("symlink was changed: %v, %v", info, err)
			}
			assertLegacyIndexFixture(t, target)
		})
	}
}

func TestLegacyIndexCleanupPreservesHardLinks(t *testing.T) {
	profile := t.TempDir()
	outside := filepath.Join(t.TempDir(), "unrelated.turso")
	index := filepath.Join(profile, "cache", "code-index")
	linkedWorktree := filepath.Join(index, (protocol.WorktreeID{1}).String())
	primary := filepath.Join(linkedWorktree, "index.turso")
	linked := filepath.Join(linkedWorktree, "index.turso-wal")
	removable := filepath.Join(index, (protocol.WorktreeID{2}).String(), "index.turso")
	for _, path := range []string{outside, primary, removable} {
		writeLegacyIndexFixture(t, path)
	}
	if err := os.Link(outside, linked); err != nil {
		t.Skipf("hard link unavailable: %v", err)
	}
	_ = cleanLegacyIndex(profile) // The unsafe family must not prevent other families from being cleaned.
	for _, path := range []string{outside, primary, linked} {
		assertLegacyIndexFixture(t, path)
	}
	if _, err := os.Lstat(removable); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unrelated retired index was not removed: %v", err)
	}
}

const legacyIndexFixtureContents = "fixture index data"

func writeLegacyIndexFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legacyIndexFixtureContents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertLegacyIndexFixture(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != legacyIndexFixtureContents {
		t.Errorf("fixture file %s was changed: %q, %v", path, data, err)
	}
}
