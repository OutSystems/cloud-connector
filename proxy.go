package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type proxyConfig struct {
	listenAddr       string
	allowlist        []string
	allowAll         bool
	dialTimeout      time.Duration
	headerTimeout    time.Duration
	maxConns         int
	idleTimeout      time.Duration
	verbose          bool
}

type allowlist struct {
	entries map[string]struct{}
	all     bool
}

func newAllowlist(entries []string, allowAll bool) (*allowlist, error) {
	al := &allowlist{
		entries: make(map[string]struct{}, len(entries)),
		all:     allowAll,
	}
	for _, e := range entries {
		normalized, err := normalizeTarget(e)
		if err != nil {
			return nil, fmt.Errorf("invalid --http-proxy-allow entry %q: %w", e, err)
		}
		al.entries[normalized] = struct{}{}
	}
	return al, nil
}

// normalizeTarget lowercases the host and validates the host:port format.
func normalizeTarget(target string) (string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return "", err
	}
	if port == "" {
		return "", fmt.Errorf("port is required in %q", target)
	}
	return net.JoinHostPort(strings.ToLower(host), port), nil
}

func (al *allowlist) allows(target string) bool {
	if al.all {
		return true
	}
	normalized, err := normalizeTarget(target)
	if err != nil {
		return false
	}
	_, ok := al.entries[normalized]
	return ok
}

// runProxy starts the embedded HTTP CONNECT proxy listener and returns it.
// The listener is closed when ctx is cancelled. Startup failure is returned
// as an error; the caller should treat it as fatal.
func runProxy(ctx context.Context, cfg proxyConfig) (net.Listener, error) {
	al, err := newAllowlist(cfg.allowlist, cfg.allowAll)
	if err != nil {
		return nil, err
	}

	ln, err := net.Listen("tcp", cfg.listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", cfg.listenAddr, err)
	}

	log.Printf("[INFO] http-proxy: listening on %s", ln.Addr())

	sem := make(chan struct{}, cfg.maxConns)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	go func() {
		var tempDelay time.Duration
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if tempDelay > 1*time.Second {
					tempDelay = 1 * time.Second
				}
				log.Printf("[WARN] http-proxy: accept error, retrying in %v: %v", tempDelay, err)
				time.Sleep(tempDelay)
				continue
			}
			tempDelay = 0

			select {
			case sem <- struct{}{}:
				go func(c net.Conn) {
					defer func() { <-sem }()
					handleConnect(ctx, c, al, cfg.headerTimeout, cfg.dialTimeout, cfg.idleTimeout, cfg.verbose)
				}(conn)
			default:
				writeProxyStatus(conn, 503, "Service Unavailable")
				conn.Close()
			}
		}
	}()

	return ln, nil
}

func handleConnect(ctx context.Context, clientConn net.Conn, al *allowlist, headerTimeout, dialTimeout, idleTimeout time.Duration, verbose bool) {
	defer clientConn.Close()

	clientConn.SetReadDeadline(time.Now().Add(headerTimeout))

	br := bufio.NewReader(clientConn)

	method, authority, err := parseConnectRequest(br)
	if err != nil {
		status := 400
		if strings.Contains(err.Error(), "too many headers") {
			status = 431
		}
		writeProxyStatus(clientConn, status, http.StatusText(status))
		return
	}

	if !strings.EqualFold(method, "CONNECT") {
		if verbose {
			log.Printf("[INFO] http-proxy: rejected non-CONNECT method %q", method)
		}
		writeProxyStatus(clientConn, 405, "Method Not Allowed")
		return
	}

	if !al.allows(authority) {
		if verbose {
			log.Printf("[INFO] http-proxy: CONNECT %s denied by allowlist", authority)
		}
		writeProxyStatus(clientConn, 403, "Forbidden")
		return
	}

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	targetConn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", authority)
	if err != nil {
		if verbose {
			log.Printf("[INFO] http-proxy: CONNECT %s dial failed: %v", authority, err)
		}
		writeProxyStatus(clientConn, 502, "Bad Gateway")
		return
	}
	defer targetConn.Close()

	if verbose {
		log.Printf("[INFO] http-proxy: CONNECT %s established", authority)
	}

	if _, err := fmt.Fprint(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		if verbose {
			log.Printf("[INFO] http-proxy: failed to write 200 response: %v", err)
		}
		return
	}

	clientConn.SetDeadline(time.Time{})
	splice(ctx, clientConn, targetConn, br, idleTimeout)
}

