package testdb

import (
	"bytes"
	"net"
	"net/url"
	"sync"
	"testing"
)

// Proxy is a TCP proxy in front of the test database that can be stopped and
// started again on the same address, to simulate a database outage. It can
// also freeze: stop forwarding without closing anything, to simulate a hung
// network.
type Proxy struct {
	t        testing.TB
	addr     string
	upstream string

	mu       sync.Mutex
	l        net.Listener
	conns    map[net.Conn]struct{}
	freezeOn []byte        // pattern in client traffic that triggers a freeze
	frozen   chan struct{} // closed when the freeze triggers
	thaw     chan struct{} // closed when a triggered freeze ends
}

// NewProxy starts a proxy to the database in dbURL and returns it together
// with dbURL rewritten to go through the proxy.
func NewProxy(t testing.TB, dbURL string) (*Proxy, string) {
	t.Helper()
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{t: t, upstream: u.Host, conns: map[net.Conn]struct{}{}}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p.addr = l.Addr().String()
	p.serve(l)
	t.Cleanup(p.Stop)
	u.Host = p.addr
	return p, u.String()
}

// FreezeOn arms a freeze: as soon as a client sends bytes containing
// pattern, the proxy stops forwarding in both directions on every connection,
// including new ones, without closing any of them; the bytes that contain
// the pattern are not forwarded either. The freeze lasts until Stop. The
// returned channel is closed when the freeze triggers.
func (p *Proxy) FreezeOn(pattern string) <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.freezeOn = []byte(pattern)
	p.frozen = make(chan struct{})
	p.thaw = nil
	return p.frozen
}

// Stop ends a freeze, closes the listener and every proxied connection.
func (p *Proxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.freezeOn = nil
	if p.thaw != nil {
		close(p.thaw)
		p.thaw = nil
	}
	if p.l != nil {
		_ = p.l.Close()
		p.l = nil
	}
	for c := range p.conns {
		_ = c.Close()
	}
	p.conns = map[net.Conn]struct{}{}
}

// Start listens again on the same address.
func (p *Proxy) Start() {
	p.t.Helper()
	l, err := net.Listen("tcp", p.addr)
	if err != nil {
		p.t.Fatalf("proxy restart: %v", err)
	}
	p.serve(l)
}

func (p *Proxy) serve(l net.Listener) {
	p.mu.Lock()
	p.l = l
	p.mu.Unlock()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", p.upstream)
			if err != nil {
				_ = c.Close()
				continue
			}
			p.mu.Lock()
			p.conns[c], p.conns[up] = struct{}{}, struct{}{}
			p.mu.Unlock()
			go p.pipe(up, c, true)
			go p.pipe(c, up, false)
		}
	}()
}

// pipe copies src to dst until either fails, holding every chunk while the
// proxy is frozen. Client traffic (fromClient) is checked for the pattern.
func (p *Proxy) pipe(dst, src net.Conn, fromClient bool) {
	defer func() { _ = dst.Close() }()
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if thaw := p.hold(buf[:n], fromClient); thaw != nil {
				<-thaw
				return // a freeze ends only with Stop, which closes the connections
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// hold reports the channel to wait on if chunk must not be forwarded now:
// the proxy is frozen, or chunk triggers the freeze.
func (p *Proxy) hold(chunk []byte, fromClient bool) <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.thaw == nil && fromClient && p.freezeOn != nil && bytes.Contains(chunk, p.freezeOn) {
		p.thaw = make(chan struct{})
		close(p.frozen)
	}
	return p.thaw
}
