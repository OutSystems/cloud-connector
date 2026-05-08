package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// exlogEntry is a structured per-connection record emitted when --exlog is
// enabled. It is designed to answer the operational question:
//
//	"Did we receive traffic for R:<port>:<host>:<port>, and was the forward
//	to the target successful?"
//
// Two entries are emitted per inbound connection received from the chisel
// tunnel, sharing the same ConnID:
//
//	Event=="open"   when the connection is accepted from the tunnel.
//	                Answers "did we receive traffic?" at arrival-time.
//	Event=="close"  when the connection terminates.
//	                Carries the dial outcome and transferred-byte counts.
//
// Within the exlog stream, ConnID joins the two events for a single TCP
// connection. Across the chisel and exlog log streams, correlation is by
// time proximity and order: chisel logs "client: tun: conn#N: Open"
// immediately before dialling our local proxy, so its open event lands
// right before our matching exlogEntry{Event:"open"}.
type exlogEntry struct {
	Timestamp   string `json:"timestamp"`
	ConnID      uint64 `json:"conn_id"`
	Event       string `json:"event"`           // "open" or "close"
	RouteRule   string `json:"route_rule"`      // original "R:local:host:remote"
	Destination string `json:"destination"`     // resolved "host:port" of target
	SourceAddr  string `json:"source_addr"`     // remote addr of incoming conn (chisel side)
	Success     bool   `json:"success"`         // close-only: did dial to target succeed?
	LatencyMs   int64  `json:"latency_ms"`      // close-only: time to establish conn to target
	DurationMs  int64  `json:"duration_ms"`     // close-only: total connection lifetime
	BytesIn     int64  `json:"bytes_in"`        // close-only: bytes from tunnel -> target
	BytesOut    int64  `json:"bytes_out"`       // close-only: bytes from target -> tunnel
	Error       string `json:"error,omitempty"` // close-only: populated on failure
}

// nextConnID hands out monotonic identifiers used to tie the open/close
// events for a single proxied connection together. Reset on process start.
var nextConnID atomic.Uint64

// exlogEmitter is the sink for exlog entries. Tests override this to capture
// emitted records. The default writes a single tagged line per entry to the
// standard logger so operators can grep / pipe through jq:
//
//	outsystemscc --exlog ... 2>&1 | grep '\[exlog\]' | sed 's/.*\[exlog\] //' | jq .
var exlogEmitter = func(e exlogEntry) {
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("[exlog] failed to marshal entry: %v", err)
		return
	}
	log.Printf("[exlog] %s", string(b))
}

// exlogDialTimeout bounds how long we wait when dialing the target host.
// Exposed as a var (rather than a const) so tests can shorten it.
var exlogDialTimeout = 30 * time.Second

// exlogProxy interposes on a single reverse remote. It accepts connections
// from the chisel client and forwards them to the real target host:port,
// recording a per-connection exlog entry on the way.
type exlogProxy struct {
	routeRule string       // original remote string, e.g. "R:8081:192.168.0.3:8393"
	target    string       // dial address of the real target, "host:port"
	listener  net.Listener // local TCP listener chisel will dial into
}

// startExlogProxies inspects each remote and, for every reverse TCP remote,
// starts a local proxy on 127.0.0.1:<auto>. It returns a parallel slice of
// remotes with the host/port rewritten to point at the local proxy. The
// original local-port is preserved so that:
//   - validateRemotes' duplicate-port check still applies
//   - generateQueryParameters reports the correct ports to the server
//   - operators see the same R:<local-port>:... values they configured
//
// Non-reverse remotes, UDP remotes, and any remote we cannot parse are
// passed through unchanged with an informational log line.
func startExlogProxies(remotes []string) ([]string, []*exlogProxy, error) {
	rewritten := make([]string, 0, len(remotes))
	proxies := make([]*exlogProxy, 0, len(remotes))

	for _, r := range remotes {
		spec, ok := parseReverseTCPRemote(r)
		if !ok {
			log.Printf("[exlog] skipping non-reverse-TCP remote (passthrough): %s", r)
			rewritten = append(rewritten, r)
			continue
		}

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			// Best-effort cleanup of any proxies we already started.
			for _, p := range proxies {
				_ = p.listener.Close()
			}
			return nil, nil, fmt.Errorf("exlog: failed to start local proxy for %q: %w", r, err)
		}

		proxy := &exlogProxy{
			routeRule: r,
			target:    net.JoinHostPort(spec.targetHost, spec.targetPort),
			listener:  ln,
		}
		proxies = append(proxies, proxy)
		go proxy.serve()

		_, proxyPort, _ := net.SplitHostPort(ln.Addr().String())
		log.Printf("[exlog] proxy active: %s -> 127.0.0.1:%s -> %s", r, proxyPort, proxy.target)

		rewritten = append(rewritten, fmt.Sprintf("R:%s:127.0.0.1:%s", spec.localPort, proxyPort))
	}

	return rewritten, proxies, nil
}

