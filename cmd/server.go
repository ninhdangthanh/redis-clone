package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"redis-clone/internal/aof"
	"redis-clone/internal/pubsub"
	"redis-clone/internal/store"
	"sync"
	"sync/atomic"
	"time"
)

// Server owns the resources whose lifetimes must be coordinated during shutdown.
type Server struct {
	listener net.Listener
	store    *store.Store
	aof      *aof.AOF
	pubsub   *pubsub.Hub
	executor *CommandExecutor

	ctx    context.Context
	cancel context.CancelFunc

	draining atomic.Bool
	connsMu  sync.Mutex
	conns    map[net.Conn]struct{}
	connWG   sync.WaitGroup
	workers  []<-chan struct{}

	shutdownMu      sync.Mutex
	shutdownStarted bool
	shutdownDone    chan struct{}
	shutdownErr     error
}

func NewServer(listener net.Listener, st *store.Store, aofFile *aof.AOF, hub *pubsub.Hub) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		listener: listener,
		store:    st,
		aof:      aofFile,
		pubsub:   hub,
		ctx:      ctx,
		cancel:   cancel,
		conns:    make(map[net.Conn]struct{}),
	}
	server.executor = NewCommandExecutor(ctx, st, aofFile, hub)
	return server
}

func (s *Server) StartWorkers() {
	s.workers = append(s.workers,
		s.store.StartTTLCheckerContext(s.ctx, time.Second),
		s.store.StartEvictionCheckerContext(s.ctx, time.Second),
	)
}

// Serve accepts connections until Shutdown closes the listener.
func (s *Server) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.draining.Load() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept connection: %w", err)
		}
		s.trackConnection(conn)
	}
}

func (s *Server) trackConnection(conn net.Conn) {
	s.connsMu.Lock()
	if s.draining.Load() {
		s.connsMu.Unlock()
		_ = conn.Close()
		return
	}
	s.conns[conn] = struct{}{}
	s.connWG.Add(1)
	s.connsMu.Unlock()

	go func() {
		defer s.connWG.Done()
		defer s.removeConnection(conn)
		handleConn(s.ctx, conn, s.executor, s.pubsub)
	}()
}

func (s *Server) removeConnection(conn net.Conn) {
	s.connsMu.Lock()
	delete(s.conns, conn)
	s.connsMu.Unlock()
}

// Shutdown first releases the TCP port, then closes clients to unblock reads,
// waits for in-flight commands, stops workers, and durably closes the AOF.
func (s *Server) Shutdown(ctx context.Context) error {
	s.shutdownMu.Lock()
	if s.shutdownStarted {
		done := s.shutdownDone
		s.shutdownMu.Unlock()
		select {
		case <-done:
			s.shutdownMu.Lock()
			err := s.shutdownErr
			s.shutdownMu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.shutdownStarted = true
	s.shutdownDone = make(chan struct{})
	s.shutdownMu.Unlock()

	err := s.shutdown(ctx)

	s.shutdownMu.Lock()
	s.shutdownErr = err
	close(s.shutdownDone)
	s.shutdownMu.Unlock()
	return err
}

func (s *Server) shutdown(ctx context.Context) error {
	s.draining.Store(true)
	var shutdownErr error
	if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		shutdownErr = fmt.Errorf("close listener: %w", err)
	}

	// Cancelling a context cannot interrupt a blocked network Read, so sockets
	// must be closed as well.
	s.cancel()
	s.closeConnections()
	if err := waitGroupContext(ctx, &s.connWG); err != nil {
		return errors.Join(shutdownErr, fmt.Errorf("wait for clients: %w", err))
	}
	if err := waitChannels(ctx, s.workers); err != nil {
		return errors.Join(shutdownErr, fmt.Errorf("wait for workers: %w", err))
	}
	if err := s.executor.Stop(ctx); err != nil {
		return errors.Join(shutdownErr, fmt.Errorf("wait for command executor: %w", err))
	}
	if err := s.aof.Close(); err != nil {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("close aof: %w", err))
	}
	return shutdownErr
}

func (s *Server) closeConnections() {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	for conn := range s.conns {
		_ = conn.Close()
	}
}

func waitChannels(ctx context.Context, channels []<-chan struct{}) error {
	for _, done := range channels {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func waitGroupContext(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
