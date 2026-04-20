package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/4gust/ring0/internal/model"
	"github.com/4gust/ring0/internal/proc"
	"github.com/4gust/ring0/internal/store"
	"github.com/4gust/ring0/internal/sysmon"
)

// Panel identifiers (tab order).
type Panel int

const (
	PanelApps Panel = iota
	PanelRoutes
	PanelSystem
	PanelLogs
)

func (p Panel) Title() string {
	switch p {
	case PanelApps:
		return "Applications"
	case PanelRoutes:
		return "Routes"
	case PanelSystem:
		return "System Monitor"
	case PanelLogs:
		return "Logs"
	}
	return ""
}

// Mode controls how key input is interpreted.
type Mode int

const (
	ModeNormal Mode = iota
	ModeSearch
	ModeForm // adding/editing
	ModeConfirm
)

// toast severities
const (
	toastInfo = iota
	toastOK
	toastWarn
	toastErr
)

type toast struct {
	text  string
	kind  int
	until time.Time
}

// formKind identifies which entity the modal form is editing.
type formKind int

const (
	formNone formKind = iota
	formAddApp
	formAddRoute
	formEditRoute
)

// Model is the root Bubble Tea model.
type Model struct {
	store *store.Store
	pm    *proc.Manager

	w, h   int
	active Panel
	mode   Mode
	toast  toast

	// per-panel cursor
	appCursor   int
	routeCursor int
	logFollow   bool
	logScroll   int // lines from bottom when not following

	// search
	search       textinput.Model
	searchActive bool

	// form state
	form     formKind
	editing  *model.Route // when editing
	inputs   []textinput.Model
	inputIdx int

	// confirm
	confirmText string
	confirmFn   func() tea.Cmd

	// system + logs
	sys     sysmon.Snapshot
	cpuHist []float64
	memHist []float64

	// animation
	frame int
}

// ----- messages -----

type tickMsg time.Time
type animMsg struct{}
type sysMsg sysmon.Snapshot
type logMsg proc.LogLine
type statusMsg proc.StatusEvent
type toastClearMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func animCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return animMsg{} })
}

func sysCmd() tea.Cmd {
	return func() tea.Msg { return sysMsg(sysmon.Sample()) }
}

func waitLog(ch <-chan proc.LogLine) tea.Cmd {
	return func() tea.Msg { return logMsg(<-ch) }
}
func waitStatus(ch <-chan proc.StatusEvent) tea.Cmd {
	return func() tea.Msg { return statusMsg(<-ch) }
}

// New constructs the root model.
func New(s *store.Store, pm *proc.Manager) Model {
	si := textinput.New()
	si.Prompt = "/ "
	si.Placeholder = "search…"
	si.CharLimit = 64
	return Model{
		store:     s,
		pm:        pm,
		active:    PanelApps,
		logFollow: true,
		search:    si,
		cpuHist:   make([]float64, 0, 60),
		memHist:   make([]float64, 0, 60),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), animCmd(), sysCmd(), waitLog(m.pm.Logs()), waitStatus(m.pm.StatusEvents()))
}

// ---------------- Update ----------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		// expire toast
		if !m.toast.until.IsZero() && time.Now().After(m.toast.until) {
			m.toast = toast{}
		}
		return m, tea.Batch(tickCmd(), sysCmd())

	case animMsg:
		m.frame++
		return m, animCmd()

	case sysMsg:
		m.sys = sysmon.Snapshot(msg)
		m.cpuHist = appendCapped(m.cpuHist, msg.CPUPercent, 60)
		m.memHist = appendCapped(m.memHist, msg.MemPercent, 60)
		return m, nil

	case logMsg:
		// no-op storage (manager owns ring buffer); just re-arm channel
		return m, waitLog(m.pm.Logs())

	case statusMsg:
		// reflect into store apps; re-arm channel
		if a := m.store.FindApp(msg.AppID); a != nil {
			a.Status = msg.Status
			a.PID = msg.PID
			a.ExitCode = msg.ExitCode
			if msg.Status == model.StatusCrashed {
				m.flash(toastErr, fmt.Sprintf("✖ %s crashed (exit %d)", a.Name, msg.ExitCode))
			} else if msg.Status == model.StatusStopped {
				m.flash(toastInfo, fmt.Sprintf("■ %s stopped", a.Name))
			} else if msg.Status == model.StatusRunning {
				m.flash(toastOK, fmt.Sprintf("● %s running (pid %d)", a.Name, msg.PID))
			}
		}
		return m, waitStatus(m.pm.StatusEvents())

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) flash(kind int, text string) {
	m.toast = toast{text: text, kind: kind, until: time.Now().Add(4 * time.Second)}
}

