package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sailry/sailry-harness/sdk/desktop/view"
	"github.com/sailry/sailry-harness/sdk/native/protocol"
)

const (
	sshTransferReady        = "ready"
	sshTransferPreparing    = "preparing"
	sshTransferTransferring = "transferring"
	sshTransferPublishing   = "publishing"
	sshTransferDone         = "done"
	sshTransferExists       = "exists"
	sshTransferFailed       = "failed"
)

// These operations belong to the window, not to a rendered page. In-flight
// transfers retain the original Node and revision across close and reopen.
type transferHost struct {
	context     context.Context
	async       func(func()) bool
	prepare     func(protocol.Command) protocol.Request
	execute     func(context.Context, protocol.Request) (protocol.Output, error)
	resolve     func(context.Context, protocol.Request) (view.Receipt, error)
	openSource  func(context.Context, string, uint64) (view.SourceFile, error)
	destination func(string) (view.Destination, error)
	upload      func(context.Context, protocol.StreamID, uint64, string, io.ReadCloser, func(uint64)) error
	download    func(context.Context, protocol.StreamID, uint64, string, io.WriteCloser, func(uint64)) error
	release     func(protocol.StreamID)
}

func newTransferHost(ctx context.Context, s view.Services) transferHost {
	return transferHost{
		context: ctx, async: s.Async, prepare: s.Prepare, execute: s.Execute, resolve: s.Resolve,
		openSource: s.OpenSource, destination: s.OpenDestination, upload: s.Upload, download: s.Download, release: s.ReleaseStream,
	}
}

type sshTransferAccess struct {
	host    transferHost
	profile protocol.SshProfile
}

func (a sshTransferAccess) request(command protocol.Command) protocol.Request {
	return a.host.prepare(command)
}

func (a sshTransferAccess) execute(ctx context.Context, command protocol.Command) (protocol.Output, error) {
	return a.host.execute(ctx, a.request(command))
}

type sshTransferStatus struct {
	stage        string
	copied, size uint64
	fault        *protocol.Fault
}

const maxFinishedSSHTransfers = 64

type transferJob struct {
	finishedAt uint64
	access     sshTransferAccess
	upload     bool
	local      string
	directory  bool
	remote     string
	status     sshTransferStatus
	plan       sshTransferPlan
	cancel     context.CancelFunc
	busy       bool
}

func (job *transferJob) snapshot(id protocol.RequestID) map[string]any {
	stage, progress := job.status.stage, any(nil)
	var failure *protocol.Fault
	unknown := false
	switch stage {
	case sshTransferTransferring:
		progress = map[string]any{"copied": job.status.copied, "size": job.status.size}
	case sshTransferFailed:
		failure = job.status.fault
		if failure == nil {
			failure = sshTransferKey("SSH file transfer is unavailable")
		}
		switch failure.Code {
		case protocol.ErrorCodeOutcomeUnknown:
			stage, unknown = "uncertain", true
		case protocol.ErrorCodeCancelled:
			stage = "cancelled"
		}
	}
	kind, source := "upload", filepath.Base(job.local)
	if !job.upload {
		kind, source = "download", job.remote
		if job.directory {
			kind = "download_directory"
		}
	}
	return map[string]any{
		"id": id, "profile": job.access.profile.ID, "kind": kind, "source": source, "path": job.remote,
		"stage": stage, "error": failure, "progress": progress,
		"can_start":   !job.busy && (job.status.stage == sshTransferReady || job.status.stage == sshTransferFailed && (!unknown || job.plan.pending == nil)),
		"can_replace": !job.busy && job.status.stage == sshTransferExists,
		"can_check":   !job.busy && job.plan.pending != nil && unknown,
		"can_cancel":  job.busy && job.status.stage != sshTransferPublishing,
	}
}

type transferStore struct {
	mu        sync.Mutex
	jobs      map[protocol.RequestID]*transferJob
	changed   chan struct{}
	uploads   chan struct{}
	downloads chan struct{}
	cursor    uint64
}

