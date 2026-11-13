//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func repoRoot() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..")
}

func entryPointHealthy() bool {
	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// allReplicasListening reports whether every server container has logged
// "listening" since it was last started.
func allReplicasListening() bool {
	out, err := exec.Command("docker", "compose", "ps", "-q", "server").Output()
	if err != nil || len(strings.Fields(string(out))) == 0 {
		return false
	}
	for _, id := range strings.Fields(string(out)) {
		started, err := exec.Command("docker", "inspect", "-f", "{{.State.StartedAt}}", id).Output()
		if err != nil {
			return false
		}
		logs, err := exec.Command("docker", "logs", "--since", strings.TrimSpace(string(started)), id).CombinedOutput()
		if err != nil || !strings.Contains(string(logs), "msg=listening") {
			return false
		}
	}
	return true
}

// compose runs `docker compose` in the repository root and returns stdout.
func compose(t *testing.T, args ...string) string {
	t.Helper()
	return run(t, "docker", append([]string{"compose"}, args...)...)
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, stderr)
	}
	return string(out)
}

// containers returns the container names of a compose service.
func containers(t *testing.T, service string) []string {
	t.Helper()
	return strings.Fields(compose(t, "ps", "--format", "{{.Name}}", service))
}

// nginxUpstreams returns the upstream address of each nginx access-log line
// written since `since` whose request contains marker.
func nginxUpstreams(t *testing.T, since time.Time, marker string) []string {
	t.Helper()
	logs := compose(t, "logs", "--no-log-prefix", "--since", since.UTC().Format(time.RFC3339), "nginx")
	return parseUpstreams(logs, marker, false)
}

// parseUpstreams extracts the upstream address of each access-log line in logs
// whose request contains marker. With servedOnly, it keeps only lines answered
// 200 by a single upstream (no retry).
func parseUpstreams(logs, marker string, servedOnly bool) []string {
	var ups []string
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		i := strings.Index(line, "upstream=")
		if i < 0 {
			continue
		}
		up := strings.Fields(line[i+len("upstream="):])[0]
		if servedOnly && (!strings.Contains(line, "upstream_status=200 ") || strings.HasSuffix(up, ",")) {
			continue
		}
		ups = append(ups, up)
	}
	return ups
}

// entryPointReachesAllReplicas sends a burst of requests through nginx and
// reports whether they were served by as many distinct upstreams as there are
// server containers. Containers being up is not enough: after a rebuild nginx
// may still hold a DNS answer (valid=5s) without the new replicas, or have
// disabled a replica (fail_timeout=5s) that refused a connection while starting.
func entryPointReachesAllReplicas() bool {
	out, err := exec.Command("docker", "compose", "ps", "-q", "server").Output()
	replicas := len(strings.Fields(string(out)))
	if err != nil || replicas == 0 {
		return false
	}
	since := time.Now().Add(-time.Second)
	marker := "ready=" + strconv.FormatInt(time.Now().UnixNano(), 10)
	for range 10 * replicas {
		resp, err := http.Get(baseURL + "/healthz?" + marker)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
	}
	logs, err := exec.Command("docker", "compose", "logs", "--no-log-prefix",
		"--since", since.UTC().Format(time.RFC3339), "nginx").Output()
	if err != nil {
		return false
	}
	seen := map[string]bool{}
	for _, u := range parseUpstreams(string(logs), marker, true) {
		seen[u] = true
	}
	return len(seen) == replicas
}

func db(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to stack database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return conn
}

func queryInt(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db(t).QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// uniqueChannel returns a channel name no other test run uses.
func uniqueChannel(t *testing.T) string {
	return fmt.Sprintf("e2e-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
}

func message(channel string, n int) string {
	return fmt.Sprintf(`{"metadata":{"channel":%q,"messageNumber":%d,"messageTime":"2022-02-02T19:39:05.86337+01:00","messageType":"RocketSpeedIncreased"},"message":{"by":100}}`, channel, n)
}

func post(t *testing.T, body string) int {
	t.Helper()
	resp, err := http.Post(baseURL+"/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /messages: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// upstreamConnections counts TCP connections in the nginx container whose
// remote port is the servers' 8088, by state (busybox netstat).
func upstreamConnections(t *testing.T) map[string]int {
	t.Helper()
	out := compose(t, "exec", "-T", "nginx", "netstat", "-tn")
	states := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || !strings.HasPrefix(f[0], "tcp") {
			continue
		}
		if strings.HasSuffix(f[4], ":8088") { // foreign address = a server replica
			states[f[5]]++
		}
	}
	return states
}

// stripComments drops "# ..." to the end of each line (nginx has no other
// comment syntax; our config has no "#" inside strings).
func stripComments(config string) string {
	lines := strings.Split(config, "\n")
	for i, l := range lines {
		if j := strings.IndexByte(l, '#'); j >= 0 {
			lines[i] = l[:j]
		}
	}
	return strings.Join(lines, "\n")
}

// block returns the text of the nginx config block starting at header.
func block(t *testing.T, config, header string) string {
	t.Helper()
	i := strings.Index(config, header)
	if i < 0 {
		t.Fatalf("loaded nginx config has no %q block", header)
	}
	depth := 0
	for j := i; j < len(config); j++ {
		switch config[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return config[i : j+1]
			}
		}
	}
	t.Fatalf("unterminated %q block", header)
	return ""
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