func appendCapped(s []float64, v float64, max int) []float64 {
	s = append(s, v)
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}

// ---------------- Key handling ----------------

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Modes that fully capture input first.
	switch m.mode {
	case ModeSearch:
		return m.keySearch(k)
	case ModeForm:
		return m.keyForm(k)
	case ModeConfirm:
		return m.keyConfirm(k)
	}

	switch k.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab":
		m.active = (m.active + 1) % 4
		return m, nil
	case "shift+tab":
		m.active = (m.active + 3) % 4
		return m, nil
	case "/":
		m.mode = ModeSearch
		m.search.SetValue("")
		m.search.Focus()
		return m, nil
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "pgup":
		m.moveCursor(-10)
		return m, nil
	case "pgdown":
		m.moveCursor(10)
		return m, nil
	case "p":
		m.store.Pet = NextPet(m.store.Pet)
		_ = m.store.Save()
		m.flash(toastInfo, "buddy: "+PetByID(m.store.Pet).Name)
		return m, nil
	}

	// Context-aware actions per panel.
	switch m.active {
	case PanelApps:
		return m.keyApps(k)
	case PanelRoutes:
		return m.keyRoutes(k)
	case PanelLogs:
		return m.keyLogs(k)
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	switch m.active {
	case PanelApps:
		apps := m.filteredApps()
		if len(apps) == 0 {
			m.appCursor = 0
			return
		}
		m.appCursor = clamp(m.appCursor+delta, 0, len(apps)-1)
	case PanelRoutes:
		rs := m.filteredRoutes()
		if len(rs) == 0 {
			m.routeCursor = 0
			return
		}
		m.routeCursor = clamp(m.routeCursor+delta, 0, len(rs)-1)
	case PanelLogs:
		// scrolling up disables follow mode
		if delta < 0 {
			m.logFollow = false
		}
		m.logScroll = clamp(m.logScroll-delta, 0, 100000)
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m Model) keySearch(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = ModeNormal
		m.search.SetValue("")
		m.search.Blur()
		return m, nil
	case "enter":
		m.mode = ModeNormal
		m.search.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(k)
	return m, cmd
}

func (m Model) keyApps(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "a":
		m.openAppForm()
		return m, nil
	case "s":
		if a := m.currentApp(); a != nil {
			m.pm.UpdateAndStart(a)
			m.active = PanelLogs
			m.logFollow = true
			m.logScroll = 0
			m.flash(toastInfo, "▶ starting "+a.Name+"…")
		}
		return m, nil
	case "x":
		if a := m.currentApp(); a != nil {
			if err := m.pm.Stop(a); err != nil {
				m.flash(toastWarn, "⚠ "+err.Error())
			}
		}
		return m, nil
	case "r":
		if a := m.currentApp(); a != nil {
			m.pm.UpdateAndStart(a)
			m.active = PanelLogs
			m.logFollow = true
			m.logScroll = 0
			m.flash(toastInfo, "↻ restarting "+a.Name+"…")
		}
		return m, nil
	case "l":
		m.active = PanelLogs
		m.logFollow = true
		m.logScroll = 0
		return m, nil
	case "d":
		if a := m.currentApp(); a != nil {
			name := a.Name
			id := a.ID
			m.confirm(fmt.Sprintf("Delete app %q? (y/N)", name), func() tea.Cmd {
				if m.pm.Running(id) {
					m.pm.Stop(a)
				}
				m.store.RemoveApp(id)
				_ = m.store.Save()
				m.flash(toastOK, "✔ deleted "+name)
				return nil
			})
		}
		return m, nil
	}
	return m, nil
}

