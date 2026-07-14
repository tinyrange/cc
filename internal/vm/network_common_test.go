package vm

import (
	"io"
	"net"
	"testing"
)

type queuedListener struct{ conns []net.Conn }

func (l *queuedListener) Accept() (net.Conn, error) {
	if len(l.conns) == 0 {
		return nil, io.EOF
	}
	conn := l.conns[0]
	l.conns = l.conns[1:]
	return conn, nil
}
func (*queuedListener) Close() error   { return nil }
func (*queuedListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestPortForwardFloodIsRejectedBeforeConnectionWorkers(t *testing.T) {
	listener := &queuedListener{}
	var peers []net.Conn
	for i := 0; i < 3; i++ {
		server, peer := net.Pipe()
		listener.conns = append(listener.conns, server)
		peers = append(peers, peer)
		defer peer.Close()
	}
	runtime := &networkRuntime{forwardSlots: make(chan struct{}, 1)}
	runtime.forwardSlots <- struct{}{}
	runtime.wg.Add(1)
	runtime.acceptPortForward(listener, "10.42.0.2:80", make(chan struct{}, 2))
	stats := runtime.PortForwardStats()
	if stats.Active != 0 || stats.Rejected != 3 || stats.Limit != 1 {
		t.Fatalf("forward stats = %+v", stats)
	}
	for i, peer := range peers {
		buf := make([]byte, 1)
		if _, err := peer.Read(buf); err == nil {
			t.Fatalf("rejected connection %d remained open", i)
		}
	}
}
