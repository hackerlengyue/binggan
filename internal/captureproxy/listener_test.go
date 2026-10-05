package captureproxy

import (
	"errors"
	"net"
	"testing"
	"time"
)

// Hold a successfully accepted connection before registration, exactly where
// shutdown could previously miss it in the active-connection snapshot.
type delayedListener struct {
	net.Listener
	conn     net.Conn
	accepted chan struct{}
	release  chan struct{}
}

func (l *delayedListener) Accept() (net.Conn, error) {
	close(l.accepted)
	<-l.release
	return l.conn, nil
}

func TestShutdownRejectsLateAcceptedConnection(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	base := &delayedListener{conn: server, accepted: make(chan struct{}), release: make(chan struct{})}
	l := &trackedListener{Listener: base, connections: map[*trackedConn]bool{}}
	done := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if conn != nil {
			defer conn.Close()
		}
		done <- err
	}()
	<-base.accepted
	l.closeConnections()
	close(base.release)
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("late accept = %v; want net.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("accept did not complete")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.connections) != 0 {
		t.Fatal("connection registered after shutdown")
	}
}
