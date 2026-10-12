package agent

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// stdioConn transports TLS over pipes; it never opens a network socket.
type stdioConn struct {
	reader io.Reader
	input  io.ReadCloser
	output io.WriteCloser
	once   sync.Once
	mu     sync.Mutex // protects deadline timer
	timer  *time.Timer
}

func (s *stdioConn) Read(b []byte) (int, error) {
	return s.reader.Read(b)
}

func (s *stdioConn) Write(b []byte) (int, error) {
	return s.output.Write(b)
}

func (s *stdioConn) Close() error {
	var err error
	s.once.Do(func() {
		s.mu.Lock()
		if s.timer != nil {
			s.timer.Stop()
		}

		s.mu.Unlock()
		err = errors.Join(s.input.Close(), s.output.Close())
	})

	return err
}

func (s *stdioConn) LocalAddr() net.Addr {
	return &net.UnixAddr{Name: "trap", Net: "pipe"}
}

func (s *stdioConn) RemoteAddr() net.Addr {
	return &net.UnixAddr{Name: "agent", Net: "pipe"}
}

func (s *stdioConn) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.timer != nil {
		s.timer.Stop()
	}

	if !t.IsZero() {
		s.timer = time.AfterFunc(time.Until(t), func() {
			s.Close()
		})
	}

	return nil
}

func (s *stdioConn) SetReadDeadline(t time.Time) error {
	return s.SetDeadline(t)
}

func (s *stdioConn) SetWriteDeadline(t time.Time) error {
	return s.SetDeadline(t)
}
