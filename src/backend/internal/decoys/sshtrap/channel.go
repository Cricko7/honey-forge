package sshtrap

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"honey-forge/internal/contract"
)

func (r *Runtime) channel(ctx context.Context, ch ssh.Channel, requests <-chan *ssh.Request, client *ssh.Client, record *recording, config Config, cancel context.CancelFunc) {
	defer ch.Close()

	stop := context.AfterFunc(ctx, func() {
		ch.Close()
	})
	defer stop()

	id := string(contract.NewID())
	session, err := client.NewSession()
	if err != nil {
		return
	}

	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return
	}

	session.Stdout = recordingWriter{record, id, "stdout", ch.Write}
	session.Stderr = recordingWriter{record, id, "stderr", ch.Stderr().Write}
	var pumps sync.WaitGroup
	defer func() {
		ch.Close()
		session.Close()
		pumps.Wait()
	}()

	started := false
	for req := range requests {
		ok := false
		replied := false
		switch req.Type {
		case "pty-req":
			var p struct {
				Term                         string
				Columns, Rows, Width, Height uint32
				Modes                        string
			}
			if !started && ssh.Unmarshal(req.Payload, &p) == nil && len(p.Term) <= 64 && len(p.Modes) <= 1024 && p.Rows <= 1000 && p.Columns <= 1000 {
				// OpenSSH interprets terminal modes. Preserve the client's echo,
				// control characters and pixel dimensions after bounding the request.
				accepted, err := session.SendRequest("pty-req", true, req.Payload)
				ok = accepted && err == nil
			}
		case "window-change":
			var p struct{ Columns, Rows, Width, Height uint32 }
			if ssh.Unmarshal(req.Payload, &p) == nil && p.Rows <= 1000 && p.Columns <= 1000 {
				ok = session.WindowChange(int(p.Rows), int(p.Columns)) == nil
			}
		case "subsystem":
			var p struct{ Name string }
			if !started && ssh.Unmarshal(req.Payload, &p) == nil && p.Name == "sftp" {
				// SFTP opens its own backend channel; release the unused shell
				// channel so multiplexed clients stay within OpenSSH MaxSessions.
				session.Close()
				if req.WantReply {
					if err := req.Reply(true, nil); err != nil {
						return
					}
				}

				r.sftp(ctx, ch, client, record, config.Session.MaxUploadBytes, cancel)

				return
			}
		case "shell", "exec":
			if !started {
				if req.Type == "shell" {
					ok = len(req.Payload) == 0 && session.Shell() == nil
				} else {
					var p struct{ Command string }
					if ssh.Unmarshal(req.Payload, &p) == nil && len(p.Command) <= 4096 {
						if err := record.io(id, "exec", []byte(p.Command)); err != nil {
							r.recordingFailed(err, cancel)
							return
						}

						ok = session.Start(p.Command) == nil
					}
				}

				if ok {
					started = true
					// A fast command may exit immediately. Acknowledge the start
					// before the waiter is allowed to close the external channel.
					if req.WantReply {
						if err := req.Reply(true, nil); err != nil {
							return
						}

						replied = true
					}

					pumps.Go(func() {
						_, err := io.Copy(recordingWriter{record, id, "stdin", stdin.Write}, ch)
						stdin.Close()
						if err != nil && ctx.Err() == nil {
							cancel()
						}
					})
					pumps.Go(func() {
						err := session.Wait()
						status := uint32(0)
						if err != nil {
							status = 1
							if exit, ok := errors.AsType[*ssh.ExitError](err); ok {
								status = uint32(exit.ExitStatus())
							}
						}

						ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
						ch.Close() // releases the stdin pump even when the client keeps the SSH connection open
					})
				}
			}
		}

		if req.WantReply && !replied {
			if err := req.Reply(ok, nil); err != nil {
				return
			}
		}
	}

	ch.Close()
	session.Close()
}

func (r *Runtime) recordingFailed(err error, cancel context.CancelFunc) {
	if !errors.Is(err, ErrRecordingLimit) {
		r.fail(err)
	}

	cancel()
}

func (r *Runtime) sftp(ctx context.Context, ch ssh.Channel, client *ssh.Client, record *recording, limit int64, cancel context.CancelFunc) {
	remote, err := sftp.NewClient(client, sftp.MaxPacket(4096))
	if err != nil {
		return
	}

	defer remote.Close()

	stop := context.AfterFunc(ctx, func() {
		remote.Close()
	})
	defer stop()

	h := &fileHandlers{
		client: remote,
		record: record,
		limit:  limit,
		slots:  make(chan struct{}, 16),
		fail: func(err error) {
			r.recordingFailed(err, cancel)
		},
	}
	server := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h}, sftp.WithStartDirectory("/home/sandbox"))
	defer server.Close()

	// Disconnects are local session failures, not a reason to stop other sessions.
	server.Serve()
}
