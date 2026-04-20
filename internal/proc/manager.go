package proc

import (
	"bufio"
	"fmt"
	"io"
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