func (m Model) keyRoutes(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "a":
		m.openRouteForm(nil)
		return m, nil
	case "e":
		if r := m.currentRoute(); r != nil {
			m.openRouteForm(r)
		}
		return m, nil
	case "d":
		if r := m.currentRoute(); r != nil {
			id := r.ID
			path := r.Path
			m.confirm(fmt.Sprintf("Delete route %q? (y/N)", path), func() tea.Cmd {
				m.store.RemoveRoute(id)
				_ = m.store.Save()
				m.flash(toastOK, "✔ deleted "+path)
				return nil
			})
		}
		return m, nil
	}
	return m, nil
}

func (m Model) keyLogs(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "f":
		m.logFollow = !m.logFollow
		if m.logFollow {
			m.logScroll = 0
		}
		s := "OFF"
		if m.logFollow {
			s = "ON"
		}
		m.flash(toastInfo, "follow: "+s)
		return m, nil
	case "g":
		m.logScroll = 100000
		m.logFollow = false
		return m, nil
	case "G":
		m.logScroll = 0
		m.logFollow = true
		return m, nil
	}
	return m, nil
}

// ---- confirm modal ----

func (m *Model) confirm(text string, fn func() tea.Cmd) {
	m.mode = ModeConfirm
	m.confirmText = text
	m.confirmFn = fn
}

func (m Model) keyConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(k.String()) {
	case "y":
		fn := m.confirmFn
		m.mode = ModeNormal
		m.confirmFn = nil
		if fn != nil {
			return m, fn()
		}
	case "n", "esc":
		m.mode = ModeNormal
		m.confirmFn = nil
	}
	return m, nil
}

// ---- form modal ----

func newInput(label, placeholder string) textinput.Model {
	t := textinput.New()
	t.Prompt = label + ": "
	t.Placeholder = placeholder
	t.CharLimit = 256
	t.Width = 40
	return t
}

func (m *Model) openAppForm() {
	m.mode = ModeForm
	m.form = formAddApp
	m.inputIdx = 0
	m.inputs = []textinput.Model{
		newInput("Name ", "my-api"),
		newInput("Cmd  ", "node server.js"),
		newInput("Cwd  ", "/path/to/dir (optional)"),
		newInput("Setup", "npm install (optional)"),
		newInput("Repo ", "git@github.com:org/app.git (optional)"),
		newInput("Port ", "3000 (optional)"),
	}
	m.inputs[0].Focus()
}

func (m *Model) openRouteForm(r *model.Route) {
	m.mode = ModeForm
	if r == nil {
		m.form = formAddRoute
		m.editing = nil
	} else {
		m.form = formEditRoute
		copy := *r
		m.editing = &copy
	}
	m.inputIdx = 0
	m.inputs = []textinput.Model{
		newInput("Path  ", "/api"),
		newInput("Host  ", "(optional)"),
		newInput("Port  ", "3000"),
		newInput("Vis   ", "public|private"),
	}
	if r != nil {
		m.inputs[0].SetValue(r.Path)
		m.inputs[1].SetValue(r.Host)
		m.inputs[2].SetValue(fmt.Sprintf("%d", r.TargetPort))
		m.inputs[3].SetValue(string(r.Visibility))
	}
	m.inputs[0].Focus()
}

