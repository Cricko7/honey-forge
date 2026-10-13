package agent

import (
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

type pipeDeadline struct {
	at         time.Time
	timer      *time.Timer
	generation uint64
	active     bool
	timedOut   bool
}

// stdioConn transports TLS over pipes; it never opens a network socket.
// Pipe deadlines interrupt blocked I/O, but must never close an idle pipe:
// WebSocket write deadlines remain on the underlying TLS connection until
// the next write updates them.
type stdioConn struct {
	reader        io.Reader
	input         io.ReadCloser
	output        io.WriteCloser
	once          sync.Once
	closeErr      error
	mu            sync.Mutex
	closed        bool
	readDeadline  pipeDeadline
	writeDeadline pipeDeadline
}

func (s *stdioConn) Read(b []byte) (int, error) {
	finish, err := s.beginIO(&s.readDeadline)
	if err != nil {
		return 0, err
	}
	n, err := s.reader.Read(b)
	if deadlineErr := finish(); deadlineErr != nil {
		err = deadlineErr
	}
	return n, err
}
func (s *stdioConn) Write(b []byte) (int, error) {
	finish, err := s.beginIO(&s.writeDeadline)
	if err != nil {
		return 0, err
	}
	n, err := s.output.Write(b)
	if deadlineErr := finish(); deadlineErr != nil {
		err = deadlineErr
	}
	return n, err
}

func (s *stdioConn) beginIO(d *pipeDeadline) (func() error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, net.ErrClosed
	}
	if !d.at.IsZero() && !time.Now().Before(d.at) {
		return nil, os.ErrDeadlineExceeded
	}
	d.active = true
	d.timedOut = false
	s.armLocked(d)
	return func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		d.active = false
		s.stopLocked(d)
		if d.timedOut {
			return os.ErrDeadlineExceeded
		}
		return nil
	}, nil
}

func (s *stdioConn) stopLocked(d *pipeDeadline) {
	d.generation++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

func (s *stdioConn) armLocked(d *pipeDeadline) {
	s.stopLocked(d)
	if !d.active || d.at.IsZero() || s.closed {
		return
	}
	generation := d.generation
	d.timer = time.AfterFunc(time.Until(d.at), func() {
		s.mu.Lock()
		if s.closed || !d.active || d.generation != generation {
			s.mu.Unlock()
			return
		}
		d.timedOut = true
		s.closed = true
		s.stopLocked(&s.readDeadline)
		s.stopLocked(&s.writeDeadline)
		s.mu.Unlock()
		// Pipes cannot be interrupted portably without closing them. Only a
		// genuinely outstanding operation may take this terminal timeout path.
		s.closePipes()
	})
}

func (s *stdioConn) closePipes() error {
	s.once.Do(func() { s.closeErr = errors.Join(s.input.Close(), s.output.Close()) })
	return s.closeErr
}

func (s *stdioConn) Close() error {
	s.mu.Lock()
	s.closed = true
	s.stopLocked(&s.readDeadline)
	s.stopLocked(&s.writeDeadline)
	s.mu.Unlock()
	return s.closePipes()
}

func (s *stdioConn) LocalAddr() net.Addr  { return &net.UnixAddr{Name: "trap", Net: "pipe"} }
func (s *stdioConn) RemoteAddr() net.Addr { return &net.UnixAddr{Name: "agent", Net: "pipe"} }

func (s *stdioConn) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	s.readDeadline.at = t
	s.writeDeadline.at = t
	s.armLocked(&s.readDeadline)
	s.armLocked(&s.writeDeadline)
	return nil
}
func (s *stdioConn) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	s.readDeadline.at = t
	s.armLocked(&s.readDeadline)
	return nil
}
func (s *stdioConn) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	s.writeDeadline.at = t
	s.armLocked(&s.writeDeadline)
	return nil
}
