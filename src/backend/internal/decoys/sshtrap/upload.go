package sshtrap

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"sync"

	"github.com/pkg/sftp"
)

type writableFile interface {
	io.WriterAt
	io.Closer
}

type upload struct {
	file           writableFile
	record         *recording
	id             string
	limit          int64
	release        func()
	fail           func(error)
	mu             sync.Mutex // protects content, completion and close across SFTP workers
	content        []byte
	failed, closed bool
}

func (u *upload) WriteAt(data []byte, offset int64) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return 0, io.ErrClosedPipe
	}
	if offset < 0 || int64(len(data)) > u.limit || offset > u.limit-int64(len(data)) {
		u.failed = true
		return 0, sftp.ErrSSHFxFailure
	}
	end := offset + int64(len(data))
	growth := max(int64(0), end-int64(len(u.content)))
	r := u.record
	r.mu.Lock()
	if int64(len(data)) > u.limit-r.uploadBytes || growth > u.limit-r.uploadExtent {
		r.reason = "upload_limit"
		r.mu.Unlock()
		u.failed = true
		u.fail(ErrRecordingLimit)
		return 0, sftp.ErrSSHFxFailure
	}
	for done := 0; done < len(data); {
		n := min(len(data)-done, 4096)
		err := r.emit("service.file_chunk", map[string]any{"file_id": u.id, "offset": offset + int64(done), "payload_base64": base64.StdEncoding.EncodeToString(data[done : done+n]), "bytes": n})
		if err != nil {
			r.mu.Unlock()
			u.failed = true
			u.fail(err)
			return 0, sftp.ErrSSHFxFailure
		}
		r.uploadBytes += int64(n)
		r.received += int64(n)
		committedEnd := offset + int64(done+n)
		committedGrowth := max(int64(0), committedEnd-int64(len(u.content)))
		if committedGrowth > 0 {
			u.content = append(u.content, make([]byte, committedGrowth)...)
			r.uploadExtent += committedGrowth
		}
		copy(u.content[offset+int64(done):], data[done:done+n])
		done += n
	}
	r.mu.Unlock()
	n, err := u.file.WriteAt(data, offset)
	if err != nil || n != len(data) {
		u.failed = true
		return n, sftp.ErrSSHFxFailure
	}
	return n, nil
}

// TransferError is invoked by RequestServer for handles abandoned on disconnect.
func (u *upload) TransferError(err error) { u.mu.Lock(); u.failed = true; u.mu.Unlock() }

func (u *upload) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return nil
	}
	u.closed = true
	defer u.release()
	closeErr := u.file.Close()
	sum := sha256.Sum256(u.content)
	err := u.record.event("service.file_closed", map[string]any{"file_id": u.id, "size": len(u.content), "sha256": hex.EncodeToString(sum[:]), "complete": !u.failed && closeErr == nil})
	if err != nil {
		u.fail(err)
		return sftp.ErrSSHFxFailure
	}
	if closeErr != nil {
		return sftp.ErrSSHFxFailure
	}
	return nil
}
