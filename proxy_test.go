package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// --- allowlist ---

func TestAllowlist_Allows(t *testing.T) {
	tests := []struct {
		name     string
		entries  []string
		allowAll bool
		target   string
		want     bool
	}{
		{
			name:    "exact match allowed",
			entries: []string{"api.corp:443"},
			target:  "api.corp:443",
			want:    true,
		},
		{
			name:    "not in allowlist denied",
			entries: []string{"api.corp:443"},
			target:  "other.corp:443",
			want:    false,
		},
		{
			name:    "case normalization - uppercase in entry",
			entries: []string{"API.CORP:443"},
			target:  "api.corp:443",
			want:    true,
		},
		{
			name:    "case normalization - uppercase in target",
			entries: []string{"api.corp:443"},
			target:  "API.CORP:443",
			want:    true,
		},
		{
			name:    "port mismatch denied",
			entries: []string{"api.corp:443"},
			target:  "api.corp:8443",
			want:    false,
		},
		{
			name:    "malformed target denied",
			entries: []string{"api.corp:443"},
			target:  "no-port-here",
			want:    false,
		},
		{
			name:     "allow-all bypasses allowlist",
			entries:  nil,
			allowAll: true,
			target:   "anything.internal:9999",
			want:     true,
		},
		{
			name:    "empty allowlist denies everything",
			entries: []string{},
			target:  "api.corp:443",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			al, err := newAllowlist(tt.entries, tt.allowAll)
			if err != nil {
				t.Fatalf("newAllowlist() unexpected error: %v", err)
			}
			if got := al.allows(tt.target); got != tt.want {
				t.Errorf("allows(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestNewAllowlist_InvalidEntry(t *testing.T) {
	_, err := newAllowlist([]string{"no-port"}, false)
	if err == nil {
		t.Fatal("newAllowlist() expected error for malformed entry, got nil")
	}
	if !strings.Contains(err.Error(), "--http-proxy-allow") {
		t.Errorf("error %q should mention --http-proxy-allow", err)
	}
}

// --- parseConnectRequest ---

func TestParseConnectRequest(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantMethod    string
		wantAuthority string
		wantErr       bool
	}{
		{
			name:          "valid CONNECT",
			input:         "CONNECT real-api.corp:443 HTTP/1.1\r\nHost: real-api.corp:443\r\n\r\n",
			wantMethod:    "CONNECT",
			wantAuthority: "real-api.corp:443",
		},
		{
			name:          "non-CONNECT method parses successfully (method rejection is handleConnect's job)",
			input:         "GET example.com:80 HTTP/1.1\r\nHost: example.com\r\n\r\n",
			wantMethod:    "GET",
			wantAuthority: "example.com:80",
		},
		{
			name:    "malformed request line - too few parts",
			input:   "CONNECT\r\n\r\n",
			wantErr: true,
		},
		{
			name:    "malformed authority - no port",
			input:   "CONNECT real-api.corp HTTP/1.1\r\nHost: real-api.corp\r\n\r\n",
			wantErr: true,
		},
		{
			name:          "CONNECT with extra headers",
			input:         "CONNECT db.internal:5432 HTTP/1.1\r\nHost: db.internal:5432\r\nProxy-Connection: Keep-Alive\r\n\r\n",
			wantMethod:    "CONNECT",
			wantAuthority: "db.internal:5432",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br := bufio.NewReader(strings.NewReader(tt.input))
			method, authority, err := parseConnectRequest(br)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseConnectRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if method != tt.wantMethod {
				t.Errorf("method = %q, want %q", method, tt.wantMethod)
			}
			if authority != tt.wantAuthority {
				t.Errorf("authority = %q, want %q", authority, tt.wantAuthority)
			}
		})
	}
}

// --- validateProxyFlags ---

func TestValidateProxyFlags(t *testing.T) {
	tests := []struct {
		name         string
		gatewayPort  int
		listenPort   int
		allow        []string
		allowAll     bool
		wantErr      bool
		errSubstring string
	}{
		{
			name:        "valid with explicit allowlist",
			gatewayPort: 8080,
			listenPort:  18080,
			allow:       []string{"api.corp:443"},
		},
		{
			name:        "valid with allow-all",
			gatewayPort: 8080,
			listenPort:  18080,
			allowAll:    true,
		},
		{
			name:         "missing allowlist and allow-all",
			gatewayPort:  8080,
			listenPort:   18080,
			wantErr:      true,
			errSubstring: "neither --http-proxy-allow nor --http-proxy-allow-all",
		},
		{
			name:         "allow and allow-all mutually exclusive",
			gatewayPort:  8080,
			listenPort:   18080,
			allow:        []string{"api.corp:443"},
			allowAll:     true,
			wantErr:      true,
			errSubstring: "mutually exclusive",
		},
		{
			name:         "equal ports",
			gatewayPort:  8080,
			listenPort:   8080,
			allow:        []string{"api.corp:443"},
			wantErr:      true,
			errSubstring: "must differ",
		},
		{
			name:         "invalid gateway port - zero",
			gatewayPort:  0,
			listenPort:   18080,
			allow:        []string{"api.corp:443"},
			wantErr:      true,
			errSubstring: "--http-proxy-gateway-port",
		},
		{
			name:         "invalid gateway port - too large",
			gatewayPort:  99999,
			listenPort:   18080,
			allow:        []string{"api.corp:443"},
			wantErr:      true,
			errSubstring: "--http-proxy-gateway-port",
		},
		{
			name:         "invalid listen port",
			gatewayPort:  8080,
			listenPort:   0,
			allow:        []string{"api.corp:443"},
			wantErr:      true,
			errSubstring: "--http-proxy-listen-port",
		},
		{
			name:         "malformed allow entry",
			gatewayPort:  8080,
			listenPort:   18080,
			allow:        []string{"no-port"},
			wantErr:      true,
			errSubstring: "--http-proxy-allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProxyFlags(tt.gatewayPort, tt.listenPort, tt.allow, tt.allowAll)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateProxyFlags() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errSubstring != "" && !strings.Contains(err.Error(), tt.errSubstring) {
				t.Errorf("error %q should contain %q", err, tt.errSubstring)
			}
		})
	}
}

