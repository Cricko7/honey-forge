// Package tcpserver owns the bounded connection lifecycle shared by TCP decoys.
// Protocols and telemetry remain in the calling decoy package.
package tcpserver

import (
	"context"
	"net"
	"sync"
)

type Listener struct {
	Listener net.Listener
	Serve    func(context.Context, net.Conn) error
}

type Server struct {
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex // protects connections and the first session/accept failure
	conns  map[net.Conn]struct{}
	err    error
}

// Start takes ownership of already-bound listeners. All listeners must be bound
// before calling it, so a failed bind never leaves a partially running decoy.
func Start(ctx context.Context, listeners []Listener, maxSessions int) *Server {
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{cancel: cancel, done: make(chan struct{}), conns: make(map[net.Conn]struct{})}
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(closed)

		for _, l := range listeners {
			// Close errors during teardown do not replace the protocol failure.
			_ = l.Listener.Close()
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		for conn := range s.conns {
			_ = conn.Close()
		}
	})
	sessions := make(chan struct{}, maxSessions)
	for _, l := range listeners {
		s.wg.Go(func() {
			s.accept(ctx, l, sessions)
		})
	}

	go func() {
		s.wg.Wait()
		cancel()
		if !stop() {
			<-closed
		}

		close(s.done)
	}()

	return s
}

func (s *Server) accept(ctx context.Context, listener Listener, sessions chan struct{}) {
	for {
		conn, err := listener.Listener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				s.fail(err)
			}

			return
		}

		select {
		case sessions <- struct{}{}:
		case <-ctx.Done():
			_ = conn.Close()
			return
		}

		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			<-sessions
			_ = conn.Close()

			return
		}

		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		// The accept goroutine keeps wg nonempty while adding session work.
		s.wg.Go(func() {
			defer func() {
				<-sessions
			}()
			defer func() {
				_ = conn.Close()
				s.mu.Lock()
				delete(s.conns, conn)
				s.mu.Unlock()
			}()

			if err := listener.Serve(ctx, conn); err != nil {
				s.fail(err)
			}
		})
	}
}

func (s *Server) fail(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}

	s.mu.Unlock()
	s.cancel()
}

func (s *Server) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.err
}

func (s *Server) Stop() {
	s.cancel()
	<-s.done
}
