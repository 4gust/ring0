package proc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/4gust/ring0/internal/model"
)

// LogLine is one line of output from a managed app.
type LogLine struct {
	AppID string
	Time  time.Time
	Text  string
	Err   bool
}

// Manager runs and supervises App processes, exposing log streams + status.
type Manager struct {
	mu       sync.RWMutex
	procs    map[string]*managed
	logs     map[string]*RingBuffer
	logCh    chan LogLine
	statusCh chan StatusEvent
}

type StatusEvent struct {
	AppID    string
	Status   model.Status
	PID      int
	ExitCode int
}

type managed struct {
	cmd    *exec.Cmd
	cancel chan struct{}
}

func NewManager() *Manager {
	return &Manager{
		procs:    map[string]*managed{},
		logs:     map[string]*RingBuffer{},
		logCh:    make(chan LogLine, 1024),
		statusCh: make(chan StatusEvent, 64),
	}
}

func (m *Manager) Logs() <-chan LogLine             { return m.logCh }
func (m *Manager) StatusEvents() <-chan StatusEvent { return m.statusCh }

// Buffer returns the ring buffer for an app (creating if needed).
func (m *Manager) Buffer(appID string) *RingBuffer {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.logs[appID]; ok {
		return b
	}
	b := NewRingBuffer(2000)
	m.logs[appID] = b
	return b
}

// Start launches the app via /bin/sh -c (cross-platform-ish).
func (m *Manager) Start(a *model.App) error {
	m.mu.Lock()
	if _, ok := m.procs[a.ID]; ok {
		m.mu.Unlock()
		return fmt.Errorf("already running")
	}
	m.mu.Unlock()

	shell, flag := "/bin/sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/C"
	}
	cmd := exec.Command(shell, flag, a.Cmd)
	if a.Cwd != "" {
		cmd.Dir = a.Cwd
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	mp := &managed{cmd: cmd, cancel: make(chan struct{})}
	m.mu.Lock()
	m.procs[a.ID] = mp
	m.mu.Unlock()

	a.PID = cmd.Process.Pid
	a.StartedAt = time.Now()
	a.Status = model.StatusRunning
	m.emitStatus(a.ID, model.StatusRunning, a.PID, 0)

	go m.pump(a.ID, stdout, false)
	go m.pump(a.ID, stderr, true)

	go func() {
		err := cmd.Wait()
		exit := 0
		status := model.StatusStopped
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				exit = ee.ExitCode()
			} else {
				exit = -1
			}
			status = model.StatusCrashed
		}
		m.mu.Lock()
		delete(m.procs, a.ID)
		m.mu.Unlock()
		a.Status = status
		a.ExitCode = exit
		a.PID = 0
		m.emitStatus(a.ID, status, 0, exit)
	}()
	return nil
}

func (m *Manager) Stop(a *model.App) error {
	m.mu.Lock()
	mp, ok := m.procs[a.ID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("not running")
	}
	if mp.cmd.Process != nil {
		_ = mp.cmd.Process.Signal(syscall.SIGTERM)
		go func(p *exec.Cmd) {
			time.Sleep(3 * time.Second)
			if p.ProcessState == nil || !p.ProcessState.Exited() {
				_ = p.Process.Kill()
			}
		}(mp.cmd)
	}
	return nil
}

func (m *Manager) Restart(a *model.App) error {
	if a.Status == model.StatusRunning {
		_ = m.Stop(a)
		// wait briefly for status flip
		for i := 0; i < 30 && a.Status == model.StatusRunning; i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return m.Start(a)
}

func (m *Manager) Running(appID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.procs[appID]
	return ok
}

func (m *Manager) pump(appID string, r io.Reader, isErr bool) {
	buf := m.Buffer(appID)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := LogLine{AppID: appID, Time: time.Now(), Text: scanner.Text(), Err: isErr}
		buf.Push(line)
		select {
		case m.logCh <- line:
		default:
		}
	}
}

func (m *Manager) emitStatus(id string, st model.Status, pid, exit int) {
	select {
	case m.statusCh <- StatusEvent{AppID: id, Status: st, PID: pid, ExitCode: exit}:
	default:
	}
}

// pushLog injects a synthetic line into the app's log buffer.
func (m *Manager) pushLog(appID, text string, isErr bool) {
	line := LogLine{AppID: appID, Time: time.Now(), Text: text, Err: isErr}
	m.Buffer(appID).Push(line)
	select {
	case m.logCh <- line:
	default:
	}
}

// RunOneShot runs a shell command synchronously, streaming its output into
// the app's log buffer. It does NOT touch the long-running process slot, so
// it's safe to call alongside Start/Stop.
func (m *Manager) RunOneShot(appID, cwd, cmdline string) error {
	shell, flag := "/bin/sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/C"
	}
	m.pushLog(appID, "$ "+cmdline, false)
	cmd := exec.Command(shell, flag, cmdline)
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); m.pump(appID, stdout, false) }()
	go func() { defer wg.Done(); m.pump(appID, stderr, true) }()
	wg.Wait()
	return cmd.Wait()
}

// UpdateAndStart runs the full pipeline asynchronously: git update → setup → start.
// Each step streams into the app's log buffer. Errors are pushed as log lines and
// abort the pipeline.
func (m *Manager) UpdateAndStart(a *model.App) {
	go func() {
		if m.Running(a.ID) {
			m.pushLog(a.ID, "stopping current process…", false)
			_ = m.Stop(a)
			for i := 0; i < 50 && m.Running(a.ID); i++ {
				time.Sleep(100 * time.Millisecond)
			}
		}
		// 1. fetch latest code
		if a.Repo != "" || isGitDir(a.Cwd) {
			if err := m.gitUpdate(a); err != nil {
				m.pushLog(a.ID, "✖ git: "+err.Error(), true)
				return
			}
		}
		// 2. setup
		if a.Setup != "" {
			if err := m.RunOneShot(a.ID, a.Cwd, a.Setup); err != nil {
				m.pushLog(a.ID, "✖ setup failed: "+err.Error(), true)
				return
			}
		}
		// 3. start
		if err := m.Start(a); err != nil {
			m.pushLog(a.ID, "✖ start failed: "+err.Error(), true)
		}
	}()
}

// RunSetup runs only the setup command (e.g. install deps), asynchronously.
func (m *Manager) RunSetup(a *model.App) {
	if a.Setup == "" {
		m.pushLog(a.ID, "no setup command configured", true)
		return
	}
	go func() {
		if err := m.RunOneShot(a.ID, a.Cwd, a.Setup); err != nil {
			m.pushLog(a.ID, "✖ setup failed: "+err.Error(), true)
		} else {
			m.pushLog(a.ID, "✔ setup complete", false)
		}
	}()
}

func (m *Manager) gitUpdate(a *model.App) error {
	cwd := a.Cwd
	if cwd == "" {
		return fmt.Errorf("no cwd set")
	}
	branch := a.Branch
	if branch == "" {
		branch = "main"
	}
	if !isGitDir(cwd) {
		if a.Repo == "" {
			return fmt.Errorf("cwd is not a git repo and no repo URL set")
		}
		return m.RunOneShot(a.ID, "", fmt.Sprintf("git clone --branch %s %s %s", branch, a.Repo, cwd))
	}
	return m.RunOneShot(a.ID, cwd, "git pull --ff-only origin "+branch)
}

func isGitDir(path string) bool {
	if path == "" {
		return false
	}
	if fi, err := os.Stat(path + "/.git"); err == nil && fi != nil {
		return true
	}
	return false
}