func (m Model) keyForm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = ModeNormal
		m.form = formNone
		m.inputs = nil
		return m, nil
	case "tab", "down":
		m.inputs[m.inputIdx].Blur()
		m.inputIdx = (m.inputIdx + 1) % len(m.inputs)
		m.inputs[m.inputIdx].Focus()
		return m, nil
	case "shift+tab", "up":
		m.inputs[m.inputIdx].Blur()
		m.inputIdx = (m.inputIdx - 1 + len(m.inputs)) % len(m.inputs)
		m.inputs[m.inputIdx].Focus()
		return m, nil
	case "enter":
		// On last field → submit; otherwise advance.
		if m.inputIdx < len(m.inputs)-1 {
			m.inputs[m.inputIdx].Blur()
			m.inputIdx++
			m.inputs[m.inputIdx].Focus()
			return m, nil
		}
		return m.submitForm()
	}
	var cmd tea.Cmd
	m.inputs[m.inputIdx], cmd = m.inputs[m.inputIdx].Update(k)
	return m, cmd
}

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	switch m.form {
	case formAddApp:
		port := 0
		fmt.Sscanf(strings.TrimSpace(m.inputs[5].Value()), "%d", &port)
		a := &model.App{
			Name:  strings.TrimSpace(m.inputs[0].Value()),
			Cmd:   strings.TrimSpace(m.inputs[1].Value()),
			Cwd:   strings.TrimSpace(m.inputs[2].Value()),
			Setup: strings.TrimSpace(m.inputs[3].Value()),
			Repo:  strings.TrimSpace(m.inputs[4].Value()),
			Port:  port,
		}
		if err := m.store.AddApp(a); err != nil {
			m.flash(toastErr, "✖ "+err.Error())
			return m, nil
		}
		_ = m.store.Save()
		m.flash(toastOK, "✔ added "+a.Name)
	case formAddRoute, formEditRoute:
		port := 0
		fmt.Sscanf(strings.TrimSpace(m.inputs[2].Value()), "%d", &port)
		vis := model.Visibility(strings.ToLower(strings.TrimSpace(m.inputs[3].Value())))
		if vis != model.Public && vis != model.Private {
			vis = model.Private
		}
		r := &model.Route{
			Path:       strings.TrimSpace(m.inputs[0].Value()),
			Host:       strings.TrimSpace(m.inputs[1].Value()),
			TargetPort: port,
			Visibility: vis,
		}
		if m.form == formEditRoute && m.editing != nil {
			r.ID = m.editing.ID
			if err := m.store.UpdateRoute(r); err != nil {
				m.flash(toastErr, "✖ "+err.Error())
				return m, nil
			}
		} else {
			if err := m.store.AddRoute(r); err != nil {
				m.flash(toastErr, "✖ "+err.Error())
				return m, nil
			}
		}
		_ = m.store.Save()
		m.flash(toastOK, "✔ saved route "+r.Path)
	}
	m.mode = ModeNormal
	m.form = formNone
	m.inputs = nil
	return m, nil
}

// ---- selection helpers ----

func (m Model) currentApp() *model.App {
	apps := m.filteredApps()
	if m.appCursor >= len(apps) {
		return nil
	}
	return apps[m.appCursor]
}

func (m Model) currentRoute() *model.Route {
	rs := m.filteredRoutes()
	if m.routeCursor >= len(rs) {
		return nil
	}
	return rs[m.routeCursor]
}

func (m Model) filteredApps() []*model.App {
	q := strings.ToLower(strings.TrimSpace(m.search.Value()))
	all := m.store.ListApps()
	if q == "" {
		return all
	}
	out := all[:0:0]
	for _, a := range all {
		if strings.Contains(strings.ToLower(a.Name), q) || strings.Contains(strings.ToLower(a.Cmd), q) {
			out = append(out, a)
		}
	}
	return out
}