// --- validateRemotes: Design A port-conflict detection ---

func TestValidateRemotes_ProxyPortConflict(t *testing.T) {
	// The synthesized proxy remote uses port 8080. A user-supplied remote with the
	// same local port must be rejected by the existing duplicate-port check.
	synthesized := "R:8080:127.0.0.1:18080"
	userSupplied := "R:8080:192.168.0.5:443"

	_, err := validateRemotes([]string{synthesized, userSupplied})
	if err == nil {
		t.Fatal("validateRemotes() expected error for duplicate local port, got nil")
	}
	if !strings.Contains(err.Error(), "duplicated") {
		t.Errorf("error %q should mention duplicated", err)
	}
}

// --- proxy integration ---

func TestProxy_Integration_CONNECT(t *testing.T) {
	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen backend: %v", err)
	}
	defer backendLn.Close()
	backendAddr := backendLn.Addr().String()

	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := runProxy(ctx, proxyConfig{
		listenAddr:  "127.0.0.1:0",
		allowlist:   []string{backendAddr},
		dialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("runProxy() error: %v", err)
	}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backendAddr, backendAddr)

	statusLine := readProxyLine(t, conn)
	if !strings.Contains(statusLine, "200 Connection Established") {
		t.Fatalf("expected 200, got %q", statusLine)
	}
	readProxyUntilBlankLine(t, conn)

	if _, err := fmt.Fprint(conn, "hello"); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 5)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if got := string(buf); got != "hello" {
		t.Errorf("echo = %q, want %q", got, "hello")
	}
}

func TestProxy_Integration_DeniedTarget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := runProxy(ctx, proxyConfig{
		listenAddr:  "127.0.0.1:0",
		allowlist:   []string{"allowed.internal:443"},
		dialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("runProxy() error: %v", err)
	}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "CONNECT denied.internal:443 HTTP/1.1\r\nHost: denied.internal:443\r\n\r\n")
	if line := readProxyLine(t, conn); !strings.Contains(line, "403") {
		t.Errorf("expected 403, got %q", line)
	}
}