func newTransferStore() *transferStore {
	return &transferStore{jobs: map[protocol.RequestID]*transferJob{}, changed: make(chan struct{}), uploads: make(chan struct{}, 2), downloads: make(chan struct{}, 2)}
}

func (s *transferStore) publish() {
	s.cursor++
	close(s.changed)
	s.changed = make(chan struct{})
}

// close stops the window's transfers. A publication that has started keeps
// its durable request and is never cancelled.
func (s *transferStore) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.jobs {
		if job.cancel != nil && job.status.stage != sshTransferPublishing {
			job.cancel()
		}
	}
}

// A completion can add one job to the retained results. Keep the most recent
// completions even when an older transfer finishes after newer transfers.
func (s *transferStore) pruneFinished(latest *transferJob) {
	if latest.status.stage != sshTransferDone && (latest.status.stage != sshTransferFailed || latest.plan.pending != nil) {
		return
	}
	latest.finishedAt = s.cursor
	if len(s.jobs) <= maxFinishedSSHTransfers {
		return
	}
	var oldest protocol.RequestID
	oldestAt, count := latest.finishedAt, 0
	for id, job := range s.jobs {
		if job.finishedAt == 0 {
			continue
		}
		count++
		if job.finishedAt < oldestAt {
			oldest, oldestAt = id, job.finishedAt
		}
	}
	if count > maxFinishedSSHTransfers {
		delete(s.jobs, oldest)
	}
}

func (s *transferStore) insert(access sshTransferAccess, upload bool, local string, directory bool, remote string) protocol.RequestID {
	stage := sshTransferReady
	if !upload {
		if _, err := os.Lstat(local); err == nil {
			stage = sshTransferExists
		}
	}
	id := protocol.NewRequestID()
	s.mu.Lock()
	s.jobs[id] = &transferJob{access: access, upload: upload, local: local, directory: directory, remote: remote, status: sshTransferStatus{stage: stage}}
	s.publish()
	s.mu.Unlock()
	return id
}

