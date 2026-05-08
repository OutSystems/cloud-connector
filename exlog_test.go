package main

import (
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureExlogEmitter swaps the package-level exlogEmitter for a thread-safe
// recorder for the duration of a test, returning a getter and a restore fn.
func captureExlogEmitter(t *testing.T) (func() []exlogEntry, func()) {
	t.Helper()
	orig := exlogEmitter
	var mu sync.Mutex
	var got []exlogEntry
	exlogEmitter = func(e exlogEntry) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
	}
	return func() []exlogEntry {
			mu.Lock()
			defer mu.Unlock()
			out := make([]exlogEntry, len(got))
			copy(out, got)
			return out
		}, func() {
			exlogEmitter = orig
		}
}

// startEchoServer starts a TCP server that echoes one read back and returns
// its listen address. It runs until the test ends.
func startEchoServer(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				_, _ = c.Write(buf[:n])
			}(c)
		}
	}()
	return ln
}

func Test_parseReverseTCPRemote(t *testing.T) {
	tests := []struct {
		in       string
		wantOK   bool
		wantHost string
		wantPort string
	}{
		{"R:8081:192.168.0.3:8393", true, "192.168.0.3", "8393"},
		{"R:127.0.0.1:8081:internal:443", true, "internal", "443"},
		{"R:8081:host:8393/tcp", true, "host", "8393"},
		{"R:8081:host:8393/udp", false, "", ""},
		{"8081:host:8393", false, "", ""},  // not reverse
		{"R:abc:host:8393", false, "", ""}, // non-numeric local port
		{"R:8081:host:xyz", false, "", ""}, // non-numeric remote port
		{"R:8081", false, "", ""},          // too few parts
		{"R::host:8393", false, "", ""},    // empty local port
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parseReverseTCPRemote(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.targetHost != tt.wantHost {
				t.Errorf("targetHost = %q, want %q", got.targetHost, tt.wantHost)
			}
			if got.targetPort != tt.wantPort {
				t.Errorf("targetPort = %q, want %q", got.targetPort, tt.wantPort)
			}
		})
	}
}

func Test_startExlogProxies_passthrough(t *testing.T) {
	// UDP and non-reverse remotes should be passed through unchanged so that
	// chisel can handle (or reject) them as it normally would.
	in := []string{
		"R:8081:host:8393/udp",
		"8081:host:8393",
		"R:8082:host:8393",
	}
	out, proxies, err := startExlogProxies(in)
	if err != nil {
		t.Fatalf("startExlogProxies: %v", err)
	}
	t.Cleanup(func() { stopExlogProxies(proxies) })

	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3", len(out))
	}
	if out[0] != in[0] {
		t.Errorf("UDP remote was rewritten: %q", out[0])
	}
	if out[1] != in[1] {
		t.Errorf("non-reverse remote was rewritten: %q", out[1])
	}
	// The third one *should* be rewritten to point at the local proxy.
	if !strings.HasPrefix(out[2], "R:8082:127.0.0.1:") {
		t.Errorf("expected reverse remote to be rewritten, got %q", out[2])
	}
	if len(proxies) != 1 {
		t.Errorf("expected 1 active proxy, got %d", len(proxies))
	}
}

