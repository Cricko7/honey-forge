package sshtrap

import (
	"io"
	"os"
	"sync"
	"unicode/utf8"

	"github.com/pkg/sftp"

	"honey-forge/internal/contract"
)

// All paths are resolved by OpenSSH inside the sandbox, never on the gateway.
type fileHandlers struct {
	client *sftp.Client
	record *recording
	limit  int64
	slots  chan struct{}
	fail   func(error)
}

func validPath(path string) bool {
	return len(path) > 0 && len(path) <= 4096 && utf8.ValidString(path)
}

func (h *fileHandlers) RealPath(path string) (string, error) {
	if !validPath(path) {
		return "", sftp.ErrSSHFxBadMessage
	}

	return h.client.RealPath(path)
}

func (h *fileHandlers) Fileread(req *sftp.Request) (io.ReaderAt, error) {
	if !validPath(req.Filepath) {
		return nil, sftp.ErrSSHFxBadMessage
	}

	select {
	case h.slots <- struct{}{}:
	default:
		return nil, sftp.ErrSSHFxFailure
	}

	f, err := h.client.Open(req.Filepath)
	if err != nil {
		<-h.slots
		return nil, sftp.ErrSSHFxFailure
	}

	return &readFile{file: f, release: func() {
		<-h.slots
	}}, nil
}

type readFile struct {
	file    *sftp.File
	release func()
	once    sync.Once
}

func (f *readFile) ReadAt(b []byte, offset int64) (int, error) {
	return f.file.ReadAt(b, offset)
}

func (f *readFile) Close() error {
	var err error
	f.once.Do(func() {
		err = f.file.Close()
		f.release()
	})

	return err
}

func (h *fileHandlers) Filewrite(req *sftp.Request) (io.WriterAt, error) {
	flags := req.Pflags()
	// Resumed writes would require recording pre-existing bytes to claim a
	// complete artifact. Demo uploads always create/truncate a fresh artifact.
	if !validPath(req.Filepath) || !flags.Trunc || flags.Append {
		return nil, sftp.ErrSSHFxOpUnsupported
	}

	select {
	case h.slots <- struct{}{}:
	default:
		return nil, sftp.ErrSSHFxFailure
	}

	release := func() {
		<-h.slots
	}
	id := string(contract.NewID())
	if err := h.record.event("service.file_opened", map[string]any{"file_id": id, "path": req.Filepath}); err != nil {
		release()
		h.fail(err)

		return nil, sftp.ErrSSHFxFailure
	}

	mode := os.O_WRONLY | os.O_TRUNC
	if flags.Creat {
		mode |= os.O_CREATE
	}

	if flags.Excl {
		mode |= os.O_EXCL
	}

	f, err := h.client.OpenFile(req.Filepath, mode)
	if err != nil {
		release()
		return nil, sftp.ErrSSHFxFailure
	}

	return &upload{file: f, record: h.record, id: id, limit: h.limit, release: release, fail: h.fail}, nil
}

func (h *fileHandlers) Filecmd(req *sftp.Request) error {
	if !validPath(req.Filepath) {
		return sftp.ErrSSHFxBadMessage
	}

	switch req.Method {
	case "Remove":
		return h.client.Remove(req.Filepath)
	case "Rmdir":
		return h.client.RemoveDirectory(req.Filepath)
	case "Mkdir":
		return h.client.Mkdir(req.Filepath)
	case "Rename":
		if !validPath(req.Target) {
			return sftp.ErrSSHFxBadMessage
		}

		return h.client.Rename(req.Filepath, req.Target)
	case "Setstat":
		// chmod is useful for executing uploaded files. Size changes and links
		// are rejected so upload hashes continue to describe the recorded bytes.
		flags := req.AttrFlags()
		if !flags.Permissions || flags.Size || flags.UidGid || flags.Acmodtime {
			return sftp.ErrSSHFxOpUnsupported
		}

		return h.client.Chmod(req.Filepath, os.FileMode(req.Attributes().Mode&0777))
	default:
		return sftp.ErrSSHFxOpUnsupported
	}
}

func (h *fileHandlers) Filelist(req *sftp.Request) (sftp.ListerAt, error) {
	if !validPath(req.Filepath) {
		return nil, sftp.ErrSSHFxBadMessage
	}

	var files []os.FileInfo
	var err error
	switch req.Method {
	case "List":
		files, err = h.client.ReadDir(req.Filepath)
	case "Stat", "Lstat":
		var info os.FileInfo
		if req.Method == "Stat" {
			info, err = h.client.Stat(req.Filepath)
		} else {
			info, err = h.client.Lstat(req.Filepath)
		}

		if err == nil {
			files = []os.FileInfo{info}
		}
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}

	return fileList(files), err
}

type fileList []os.FileInfo

func (l fileList) ListAt(dst []os.FileInfo, offset int64) (int, error) {
	if offset < 0 {
		return 0, sftp.ErrSSHFxBadMessage
	}

	if offset >= int64(len(l)) {
		return 0, io.EOF
	}

	n := copy(dst, l[offset:])
	if n < len(dst) {
		return n, io.EOF
	}

	return n, nil
}
