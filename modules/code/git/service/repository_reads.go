package git

import (
	"context"
	"strings"
	"time"

	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

// ReadTimed preserves the deadline even for an underlying read that cannot be
// interrupted at its current filesystem call.
func ReadTimed(ctx context.Context, duration time.Duration, status bool, read func(context.Context) (protocol.Output, error)) (protocol.Output, error) {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	type result struct {
		output protocol.Output
		err    error
	}
	done := make(chan result, 1)
	go func() { output, err := read(ctx); done <- result{output, err} }()
	select {
	case <-ctx.Done():
		if status {
			return protocol.Output{}, statusFailure(mapError(ctx.Err()), protocol.GitInspectionDeadlineExceeded)
		}
		return protocol.Output{}, mapError(ctx.Err())
	case value := <-done:
		return value.output, value.err
	}
}
func (s *Service) ResolveRevision(ctx context.Context, root, revision string) (string, error) {
	r, err := s.openReadWorktree(ctx, root)
	if err != nil {
		return "", err
	}
	value, err := resolve(r, revision)
	return value.String(), err
}

func (s *Service) StashDiff(ctx context.Context, root, commit string) (protocol.GitDiff, error) {
	stashes, err := s.stashes(ctx, root)
	if err != nil {
		return protocol.GitDiff{}, err
	}
	for _, stash := range stashes {
		if stash.Commit == commit {
			capture, err := s.command(ctx, root, []string{"stash", "show", "--patch", "--no-ext-diff", "--no-color", commit}, nil, true)
			text, partial := bounded(string(capture.Stdout), maxDiff)
			return protocol.GitDiff{Scope: protocol.GitDiffScopeAll, Text: text, Truncated: partial || capture.Truncated}, err
		}
	}
	return protocol.GitDiff{}, conflict()
}
func (s *Service) RemoteTags(ctx context.Context, root, remote string) ([]protocol.GitTag, error) {
	if err := name(remote); err != nil {
		return nil, err
	}
	r, err := s.openReadWorktree(ctx, root)
	if err != nil {
		return nil, err
	}
	if _, err := r.Remote(remote); err != nil {
		return nil, mapError(err)
	}
	capture, err := s.command(ctx, root, []string{"ls-remote", "--tags", "--refs", remote}, nil, false)
	if err != nil {
		return nil, err
	}
	if capture.Truncated || capture.InvalidUTF8 {
		return nil, fault(protocol.ErrorCodeUnavailable, "Git remote listing exceeds output limit")
	}
	result := []protocol.GitTag{}
	for _, line := range strings.Split(strings.TrimSpace(string(capture.Stdout)), "\n") {
		commit, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		tag, ok := strings.CutPrefix(ref, "refs/tags/")
		if !ok {
			continue
		}
		if err := fullHash(commit); err != nil {
			return nil, err
		}
		result = append(result, protocol.GitTag{Name: tag, Commit: commit})
		if len(result) > maxEntries {
			return nil, fault(protocol.ErrorCodeUnavailable, "remote tag listing exceeds limit")
		}
	}
	return result, nil
}