func (s *transferStore) next(ctx context.Context, seen string) (json.RawMessage, error) {
	for {
		s.mu.Lock()
		cursor := strconv.FormatUint(s.cursor, 10)
		if cursor != seen {
			ids := make([]protocol.RequestID, 0, len(s.jobs))
			for id := range s.jobs {
				ids = append(ids, id)
			}
			slices.SortFunc(ids, func(a, b protocol.RequestID) int { return bytes.Compare(a[:], b[:]) })
			values := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				values = append(values, s.jobs[id].snapshot(id))
			}
			s.mu.Unlock()
			return json.Marshal(map[string]any{"cursor": cursor, "transfers": values})
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *transferStore) action(id string, kind string, available func(protocol.SshProfile, bool) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var identity protocol.RequestID
	var job *transferJob
	for key, value := range s.jobs {
		if key.String() == id {
			identity, job = key, value
			break
		}
	}
	if job == nil || !available(job.access.profile, kind == "start" || kind == "replace") {
		return sshTransferUnavailable()
	}
	switch kind {
	case "cancel":
		if job.cancel != nil {
			job.cancel()
		}
		return nil
	case "dismiss":
		if !job.busy {
			delete(s.jobs, identity)
			s.publish()
		}
		return nil
	}
	check, replace := kind == "check", kind == "replace"
	allowed := false
	switch job.status.stage {
	case sshTransferReady:
		allowed = kind == "start"
	case sshTransferExists:
		allowed = replace
	case sshTransferFailed:
		if job.status.fault != nil && job.status.fault.Code == protocol.ErrorCodeOutcomeUnknown && job.plan.pending != nil {
			allowed = check
		} else {
			allowed = kind == "start"
		}
	}
	if job.busy || !allowed {
		return sshTransferUnavailable()
	}
	host := job.access.host
	ctx, cancel := context.WithCancel(host.context)
	plan := job.plan
	job.finishedAt = 0
	job.cancel, job.busy, job.status, job.plan = cancel, true, sshTransferStatus{stage: sshTransferPreparing}, sshTransferPlan{}
	started := host.async(func() {
		defer cancel()
		update := func(status sshTransferStatus) {
			s.mu.Lock()
			job.status = status
			s.publish()
			s.mu.Unlock()
		}
		var status sshTransferStatus
		if job.upload {
			select {
			case s.uploads <- struct{}{}:
				plan, status = job.access.upload(ctx, job.local, job.remote, replace, plan, check, update)
				<-s.uploads
			case <-ctx.Done():
				status = sshTransferFailure(sshTransferCancelled())
			}
		} else {
			select {
			case s.downloads <- struct{}{}:
				status = job.access.download(ctx, job.remote, job.local, job.directory, update)
				<-s.downloads
			case <-ctx.Done():
				status = sshTransferFailure(sshTransferCancelled())
			}
		}
		if status.stage == sshTransferDone {
			plan = sshTransferPlan{}
		}
		s.mu.Lock()
		job.plan, job.status, job.busy, job.cancel = plan, status, false, nil
		s.pruneFinished(job)
		s.publish()
		s.mu.Unlock()
	})
	if !started {
		cancel()
		job.plan, job.busy, job.cancel = plan, false, nil
		job.status = sshTransferFailure(sshTransferUnavailable())
		s.pruneFinished(job)
	}
	s.publish()
	return nil
}

func sshTransferUnavailable() *protocol.Fault {
	return sshTransferKey("SSH file transfer is unavailable")
}

func sshTransferJoin(directory, name string) string {
	return strings.TrimRight(directory, "/") + "/" + name
}

func sshTransferRemoteName(remote string) string { return remote[strings.LastIndex(remote, "/")+1:] }

type previewBuffer struct{ bytes.Buffer }

func (*previewBuffer) Close() error { return nil }

type transferViews struct {
	store        *transferStore
	host         transferHost
	available    func(protocol.SshProfile, bool) bool
	chooseFiles  func(context.Context) ([]string, error)
	chooseFolder func(context.Context) (string, error)
	chooseSave   func(context.Context, string) (string, error)
	droppedFiles func(string) ([]string, error)
	preview      func(string, func(context.Context, uint64, func(uint64, uint64)) ([]byte, error)) error
}

func (v transferViews) access(raw json.RawMessage) (sshTransferAccess, error) {
	value, err := argument[map[string]json.RawMessage]([]json.RawMessage{raw}, 0)
	if err != nil {
		return sshTransferAccess{}, err
	}
	revision, err := scriptRevision(value["revision"])
	if err != nil {
		return sshTransferAccess{}, err
	}
	value["revision"], _ = json.Marshal(revision)
	body, err := json.Marshal(value)
	if err != nil {
		return sshTransferAccess{}, err
	}
	var profile protocol.SshProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return sshTransferAccess{}, invalid("invalid SSH transfer profile")
	}
	if !v.available(profile, true) {
		return sshTransferAccess{}, sshTransferUnavailable()
	}
	return sshTransferAccess{host: v.host, profile: profile}, nil
}

func (v transferViews) uploadFiles(ctx context.Context, arguments []json.RawMessage, dropped bool) (json.RawMessage, error) {
	if len(arguments) != 3 {
		return nil, invalid("SSH upload requires a profile and destination")
	}
	access, err := v.access(arguments[0])
	if err != nil {
		return nil, err
	}
	directory, err := argument[string](arguments, 1)
	if err != nil {
		return nil, err
	}
	var paths []string
	if dropped {
		handle, err := argument[string](arguments, 2)
		if err != nil {
			return nil, err
		}
		paths, err = v.droppedFiles(handle)
		if err != nil {
			return nil, err
		}
	} else {
		paths, err = v.chooseFiles(ctx)
		if err != nil {
			return nil, err
		}
	}
	for _, source := range paths {
		name := filepath.Base(source)
		if !filepath.IsAbs(source) || !utf8.ValidString(name) || !sshTransferLocalName(name) {
			return nil, sshTransferUnavailable()
		}
	}
	ids := make([]protocol.RequestID, 0, len(paths))
	for _, source := range paths {
		ids = append(ids, v.store.insert(access, true, source, false, sshTransferJoin(directory, filepath.Base(source))))
	}
	return json.Marshal(ids)
}

