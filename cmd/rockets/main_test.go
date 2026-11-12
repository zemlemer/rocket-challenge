package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binary string

// TestMain builds the rockets executable once; tests run it as a subprocess,
// so exit statuses and output are checked exactly as an operator sees them.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "rockets-bin")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "rockets")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		panic("build rockets: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestHelpListsModes(t *testing.T) {
	r := rockets(t, nil, "--help")
	if r.code != 0 {
		t.Fatalf("exit status = %d, want 0; stderr: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "migrate") {
		t.Fatalf("help does not list migrate:\n%s", r.stdout)
	}
}

func TestModeHelp(t *testing.T) {
	r := rockets(t, nil, "migrate", "--help")
	if r.code != 0 || !strings.Contains(r.stdout, "rockets migrate") {
		t.Fatalf("exit status = %d, stdout:\n%s", r.code, r.stdout)
	}
}

func TestUsageErrorsExitWith2(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown mode": {"launch"},
		"no mode":      {},
		"unknown flag": {"migrate", "--nope"},
		"bad level":    {"--log-level", "loud", "migrate"},
	} {
		t.Run(name, func(t *testing.T) {
			r := rockets(t, nil, args...)
			if r.code != 2 {
				t.Fatalf("exit status = %d, want 2; stderr: %s", r.code, r.stderr)
			}
			if !strings.Contains(r.stderr, "rockets: error:") || !strings.Contains(r.stderr, "Usage:") {
				t.Fatalf("stderr lacks error and usage:\n%s", r.stderr)
			}
		})
	}
}

func TestMissingDatabaseURL(t *testing.T) {
	r := rockets(t, nil, "migrate")
	if r.code != 2 {
		t.Fatalf("exit status = %d, want 2", r.code)
	}
	for _, want := range []string{"DATABASE_URL", "--database-url"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr does not name %s:\n%s", want, r.stderr)
		}
	}
}

func TestFlagOverridesEnvironment(t *testing.T) {
	r := rockets(t,
		[]string{"DATABASE_URL=postgres://u:p@env-host.invalid/db", "DB_CONNECT_TIMEOUT=300ms"},
		"migrate", "--database-url", "postgres://u:p@flag-host.invalid/db")
	if !strings.Contains(r.stderr, "flag-host.invalid") || strings.Contains(r.stderr, "env-host.invalid") {
		t.Fatalf("expected only the flag's host to be used:\n%s", r.stderr)
	}
}

func TestUnreachableDatabaseExitsWith1(t *testing.T) {
	r := rockets(t,
		[]string{"DATABASE_URL=postgres://u:p@127.0.0.1:1/db", "DB_CONNECT_TIMEOUT=1s"},
		"migrate")
	if r.code != 1 {
		t.Fatalf("exit status = %d, want 1; stderr: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "could not be reached within 1s") {
		t.Fatalf("stderr lacks a clear message:\n%s", r.stderr)
	}
}