func Test_exlogProxy_successfulForward(t *testing.T) {
	getEntries, restore := captureExlogEmitter(t)
	defer restore()

	echo := startEchoServer(t)
	echoAddr := echo.Addr().String()
	host, port, _ := net.SplitHostPort(echoAddr)

	original := "R:9999:" + host + ":" + port
	rewritten, proxies, err := startExlogProxies([]string{original})
	if err != nil {
		t.Fatalf("startExlogProxies: %v", err)
	}
	defer stopExlogProxies(proxies)

	// Extract the proxy address from the rewritten remote: R:9999:127.0.0.1:<proxy>
	parts := strings.Split(rewritten[0], ":")
	if len(parts) != 4 {
		t.Fatalf("unexpected rewritten form: %s", rewritten[0])
	}
	proxyAddr := net.JoinHostPort(parts[2], parts[3])

	// Simulate chisel delivering a tunnel connection to our proxy.
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	payload := []byte("hello-exlog")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Half-close so the echo server returns and our proxy goroutines unwind.
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
	// Read echoed bytes.
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("echo mismatch: got %q, want %q", got, payload)
	}
	_ = conn.Close()

	// Wait for both exlog entries (open + close) to be emitted.
	deadline := time.Now().Add(2 * time.Second)
	var entries []exlogEntry
	for time.Now().Before(deadline) {
		entries = getEntries()
		if len(entries) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 exlog entries (open+close), got %d", len(entries))
	}

	open, closed := entries[0], entries[1]
	if open.Event != "open" {
		t.Errorf("entries[0].Event = %q, want %q", open.Event, "open")
	}
	if closed.Event != "close" {
		t.Errorf("entries[1].Event = %q, want %q", closed.Event, "close")
	}
	if open.ConnID == 0 {
		t.Errorf("ConnID should be non-zero")
	}
	if open.ConnID != closed.ConnID {
		t.Errorf("conn_id mismatch: open=%d close=%d", open.ConnID, closed.ConnID)
	}
	if open.SourceAddr != closed.SourceAddr {
		t.Errorf("source_addr mismatch between open and close events")
	}
	if open.RouteRule != original {
		t.Errorf("open.RouteRule = %q, want %q", open.RouteRule, original)
	}
	if closed.Destination != echoAddr {
		t.Errorf("close.Destination = %q, want %q", closed.Destination, echoAddr)
	}
	if !closed.Success {
		t.Errorf("close.Success = false, want true; error=%q", closed.Error)
	}
	if closed.BytesIn != int64(len(payload)) {
		t.Errorf("BytesIn = %d, want %d", closed.BytesIn, len(payload))
	}
	if closed.BytesOut != int64(len(payload)) {
		t.Errorf("BytesOut = %d, want %d", closed.BytesOut, len(payload))
	}
	if open.Timestamp == "" || closed.Timestamp == "" {
		t.Errorf("Timestamp empty (open=%q close=%q)", open.Timestamp, closed.Timestamp)
	}
	if closed.LatencyMs < 0 {
		t.Errorf("LatencyMs negative: %d", closed.LatencyMs)
	}
}

func Test_exlogProxy_targetUnreachable(t *testing.T) {
	getEntries, restore := captureExlogEmitter(t)
	defer restore()

	// Reserve a port and immediately close it so the target is guaranteed
	// to refuse connections.
	tmp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	deadAddr := tmp.Addr().String()
	host, port, _ := net.SplitHostPort(deadAddr)
	_ = tmp.Close()

	// Tighten the dial timeout so the test stays fast even on slow CI.
	origTimeout := exlogDialTimeout
	exlogDialTimeout = 500 * time.Millisecond
	defer func() { exlogDialTimeout = origTimeout }()

	original := "R:9998:" + host + ":" + port
	rewritten, proxies, err := startExlogProxies([]string{original})
	if err != nil {
		t.Fatalf("startExlogProxies: %v", err)
	}
	defer stopExlogProxies(proxies)

	parts := strings.Split(rewritten[0], ":")
	proxyAddr := net.JoinHostPort(parts[2], parts[3])

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	// On a closed target the proxy will close us right back.
	_, _ = io.ReadAll(conn)
	_ = conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	var entries []exlogEntry
	for time.Now().Before(deadline) {
		entries = getEntries()
		if len(entries) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 exlog entries (open+close), got %d", len(entries))
	}
	open, closed := entries[0], entries[1]
	if open.Event != "open" || closed.Event != "close" {
		t.Fatalf("unexpected event sequence: open=%q close=%q", open.Event, closed.Event)
	}
	if open.ConnID != closed.ConnID || open.ConnID == 0 {
		t.Errorf("conn_id mismatch or zero: open=%d close=%d", open.ConnID, closed.ConnID)
	}
	if closed.Success {
		t.Errorf("close.Success = true, want false")
	}
	if closed.Error == "" {
		t.Errorf("close.Error empty, want a dial failure message")
	}
	if closed.BytesIn != 0 || closed.BytesOut != 0 {
		t.Errorf("expected zero bytes on failure, got in=%d out=%d", closed.BytesIn, closed.BytesOut)
	}
}
