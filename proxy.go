package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

type proxyConfig struct {
	listenAddr  string
	allowlist   []string
	allowAll    bool
	dialTimeout time.Duration
	verbose     bool
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

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
				default:
					log.Printf("[WARN] http-proxy: accept: %v", err)
				}
				return
			}
			go handleConnect(ctx, conn, al, cfg.dialTimeout, cfg.verbose)
		}
	}()

	return ln, nil
}

func handleConnect(ctx context.Context, clientConn net.Conn, al *allowlist, dialTimeout time.Duration, verbose bool) {
	defer clientConn.Close()

	br := bufio.NewReader(clientConn)

	method, authority, err := parseConnectRequest(br)
	if err != nil {
		writeProxyStatus(clientConn, 400, "Bad Request")
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

	fmt.Fprint(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	splice(ctx, clientConn, targetConn, br)
}

// parseConnectRequest reads the HTTP request line and drains the headers.
// It returns the method and authority (host:port), validating the authority format.
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

	// Drain headers until blank line.
	for {
		line, err := br.ReadString('\n')
		if err != nil || strings.TrimRight(line, "\r\n") == "" {
			break
		}
	}

	return method, authority, nil
}

// splice bidirectionally copies between clientConn and targetConn until one
// side closes or ctx is cancelled. clientBuf may contain already-buffered
// bytes from the client (data after the CONNECT headers).
func splice(ctx context.Context, clientConn net.Conn, targetConn net.Conn, clientBuf *bufio.Reader) {
	errc := make(chan error, 2)

	go func() {
		_, err := io.Copy(targetConn, clientBuf)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, targetConn)
		errc <- err
	}()

	select {
	case <-ctx.Done():
	case <-errc:
	}
}

func writeProxyStatus(w io.Writer, code int, status string) {
	fmt.Fprintf(w, "HTTP/1.1 %d %s\r\n\r\n", code, status)
}

// validateProxyFlags returns an error for any invalid combination of proxy flags.
func validateProxyFlags(gatewayPort, listenPort int, allow []string, allowAll bool) error {
	if !isValidPort(gatewayPort) {
		return fmt.Errorf("--http-proxy-gateway-port %d is not a valid TCP port (1-65535)", gatewayPort)
	}
	if !isValidPort(listenPort) {
		return fmt.Errorf("--http-proxy-listen-port %d is not a valid TCP port (1-65535)", listenPort)
	}
	if gatewayPort == listenPort {
		return fmt.Errorf("--http-proxy-gateway-port and --http-proxy-listen-port must differ (both are %d)", gatewayPort)
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