func TestProxy_Integration_NonCONNECTMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := runProxy(ctx, proxyConfig{
		listenAddr:  "127.0.0.1:0",
		allowlist:   []string{"api.corp:443"},
		dialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("runProxy() error: %v", err)
	}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET api.corp:80 HTTP/1.1\r\nHost: api.corp\r\n\r\n")
	if line := readProxyLine(t, conn); !strings.Contains(line, "405") {
		t.Errorf("expected 405, got %q", line)
	}
}

func TestProxy_Integration_MalformedRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := runProxy(ctx, proxyConfig{
		listenAddr:  "127.0.0.1:0",
		allowlist:   []string{"api.corp:443"},
		dialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("runProxy() error: %v", err)
	}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "CONNECT no-port HTTP/1.1\r\nHost: no-port\r\n\r\n")
	if line := readProxyLine(t, conn); !strings.Contains(line, "400") {
		t.Errorf("expected 400, got %q", line)
	}
}

func TestProxy_Integration_UnreachableTarget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	unreachable := "127.0.0.1:1" // port 1 should not be listening

	ln, err := runProxy(ctx, proxyConfig{
		listenAddr:  "127.0.0.1:0",
		allowlist:   []string{unreachable},
		dialTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("runProxy() error: %v", err)
	}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", unreachable, unreachable)
	if line := readProxyLine(t, conn); !strings.Contains(line, "502") {
		t.Errorf("expected 502, got %q", line)
	}
}

func TestProxy_Integration_TLSPassthrough(t *testing.T) {
	// Generate a self-signed certificate with IP SAN for 127.0.0.1. The proxy
	// must pass TLS bytes verbatim so the client's TLS stack can verify the
	// certificate against the real backend hostname.
	tlsCert, certPool := generateTestCert(t, net.ParseIP("127.0.0.1"))

	backendLn, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	})
	if err != nil {
		t.Fatalf("listen backend: %v", err)
	}
	defer backendLn.Close()

	backendPort := backendLn.Addr().(*net.TCPAddr).Port
	backendTarget := fmt.Sprintf("127.0.0.1:%d", backendPort)

	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := runProxy(ctx, proxyConfig{
		listenAddr:  "127.0.0.1:0",
		allowlist:   []string{backendTarget},
		dialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("runProxy() error: %v", err)
	}

	rawConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer rawConn.Close()

	fmt.Fprintf(rawConn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", backendTarget, backendTarget)
	statusLine := readProxyLine(t, rawConn)
	if !strings.Contains(statusLine, "200 Connection Established") {
		t.Fatalf("expected 200, got %q", statusLine)
	}
	readProxyUntilBlankLine(t, rawConn)

	// Wrap the tunnel in TLS; certificate verification must succeed because the
	// proxy splices bytes without inspection, preserving end-to-end TLS.
	tlsConn := tls.Client(rawConn, &tls.Config{
		RootCAs:    certPool,
		ServerName: "127.0.0.1",
	})
	if err := tlsConn.Handshake(); err != nil {
		t.Errorf("TLS handshake failed: %v", err)
	}
}

func generateTestCert(t *testing.T, ip net.IP) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		IPAddresses:  []net.IP{ip},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	x509Cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(x509Cert)

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, pool
}

func readProxyLine(t *testing.T, conn net.Conn) string {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	var line []byte
	for {
		if _, err := conn.Read(buf); err != nil {
			t.Fatalf("readProxyLine: %v", err)
		}
		line = append(line, buf[0])
		if buf[0] == '\n' {
			break
		}
	}
	return strings.TrimRight(string(line), "\r\n")
}

func readProxyUntilBlankLine(t *testing.T, conn net.Conn) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	var line []byte
	for {
		if _, err := conn.Read(buf); err != nil {
			t.Fatalf("readProxyUntilBlankLine: %v", err)
		}
		if buf[0] == '\n' {
			if strings.TrimRight(string(line), "\r") == "" {
				return
			}
			line = line[:0]
		} else {
			line = append(line, buf[0])
		}
	}
}