func (v transferViews) selectDownload(ctx context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
	if len(arguments) != 3 {
		return nil, invalid("SSH download requires a profile and files")
	}
	access, err := v.access(arguments[0])
	if err != nil {
		return nil, err
	}
	files, err := argument[[]struct {
		Path      string `json:"path"`
		Directory bool   `json:"directory"`
	}](arguments, 1)
	if err != nil || len(files) == 0 {
		return nil, invalid("SSH download requires at least one file")
	}
	names := make([]string, len(files))
	for i, file := range files {
		names[i] = sshTransferRemoteName(file.Path)
		if !sshTransferLocalName(names[i]) {
			return nil, sshTransferUnavailable()
		}
	}
	ids := make([]protocol.RequestID, 0, len(files))
	if len(files) == 1 {
		local, err := v.chooseSave(ctx, names[0])
		if err != nil {
			return nil, err
		}
		if local != "" {
			ids = append(ids, v.store.insert(access, false, local, files[0].Directory, files[0].Path))
		}
	} else {
		root, err := v.chooseFolder(ctx)
		if err != nil {
			return nil, err
		}
		if root != "" {
			for i, file := range files {
				ids = append(ids, v.store.insert(access, false, filepath.Join(root, names[i]), file.Directory, file.Path))
			}
		}
	}
	return json.Marshal(ids)
}

func (v transferViews) previewFile(_ context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
	if len(arguments) != 2 {
		return nil, invalid("SSH preview requires a profile and file")
	}
	access, err := v.access(arguments[0])
	if err != nil {
		return nil, err
	}
	path, err := argument[string](arguments, 1)
	if err != nil {
		return nil, err
	}
	return json.RawMessage("null"), v.preview(path, func(ctx context.Context, limit uint64, progress func(uint64, uint64)) ([]byte, error) {
		profile := access.profile
		out, err := access.execute(ctx, protocol.Command{Kind: "download_ssh_file", Data: protocol.CommandDownloadSshFilePayload{Profile: profile.ID, ExpectedRevision: profile.Revision, Path: path}})
		if err != nil {
			return nil, err
		}
		outcome, ok := payload[protocol.SshOutcome](out.Data)
		if !ok || out.Kind != "ssh_outcome" || outcome.Kind != "download" {
			return nil, sshTransferUnavailable()
		}
		file, ok := payload[protocol.SshDownload](outcome.Data)
		if !ok {
			return nil, sshTransferUnavailable()
		}
		if file.Path != path || file.Size > limit {
			access.release(file.Stream)
			return nil, sshTransferUnavailable()
		}
		var buffer previewBuffer
		if err := access.host.download(ctx, file.Stream, file.Size, file.Revision, &buffer, func(copied uint64) { progress(copied, file.Size) }); err != nil {
			return nil, err
		}
		return buffer.Bytes(), nil
	})
}

func (v transferViews) nextTransfers(ctx context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
	seen, err := argument[string](arguments, 0)
	if err != nil {
		return nil, err
	}
	return v.store.next(ctx, seen)
}

func (v transferViews) transferAction(_ context.Context, arguments []json.RawMessage) (json.RawMessage, error) {
	id, err := argument[string](arguments, 0)
	if err != nil {
		return nil, err
	}
	kind, err := argument[string](arguments, 1)
	if err != nil {
		return nil, err
	}
	return json.RawMessage("null"), v.store.action(id, kind, v.available)
}
