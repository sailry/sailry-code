package codehost

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/sailry/sailry-harness/sdk/native/files"
	"github.com/sailry/sailry-harness/sdk/native/node"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
	"github.com/sailry/sailry-harness/sdk/native/storage"
)

type gitWithLegacyIndexCleanup struct{ node.Module }

func (module gitWithLegacyIndexCleanup) WorkspaceServices() node.WorkspaceServices {
	services := module.Module.(node.WorkspaceServiceProvider).WorkspaceServices()
	bindGit := services.Bind
	services.Bind = func(runtime *node.WorkspaceRuntime) (node.WorkspaceBindings, error) {
		bindings, err := bindGit(runtime)
		if err != nil {
			return bindings, err
		}
		_, err = bindLegacyIndexCleanup(runtime)
		return bindings, err
	}
	return services
}

// Workspace factories open synchronously under the Node's profile lock, for
// both the standalone Host and the Desktop's local Node. Piggyback on the Git
// binding without registering any new module or restoring the retired service.
var bindLegacyIndexCleanup = (&node.WorkspaceFactory[struct{}]{
	Open: func(options node.WorkspaceOptions) (struct{}, func() error, error) {
		if err := cleanLegacyIndex(options.Profile); err != nil {
			slog.Warn("Legacy code index cleanup incomplete", "error", err)
		}
		return struct{}{}, nil, nil
	},
}).Bind(func(struct{}) node.WorkspaceBindings { return node.WorkspaceBindings{} })

const legacyIndexDatabase = "index.turso"

var legacyIndexFiles = files.DatabaseFiles(legacyIndexDatabase)

func cleanLegacyIndex(profile string) error {
	// The startup profile is canonical; refuse a replaced final component too.
	info, err := os.Lstat(profile)
	if err != nil || !info.IsDir() {
		return err
	}
	root, err := os.OpenRoot(profile)
	if err != nil {
		return err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return err
	}
	cache, err := openLegacyIndexDirectory(root, "cache")
	if err != nil || cache == nil {
		return err
	}
	defer cache.Close()
	index, err := openLegacyIndexDirectory(cache, "code-index")
	if err != nil || index == nil {
		return err
	}
	defer index.Close()

	entries, err := fs.ReadDir(index.FS(), ".")
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if id, err := protocol.ParseWorktreeID(name); err != nil || id.String() != name {
			continue
		}
		dir, err := openLegacyIndexDirectory(index, name)
		if err == nil && dir != nil {
			err = errors.Join(removeLegacyIndexFiles(dir), dir.Close())
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func openLegacyIndexDirectory(parent *os.Root, name string) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() {
		return nil, err
	}
	dir, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := dir.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = dir.Close()
		return nil, err
	}
	return dir, nil
}

func removeLegacyIndexFiles(dir *os.Root) error {
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil {
		return err
	}
	members := make(map[string]fs.FileInfo, len(legacyIndexFiles))
	for _, entry := range entries {
		// Compare actual names even on case-insensitive filesystems.
		if !slices.Contains(legacyIndexFiles, entry.Name()) {
			continue
		}
		info, err := dir.Lstat(entry.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		members[entry.Name()] = info
	}
	if len(members) == 0 {
		return nil
	}
	// Use the SDK's read-only database family check to reject hard links on
	// Unix and Windows. Unlink only the inspected entries via the open root.
	if err := storage.CheckDatabaseFiles(filepath.Join(dir.Name(), legacyIndexDatabase)); err != nil {
		return err
	}
	var failures []error
	for name, info := range members {
		current, err := dir.Lstat(name)
		if err == nil && os.SameFile(info, current) {
			err = dir.Remove(name)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
