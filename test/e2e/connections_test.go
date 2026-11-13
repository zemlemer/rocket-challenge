//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Spec deployment "Entry point connection reuse".
//
// Counting sockets alone cannot prove reuse here: with Docker Desktop's
// networking and nginx reloads, even a configuration that disables reuse
// leaves only a fraction of the expected TIME_WAIT sockets. So the test checks
// the configuration nginx actually loaded — the three settings that together
// enable upstream keep-alive — and then sanity-checks that a burst of 200
// requests leaves far fewer than 200 upstream sockets.
func TestEntryPointReusesUpstreamConnections(t *testing.T) {
	loaded := stripComments(compose(t, "exec", "-T", "nginx", "nginx", "-T"))
	upstream := block(t, loaded, "upstream api {")
	server := block(t, loaded, "server {") // inherited by every location
	for _, want := range []struct{ where, text string }{
		{upstream, "keepalive 32;"},
		{upstream, "keepalive_timeout 50s;"}, // below the server's 60 s IdleTimeout
		{server, "proxy_http_version 1.1;"},
		{server, `proxy_set_header Connection "";`},
	} {
		if !strings.Contains(want.where, want.text) {
			t.Errorf("loaded nginx config lacks %q in the right block", want.text)
		}
	}
	// A proxy_set_header in a location would replace, not extend, the
	// inherited ones and drop the empty Connection header.
	for _, header := range []string{"location / {", "location /rockets {", "location /stats {"} {
		if strings.Contains(block(t, loaded, header), "proxy_set_header") {
			t.Errorf("%s sets its own proxy headers and loses the server-level Connection \"\"", header)
		}
	}

	before := upstreamConnections(t)["TIME_WAIT"]
	for i := range 200 {
		resp, err := http.Get(baseURL + "/healthz?reuse=" + strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	after := upstreamConnections(t)
	if opened := after["TIME_WAIT"] - before + after["ESTABLISHED"]; opened > 100 {
		t.Fatalf("200 requests left %d upstream sockets (states %v); want far fewer", opened, after)
	}
}

// Spec deployment "Stop grace period": the container runtime waits longer
// than the server's shutdown timeout before killing a stopping server.
func TestStopGracePeriodExceedsShutdownTimeout(t *testing.T) {
	victim := containers(t, "server")[0]
	timeout := strings.TrimSpace(run(t, "docker", "inspect", "-f", "{{.Config.StopTimeout}}", victim))
	grace, err := strconv.Atoi(timeout)
	if err != nil {
		t.Fatalf("server container has no stop timeout set (got %q); Docker would use 10 s", timeout)
	}
	const shutdownTimeout = 15 * time.Second // server default
	if time.Duration(grace)*time.Second <= shutdownTimeout {
		t.Fatalf("stop grace period %ds does not exceed the %s shutdown timeout", grace, shutdownTimeout)
	}
}

// Spec deployment "Server replicas" and "Processor replicas": servers and
// processors start only after PostgreSQL is healthy and migrate has completed.
func TestServicesStartAfterDatabaseAndMigrate(t *testing.T) {
	var cfg struct {
		Services map[string]struct {
			DependsOn map[string]struct {
				Condition string `json:"condition"`
			} `json:"depends_on"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(compose(t, "config", "--format", "json")), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"server", "processor"} {
		deps := cfg.Services[service].DependsOn
		for svc, want := range map[string]string{"postgres": "service_healthy", "migrate": "service_completed_successfully"} {
			if got := deps[svc].Condition; got != want {
				t.Errorf("%s depends on %s with condition %q, want %q", service, svc, got, want)
			}
		}
	}
}

// Spec deployment "Processor replicas": four processors run, each with a stop
// grace period longer than its 15 s shutdown timeout.
func TestFourProcessorsRunning(t *testing.T) {
	procs := containers(t, "processor")
	if len(procs) != 4 {
		t.Fatalf("%d processor containers running, want 4", len(procs))
	}
	for _, c := range procs {
		grace, err := strconv.Atoi(strings.TrimSpace(run(t, "docker", "inspect", "-f", "{{.Config.StopTimeout}}", c)))
		if err != nil || grace <= 15 {
			t.Errorf("%s: stop grace period %d s, want > 15 s (%v)", c, grace, err)
		}
	}
}