func (m Model) filteredRoutes() []*model.Route {
	q := strings.ToLower(strings.TrimSpace(m.search.Value()))
	all := m.store.ListRoutes()
	if q == "" {
		return all
	}
	out := all[:0:0]
	for _, r := range all {
		if strings.Contains(strings.ToLower(r.Path), q) || strings.Contains(strings.ToLower(r.Host), q) {
			out = append(out, r)
		}
	}
	return out
}

// ---------------- View ----------------

func (m Model) View() string {
	if m.w == 0 || m.h == 0 {
		return "initializing…"
	}

	// Outer margin: 1 row top/bottom, 2 cols left/right.
	const marginX, marginY = 2, 1
	ew := m.w - marginX*2
	eh := m.h - marginY*2
	if ew < 20 || eh < 10 {
		// Terminal too small — render plain message.
		return "ring0: terminal too small"
	}

	header := m.renderHeader(ew)
	footer := m.renderFooter(ew)
	cat := m.renderCat(ew)

	bodyH := eh - lipgloss.Height(header) - lipgloss.Height(footer) - lipgloss.Height(cat)
	if bodyH < 6 {
		bodyH = 6
	}

	leftW := ew / 2
	rightW := ew - leftW
	colH := bodyH / 2
	colHB := bodyH - colH

	apps := m.renderPanel(PanelApps, leftW, colH, m.viewApps(leftW-4, colH-2))
	routes := m.renderPanel(PanelRoutes, leftW, colHB, m.viewRoutes(leftW-4, colHB-2))
	sysv := m.renderPanel(PanelSystem, rightW, colH, m.viewSystem(rightW-4, colH-2))
	logs := m.renderPanel(PanelLogs, rightW, colHB, m.viewLogs(rightW-4, colHB-2))

	left := lipgloss.JoinVertical(lipgloss.Left, apps, routes)
	right := lipgloss.JoinVertical(lipgloss.Left, sysv, logs)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	out := lipgloss.JoinVertical(lipgloss.Left, cat, header, body, footer)

	if m.mode == ModeForm {
		out = m.overlay(out, m.viewForm())
	} else if m.mode == ModeConfirm {
		out = m.overlay(out, m.viewConfirm())
	}

	// Apply outer margin.
	return lipgloss.NewStyle().Margin(marginY, marginX).Render(out)
}

// renderCat draws the selected Tamagotchi-style pet that reacts to app state.
func (m Model) renderCat(width int) string {
	pet := PetByID(m.store.Pet)
	mood, label, labelColor := m.petMood()

	var frame [3]string
	switch mood {
	case "alert":
		frame = pet.Alert
	case "sleep":
		// tiny "breathing" variation: every other tick swap rows 0 and 2 trailing spaces
		frame = pet.Sleep
		if m.frame%2 == 1 {
			frame[1] = swapTrailing(frame[1])
		}
	default:
		frame = pet.Happy
		// Blink every 5 frames for cats/foxes/etc (only affects eye row).
		if m.frame%5 == 0 {
			frame[1] = blink(frame[1])
		}
	}

	// Stats line (hearts from running apps, skulls from crashed).
	apps := m.store.ListApps()
	running := 0
	crashed := 0
	for _, a := range apps {
		if a.Status == model.StatusRunning {
			running++
		}
		if a.Status == model.StatusCrashed {
			crashed++
		}
	}
	var hearts string
	if running == 0 {
		hearts = lipgloss.NewStyle().Foreground(ColorDim).Render("· · ·")
	} else {
		hearts = lipgloss.NewStyle().Foreground(ColorRed).Render(strings.TrimSpace(strings.Repeat("♥ ", running)))
	}
	skulls := ""
	if crashed > 0 {
		skulls = " " + lipgloss.NewStyle().Foreground(ColorRed).Render(strings.TrimSpace(strings.Repeat("☠ ", crashed)))
	}

	name := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render(pet.Name)
	moodTxt := lipgloss.NewStyle().Foreground(labelColor).Render(label)
	hint := lipgloss.NewStyle().Foreground(ColorDim).Render("[p] next buddy")
	stats := fmt.Sprintf("%s  %s  %s%s   %s", name, moodTxt, hearts, skulls, hint)

	catStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	catBlock := lipgloss.JoinVertical(lipgloss.Left,
		catStyle.Render(frame[0]),
		catStyle.Render(frame[1]),
		catStyle.Render(frame[2]),
	)
	statsBlock := lipgloss.NewStyle().PaddingLeft(2).Render("\n" + stats + "\n")

	row := lipgloss.JoinHorizontal(lipgloss.Top, catBlock, statsBlock)
	rowW := lipgloss.Width(row)
	if rowW >= width {
		return row
	}
	return row + strings.Repeat(" ", width-rowW)
}