// stopExlogProxies closes all proxy listeners. Safe to call multiple times.
func stopExlogProxies(proxies []*exlogProxy) {
	for _, p := range proxies {
		if p != nil && p.listener != nil {
			_ = p.listener.Close()
		}
	}
}

func (p *exlogProxy) serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			// Listener closed or temporary error. If it's permanent (closed
			// listener) the loop exits naturally. If it were transient we'd
			// see repeated errors -- log them quietly.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("[exlog] accept error on %s: %v", p.routeRule, err)
			return
		}
		go p.handle(conn)
	}
}

// handle owns the lifecycle of a single proxied connection: emit an "open"
// exlog entry on accept, dial the target, pipe data both directions, and
// emit a matching "close" exlog entry on teardown. Both entries share the
// same ConnID.
func (p *exlogProxy) handle(client net.Conn) {
	connID := nextConnID.Add(1)
	sourceAddr := client.RemoteAddr().String()
	start := time.Now()

	// Open event: emitted immediately so an operator can confirm traffic
	// arrived for this route without waiting for the connection to close.
	exlogEmitter(exlogEntry{
		Timestamp:   start.UTC().Format(time.RFC3339Nano),
		ConnID:      connID,
		Event:       "open",
		RouteRule:   p.routeRule,
		Destination: p.target,
		SourceAddr:  sourceAddr,
	})

	// Close event: built up below; emitted exactly once on return.
	closeEntry := exlogEntry{
		ConnID:      connID,
		Event:       "close",
		RouteRule:   p.routeRule,
		Destination: p.target,
		SourceAddr:  sourceAddr,
	}
	defer func() {
		closeEntry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
		closeEntry.DurationMs = time.Since(start).Milliseconds()
		exlogEmitter(closeEntry)
	}()
	defer client.Close()

	dialStart := time.Now()
	target, err := net.DialTimeout("tcp", p.target, exlogDialTimeout)
	closeEntry.LatencyMs = time.Since(dialStart).Milliseconds()
	if err != nil {
		closeEntry.Success = false
		closeEntry.Error = err.Error()
		return
	}
	defer target.Close()
	closeEntry.Success = true

	var bytesIn, bytesOut int64
	var wg sync.WaitGroup
	wg.Add(2)

	// tunnel -> target  (request bytes)
	go func() {
		defer wg.Done()
		n, _ := io.Copy(target, client)
		atomic.StoreInt64(&bytesIn, n)
		// Half-close so the target sees EOF and can flush its response.
		if tc, ok := target.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()

	// target -> tunnel  (response bytes)
	go func() {
		defer wg.Done()
		n, _ := io.Copy(client, target)
		atomic.StoreInt64(&bytesOut, n)
		if tc, ok := client.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()

	wg.Wait()
	closeEntry.BytesIn = atomic.LoadInt64(&bytesIn)
	closeEntry.BytesOut = atomic.LoadInt64(&bytesOut)
}

// remoteSpec is the subset of a chisel remote definition we care about for exlog purposes.
type remoteSpec struct {
	localHost  string // optional, defaults to ""
	localPort  string
	targetHost string
	targetPort string
}

// parseReverseTCPRemote understands the chisel remote forms we record:
//
//	R:<local-port>:<host>:<remote-port>
//	R:<local-host>:<local-port>:<host>:<remote-port>
//
// It deliberately rejects anything ending in /udp (chisel's UDP marker) and
// any non-reverse form (no leading "R:"). When ok is false, callers should
// pass the remote through to chisel unchanged.
func parseReverseTCPRemote(r string) (spec remoteSpec, ok bool) {
	if !strings.HasPrefix(r, "R:") {
		return spec, false
	}
	body := strings.TrimPrefix(r, "R:")

	// Reject explicit UDP markers - we only record TCP.
	if strings.Contains(body, "/udp") {
		return spec, false
	}
	// Strip a /tcp suffix if present, it doesn't change semantics.
	body = strings.TrimSuffix(body, "/tcp")

	parts := strings.Split(body, ":")
	switch len(parts) {
	case 3:
		// <local-port>:<remote-host>:<remote-port>
		spec.localPort = parts[0]
		spec.targetHost = parts[1]
		spec.targetPort = parts[2]
	case 4:
		// <local-host>:<local-port>:<remote-host>:<remote-port>
		spec.localHost = parts[0]
		spec.localPort = parts[1]
		spec.targetHost = parts[2]
		spec.targetPort = parts[3]
	default:
		return spec, false
	}

	// Sanity-check the ports are numeric. chisel will validate more
	// strictly later; we only need enough certainty to safely build
	// our proxy address.
	if !isNumericPort(spec.localPort) || !isNumericPort(spec.targetPort) {
		return spec, false
	}
	if spec.targetHost == "" {
		return spec, false
	}
	return spec, true
}

func isNumericPort(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