// parseConnectRequest reads the HTTP request line and drains the headers.
// It returns the method and authority (host:port), validating the authority format.
// Limits: 8 KB total, 100 headers max.
func parseConnectRequest(br *bufio.Reader) (method, authority string, err error) {
	requestLine, err := br.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("read request line: %w", err)
	}
	requestLine = strings.TrimRight(requestLine, "\r\n")

	parts := strings.SplitN(requestLine, " ", 3)
	if len(parts) != 3 {
		return "", "", fmt.Errorf("malformed request line: %q", requestLine)
	}

	method, authority = parts[0], parts[1]

	if _, _, err := net.SplitHostPort(authority); err != nil {
		return "", "", fmt.Errorf("malformed authority %q: %w", authority, err)
	}

	// Drain headers until blank line (max 100 headers, 8 KB total).
	const maxHeaderBytes = 8192
	const maxHeaderCount = 100

	totalBytes := len(requestLine)
	headerCount := 0

	for {
		if headerCount >= maxHeaderCount {
			return "", "", fmt.Errorf("too many headers")
		}
		line, err := br.ReadString('\n')
		if err != nil {
			return "", "", fmt.Errorf("read headers: %w", err)
		}
		totalBytes += len(line)
		if totalBytes > maxHeaderBytes {
			return "", "", fmt.Errorf("headers too large")
		}
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
		headerCount++
	}

	return method, authority, nil
}

// splice bidirectionally copies between clientConn and targetConn until one
// side closes, context is cancelled, or idle timeout elapses. clientBuf may contain
// already-buffered bytes from the client (data after the CONNECT headers).
// Idle timeout resets on activity in either direction.
// Half-close: when one direction completes, signal with CloseWrite and bound drain to 1s.
func splice(ctx context.Context, clientConn net.Conn, targetConn net.Conn, clientBuf *bufio.Reader, idleTimeout time.Duration) {
	errc := make(chan error, 2)
	activityc := make(chan struct{}, 1)

	go func() {
		_, err := io.Copy(targetConn, &activityMonitor{Reader: clientBuf, activity: activityc})
		if tc, ok := targetConn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		errc <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, &activityMonitor{Reader: targetConn, activity: activityc})
		if tc, ok := clientConn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		errc <- err
	}()

	if idleTimeout <= 0 {
		select {
		case <-ctx.Done():
		case <-errc:
		case <-errc:
		}
		clientConn.Close()
		targetConn.Close()
		return
	}

	idleTicker := time.NewTicker(idleTimeout / 2)
	defer idleTicker.Stop()
	lastActivity := time.Now()
	doneCount := 0

	for {
		select {
		case <-ctx.Done():
			clientConn.Close()
			targetConn.Close()
			return
		case <-errc:
			doneCount++
			if doneCount == 1 {
				select {
				case <-errc:
					doneCount++
				case <-time.After(1 * time.Second):
				case <-ctx.Done():
				}
			}
			clientConn.Close()
			targetConn.Close()
			return
		case <-activityc:
			lastActivity = time.Now()
		case <-idleTicker.C:
			if time.Since(lastActivity) > idleTimeout {
				clientConn.Close()
				targetConn.Close()
				return
			}
		}
	}
}

type activityMonitor struct {
	Reader   io.Reader
	activity chan struct{}
}

func (am *activityMonitor) Read(p []byte) (int, error) {
	n, err := am.Reader.Read(p)
	if n > 0 {
		select {
		case am.activity <- struct{}{}:
		default:
		}
	}
	return n, err
}

func writeProxyStatus(w io.Writer, code int, status string) {
	fmt.Fprintf(w, "HTTP/1.1 %d %s\r\n\r\n", code, status)
}

// validateProxyFlags returns an error for any invalid combination of proxy flags.
func validateProxyFlags(gatewayPort, listenPort, maxConns int, headerTimeout, idleTimeout time.Duration, allow []string, allowAll bool) error {
	if !isValidPort(gatewayPort) {
		return fmt.Errorf("--http-proxy-gateway-port %d is not a valid TCP port (1-65535)", gatewayPort)
	}
	if !isValidPort(listenPort) {
		return fmt.Errorf("--http-proxy-listen-port %d is not a valid TCP port (1-65535)", listenPort)
	}
	if gatewayPort == listenPort {
		return fmt.Errorf("--http-proxy-gateway-port and --http-proxy-listen-port must differ (both are %d)", gatewayPort)
	}
	if maxConns < 1 {
		return fmt.Errorf("--http-proxy-max-conns %d must be at least 1", maxConns)
	}
	if headerTimeout < 1*time.Second {
		return fmt.Errorf("--http-proxy-header-timeout %v must be at least 1s", headerTimeout)
	}
	if idleTimeout < 0 {
		return fmt.Errorf("--http-proxy-idle-timeout %v must be non-negative", idleTimeout)
	}
	if allowAll && len(allow) > 0 {
		return fmt.Errorf("--http-proxy-allow-all and --http-proxy-allow are mutually exclusive")
	}
	if !allowAll && len(allow) == 0 {
		return fmt.Errorf("--http-proxy is enabled but neither --http-proxy-allow nor --http-proxy-allow-all was provided")
	}
	for _, entry := range allow {
		if _, err := normalizeTarget(entry); err != nil {
			return fmt.Errorf("invalid --http-proxy-allow entry %q: %w", entry, err)
		}
	}
	return nil
}

func isValidPort(p int) bool {
	return p >= 1 && p <= 65535
}