// blink replaces letter-like eye chars with closed-eye equivalents.
func blink(s string) string {
	r := strings.NewReplacer(
		"o", "-", "O", "-", "^", "-", "@", "-",
		".", "_",
	)
	return r.Replace(s)
}

// swapTrailing toggles trailing spaces/z for a subtle breathing effect.
func swapTrailing(s string) string {
	if strings.HasSuffix(s, " Z") {
		return strings.TrimSuffix(s, " Z") + "z "
	}
	if strings.HasSuffix(s, "Z ") {
		return strings.TrimSuffix(s, "Z ") + " Z"
	}
	return s
}

// petMood derives the pet's state from app statuses.
func (m Model) petMood() (mood, label string, color lipgloss.Color) {
	apps := m.store.ListApps()
	anyRunning := false
	anyCrashed := false
	for _, a := range apps {
		if a.Status == model.StatusRunning {
			anyRunning = true
		}
		if a.Status == model.StatusCrashed {
			anyCrashed = true
		}
	}
	switch {
	case anyCrashed:
		return "alert", "yikes!", ColorRed
	case anyRunning:
		return "happy", "purring", ColorGreen
	default:
		return "sleep", "napping", ColorGray
	}
}

func (m Model) renderHeader(width int) string {
	title := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render(" ring0 ")
	tabs := []string{}
	for i := PanelApps; i <= PanelLogs; i++ {
		s := StyleTitleInactive
		if i == m.active {
			s = StyleTitle
		}
		tabs = append(tabs, s.Render(fmt.Sprintf("[%d] %s", int(i)+1, i.Title())))
	}
	right := lipgloss.NewStyle().Foreground(ColorGray).Render(time.Now().Format("15:04:05"))
	bar := lipgloss.JoinHorizontal(lipgloss.Top, title, strings.Join(tabs, " "))
	pad := width - lipgloss.Width(bar) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return bar + strings.Repeat(" ", pad) + right
}

func (m Model) renderFooter(width int) string {
	keys := m.contextKeys()
	help := lipgloss.NewStyle().Foreground(ColorGray).Render(keys)

	var t string
	if m.mode == ModeSearch {
		t = m.search.View()
	} else if m.toast.text != "" {
		switch m.toast.kind {
		case toastErr:
			t = StyleToastErr.Render(m.toast.text)
		case toastWarn:
			t = StyleToastWarn.Render(m.toast.text)
		case toastOK:
			t = StyleToastOK.Render(m.toast.text)
		default:
			t = StyleToastInfo.Render(m.toast.text)
		}
	}
	if t == "" {
		t = StyleStatusBar.Render(" ready ")
	}
	pad := width - lipgloss.Width(t) - lipgloss.Width(help) - 2
	if pad < 1 {
		pad = 1
	}
	return t + strings.Repeat(" ", pad) + help + " "
}

func (m Model) contextKeys() string {
	common := "tab:panel  /:search  q:quit"
	switch m.active {
	case PanelApps:
		return "a:add  s:start  x:stop  r:restart  l:logs  d:del  " + common
	case PanelRoutes:
		return "a:add  e:edit  d:del  " + common
	case PanelLogs:
		return "f:follow  g/G:top/bot  " + common
	}
	return common
}

