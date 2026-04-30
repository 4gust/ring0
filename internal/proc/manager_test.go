package proc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/4gust/ring0/internal/model"
)

// Verifies that AutoRestart re-spawns a crashing process.
func TestAutoRestartOnCrash(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "count")

	app := &model.App{
		ID:              "test-autorestart",
		Name:            "test",
		// Append to the marker file then exit non-zero. Restart loop
		// should produce >=2 lines within the test timeout.
		Cmd:             `sh -c 'echo tick >> ` + marker + `; exit 1'`,
		AutoRestart:     true,
		RestartDelaySec: 1,
	}

	m := NewManager()
	if err := m.Start(app); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait up to 5s for at least 2 invocations.
	deadline := time.Now().Add(5 * time.Second)
	var lines int
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(marker); err == nil {
			lines = 0
			for _, b := range data {
				if b == '\n' {
					lines++
				}
			}
			if lines >= 2 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Stop to prevent further restarts polluting other tests.
	if app.Status == model.StatusRunning {
		_ = m.Stop(app)
	}

	if lines < 2 {
		t.Fatalf("expected at least 2 invocations from auto-restart, got %d", lines)
	}
}

// Verifies that env vars are passed to the child process.
func TestAppEnvIsPassed(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "env.out")

	app := &model.App{
		ID:   "test-env",
		Name: "test-env",
		Cmd:  `sh -c 'echo "$FOO=$BAR" > ` + out + `'`,
		Env:  map[string]string{"FOO": "hello", "BAR": "world"},
	}

	m := NewManager()
	if err := m.Start(app); err != nil {
		t.Fatalf("start: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(out); err == nil && len(data) > 0 {
			got := string(data)
			if got != "hello=world\n" {
				t.Fatalf("env not propagated correctly, got %q", got)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("child never wrote env file")
}

// Verifies that stop=user does NOT trigger AutoRestart.
func TestAutoRestartSkipsUserStop(t *testing.T) {
	app := &model.App{
		ID:              "test-no-restart-on-stop",
		Name:            "no-restart",
		Cmd:             `sh -c 'sleep 30'`,
		AutoRestart:     true,
		RestartDelaySec: 1,
	}
	m := NewManager()
	if err := m.Start(app); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := m.Stop(app); err != nil {
		t.Fatalf("stop: %v", err)
	}
	// Wait past restart delay; ensure not running again.
	time.Sleep(2500 * time.Millisecond)
	if m.Running(app.ID) {
		t.Fatalf("user-stopped app was respawned by AutoRestart")
	}
}
