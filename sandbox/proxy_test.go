package sandbox

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestProxyHandshakePreservesBufferedRelay(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(server).ReadString('\n')
		if err == nil && !strings.Contains(line, `"host":"localhost"`) {
			err = errors.New("destination lost")
		}
		if err == nil {
			_, err = io.WriteString(server, "{\"version\":1}\nhello")
		}
		done <- err
	}()
	conn, err := proxyHandshake(context.Background(), client, []byte("{\"version\":1,\"host\":\"localhost\",\"port\":\"8080\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	data := make([]byte, 5)
	if _, err := io.ReadFull(conn, data); err != nil || string(data) != "hello" {
		t.Fatalf("relay %q: %v", data, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProxyHandshakeRejectsResponses(t *testing.T) {
	for _, response := range []string{"{\"version\":2}\n", "{\"version\":1,\"error\":\"refused\"}\n", "garbage\n", strings.Repeat("x", proxyHeaderLimit+1) + "\n"} {
		t.Run(response[:min(len(response), 30)], func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			done := make(chan struct{})
			go func() { defer close(done); bufio.NewReader(server).ReadString('\n'); io.WriteString(server, response) }()
			if c, err := proxyHandshake(context.Background(), client, []byte("{}\n")); err == nil {
				c.Close()
				t.Fatal("accepted invalid response")
			}
			<-done
		})
	}
}

func TestProxyHandshakeCancellation(t *testing.T) {
	for _, blockWrite := range []bool{true, false} {
		client, server := net.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := proxyHandshake(ctx, client, []byte("{}\n")); done <- err }()
		if !blockWrite {
			if _, err := bufio.NewReader(server).ReadString('\n'); err != nil {
				t.Fatal(err)
			}
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancellation did not interrupt handshake")
		}
		server.Close()
	}
}

func TestProxyHandshakeDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := proxyHandshake(ctx, client, []byte("{}\n")); err == nil {
		t.Fatal("deadline not enforced")
	}
}