func (m Model) renderPanel(p Panel, w, h int, body string) string {
	style := StylePanel
	titleColor := ColorGray
	if p == m.active {
		style = StylePanelActive
		titleColor = ColorBlue
	}
	title := lipgloss.NewStyle().Foreground(titleColor).Bold(true).Render(" " + p.Title() + " ")
	inner := lipgloss.NewStyle().Width(w - 2).Height(h - 2).Render(body)
	return style.Width(w).Height(h).Render(lipgloss.JoinVertical(lipgloss.Left, title, inner))
}

// --- panel bodies ---

func (m Model) viewApps(w, h int) string {
	apps := m.filteredApps()
	if len(apps) == 0 {
		return StyleDim.Render("No apps. Press 'a' to add one.")
	}
	nameW := w - 28
	if nameW < 10 {
		nameW = 10
	}
	header := lipgloss.NewStyle().Foreground(ColorGray).Render(
		fmt.Sprintf("  %s  %s  %s  %s",
			PadRight("NAME", nameW),
			PadRight("STATUS", 9),
			PadRight("PID", 7),
			PadRight("PORT", 5),
		))
	rows := []string{header}
	for i, a := range apps {
		dot, statusTxt := statusDot(a.Status)
		row := fmt.Sprintf("  %s  %s %s  %s  %s",
			PadRight(Truncate(a.Name, nameW), nameW),
			dot, PadRight(statusTxt, 7),
			PadRight(pidStr(a), 7),
			PadRight(portStr(a.Port), 5),
		)
		if i == m.appCursor && m.active == PanelApps {
			row = StyleSelected.Render(PadRight(row, w))
		}
		rows = append(rows, row)
		if len(rows) >= h {
			break
		}
	}
	return strings.Join(rows, "\n")
}

func (m Model) viewRoutes(w, h int) string {
	rs := m.filteredRoutes()
	if len(rs) == 0 {
		return StyleDim.Render("No routes. Press 'a' to add one.")
	}
	pathW := w - 26
	if pathW < 10 {
		pathW = 10
	}
	header := lipgloss.NewStyle().Foreground(ColorGray).Render(
		fmt.Sprintf("  %s  %s  %s",
			PadRight("PATH", pathW),
			PadRight("PORT", 6),
			PadRight("ACCESS", 9),
		))
	rows := []string{header}
	for i, r := range rs {
		var badge string
		if r.Visibility == model.Public {
			badge = StyleBadgePub.Render("PUBLIC")
		} else {
			badge = StyleBadgePriv.Render("PRIVATE")
		}
		path := r.Path
		if r.Host != "" {
			path = r.Host + r.Path
		}
		row := fmt.Sprintf("  %s  %s  %s",
			PadRight(Truncate(path, pathW), pathW),
			PadRight(fmt.Sprintf(":%d", r.TargetPort), 6),
			badge,
		)
		if i == m.routeCursor && m.active == PanelRoutes {
			row = StyleSelected.Render(PadRight(row, w))
		}
		rows = append(rows, row)
		if len(rows) >= h {
			break
		}
	}
	return strings.Join(rows, "\n")
}

func (m Model) viewSystem(w, h int) string {
	cpu := bar("CPU", m.sys.CPUPercent, w-6)
	mem := bar("MEM", m.sys.MemPercent, w-6)
	memTxt := StyleDim.Render(fmt.Sprintf(" %d / %d MB", m.sys.MemUsedMB, m.sys.MemTotalMB))
	spark := "CPU " + sparkline(m.cpuHist, w-6)
	mspark := "MEM " + sparkline(m.memHist, w-6)
	apps := m.store.ListApps()
	running := 0
	for _, a := range apps {
		if a.Status == model.StatusRunning {
			running++
		}
	}
	summary := StyleDim.Render(fmt.Sprintf("apps: %d running / %d total    routes: %d",
		running, len(apps), len(m.store.ListRoutes())))
	return strings.Join([]string{cpu, mem + memTxt, "", spark, mspark, "", summary}, "\n")
}

