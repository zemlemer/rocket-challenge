package testdb

import (
	"io"
	"net"
	"net/url"
	"sync"
	"testing"
)

// Proxy is a TCP proxy in front of the test database that can be stopped and
// started again on the same address, to simulate a database outage.
type Proxy struct {
	t        testing.TB
	addr     string
	upstream string

	mu    sync.Mutex
	l     net.Listener
	conns map[net.Conn]struct{}
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

// Stop closes the listener and every proxied connection.
func (p *Proxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
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
			go func() { _, _ = io.Copy(up, c); _ = up.Close() }()
			go func() { _, _ = io.Copy(c, up); _ = c.Close() }()
		}
	}()
}