func (m Model) viewLogs(w, h int) string {
	a := m.currentApp()
	if a == nil {
		return StyleDim.Render("Select an app in the Applications panel to view logs.")
	}
	lines := m.pm.Buffer(a.ID).Snapshot()
	header := StyleDim.Render(fmt.Sprintf("logs: %s   follow:%s   lines:%d",
		a.Name, onOff(m.logFollow), len(lines)))
	visible := h - 1
	if visible < 1 {
		visible = 1
	}
	end := len(lines) - m.logScroll
	if end > len(lines) {
		end = len(lines)
	}
	if end < 0 {
		end = 0
	}
	start := end - visible
	if start < 0 {
		start = 0
	}
	out := []string{header}
	for _, l := range lines[start:end] {
		ts := StyleDim.Render(l.Time.Format("15:04:05"))
		text := Truncate(l.Text, w-10)
		if l.Err {
			out = append(out, ts+" "+StyleErr.Render(text))
		} else {
			out = append(out, ts+" "+text)
		}
	}
	if len(out) == 1 {
		out = append(out, StyleDim.Render("  (no output yet)"))
	}
	return strings.Join(out, "\n")
}

// --- modal overlay ---

func (m Model) overlay(_ string, modal string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBlue).
		Background(ColorPanel).
		Padding(1, 2).
		Render(modal)
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "))
}

func (m Model) viewForm() string {
	title := "Add Application"
	switch m.form {
	case formAddRoute:
		title = "Add Route"
	case formEditRoute:
		title = "Edit Route"
	}
	rows := []string{StyleTitle.Render(title), ""}
	for _, in := range m.inputs {
		rows = append(rows, in.View())
	}
	rows = append(rows, "", StyleDim.Render("enter: next/save   tab: next   esc: cancel"))
	return strings.Join(rows, "\n")
}

func (m Model) viewConfirm() string {
	return strings.Join([]string{
		StyleWarn.Render("Confirm"),
		"",
		m.confirmText,
		"",
		StyleDim.Render("y: yes    n/esc: cancel"),
	}, "\n")
}

// --- helpers ---

func statusDot(s model.Status) (string, string) {
	switch s {
	case model.StatusRunning:
		return Dot(ColorGreen), "running"
	case model.StatusCrashed:
		return Dot(ColorRed), "crashed"
	default:
		return Dot(ColorGray), "stopped"
	}
}

func pidStr(a *model.App) string {
	if a.PID == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", a.PID)
}

func portStr(p int) string {
	if p == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", p)
}

func onOff(b bool) string {
	if b {
		return StyleOK.Render("ON")
	}
	return StyleDim.Render("OFF")
}

func bar(label string, pct float64, width int) string {
	if width < 10 {
		width = 10
	}
	filled := int(float64(width) * pct / 100.0)
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	color := ColorGreen
	switch {
	case pct >= 85:
		color = ColorRed
	case pct >= 65:
		color = ColorYellow
	}
	fill := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled))
	rest := lipgloss.NewStyle().Foreground(ColorDim).Render(strings.Repeat("░", width-filled))
	return fmt.Sprintf("%s [%s%s] %5.1f%%", label, fill, rest, pct)
}

func sparkline(vals []float64, width int) string {
	if width <= 0 || len(vals) == 0 {
		return ""
	}
	runes := []rune("▁▂▃▄▅▆▇█")
	// resample to width
	start := 0
	if len(vals) > width {
		start = len(vals) - width
	}
	v := vals[start:]
	out := make([]rune, 0, len(v))
	for _, x := range v {
		idx := int(x / 100.0 * float64(len(runes)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(runes) {
			idx = len(runes) - 1
		}
		out = append(out, runes[idx])
	}
	return lipgloss.NewStyle().Foreground(ColorBlue).Render(string(out))
}
