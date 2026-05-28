// Package output renders a live htop-style TUI using Bubbletea.
// Navigasi: services → endpoints (enter) → detail latency (enter) → kembali (esc).
package output

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Raa-11/kerno/internal/aggregator"
	"github.com/Raa-11/kerno/internal/collector"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	borderStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))
	titleStyle  = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	dangerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	filterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	pauseStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
)

// ── Sort ──────────────────────────────────────────────────────────────────────

type sortField int

const (
	sortByCount sortField = iota
	sortByP50
	sortByP99
	sortByErr
)

var sortCycle = []sortField{sortByCount, sortByP50, sortByP99, sortByErr}

var sortName = map[sortField]string{
	sortByCount: "REQ/S",
	sortByP50:   "P50",
	sortByP99:   "P99",
	sortByErr:   "ERR%",
}

// ── View level ────────────────────────────────────────────────────────────────

// viewLevel menentukan halaman mana yang sedang aktif.
type viewLevel int

const (
	levelMain    viewLevel = iota // daftar semua service
	levelService                  // daftar endpoint satu service
	levelDetail                   // bar chart latency satu endpoint
)

// ── Data structures ───────────────────────────────────────────────────────────

// entry adalah satu endpoint dengan stats yang sudah dihitung.
type entry struct {
	key     string
	service string
	method  string
	path    string
	s       *collector.EndpointStats
	rps     uint64
	p50     uint64
	p99     uint64
	errPct  float64
}

// serviceGroup merangkum semua endpoint di bawah satu service.
type serviceGroup struct {
	name      string
	endpoints []entry
	totalRPS  uint64
	maxP99    uint64
	worstErr  float64
}

// ── Model ─────────────────────────────────────────────────────────────────────

type tickMsg time.Time

type model struct {
	getStats      func() map[string]*collector.EndpointStats
	mainTable     table.Model // tabel halaman 1: semua service
	serviceTable  table.Model // tabel halaman 2: endpoint satu service
	level         viewLevel
	groups        []serviceGroup
	selectedGroup serviceGroup // service yang sedang dibuka di halaman 2
	selectedEntry entry        // endpoint yang sedang dibuka di halaman 3
	sortBy        sortField
	filter        string
	filtering     bool
	paused        bool
}

func (m model) Init() tea.Cmd {
	return nextTick()
}

func nextTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if !m.paused {
			m = applyRefresh(m)
		}
		return m, nextTick()

	case tea.KeyMsg:
		if m.filtering {
			return updateFilter(m, msg)
		}
		return updateNormal(m, msg)
	}

	// Teruskan ke tabel yang sedang aktif.
	return updateActiveTable(m, msg)
}

func updateNormal(m model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc":
		switch m.level {
		case levelDetail:
			m.level = levelService
		case levelService:
			m.level = levelMain
		case levelMain:
			if m.filter != "" {
				m.filter = ""
				return applyRefresh(m), nil
			}
		}
		return m, nil

	case "enter":
		switch m.level {
		case levelMain:
			// Masuk ke halaman endpoint service yang dipilih.
			cursor := m.mainTable.Cursor()
			if cursor < len(m.groups) {
				m.selectedGroup = m.groups[cursor]
				m.serviceTable.SetRows(buildServiceRows(m.selectedGroup.endpoints))
				m.level = levelService
			}
		case levelService:
			// Masuk ke halaman detail endpoint yang dipilih.
			cursor := m.serviceTable.Cursor()
			if cursor < len(m.selectedGroup.endpoints) {
				m.selectedEntry = m.selectedGroup.endpoints[cursor]
				m.level = levelDetail
			}
		}
		return m, nil

	case "s":
		// Sort hanya berlaku di halaman 1 dan 2.
		if m.level != levelDetail {
			for i, f := range sortCycle {
				if f == m.sortBy {
					m.sortBy = sortCycle[(i+1)%len(sortCycle)]
					break
				}
			}
			return applyRefresh(m), nil
		}

	case "/":
		// Filter hanya di halaman 1.
		if m.level == levelMain {
			m.filtering = true
			m.filter = ""
			return applyRefresh(m), nil
		}

	case " ":
		m.paused = !m.paused
		return m, nil
	}

	return updateActiveTable(m, msg)
}

func updateFilter(m model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.filtering = false
	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m = applyRefresh(m)
		}
	default:
		if len(msg.Runes) > 0 {
			m.filter += string(msg.Runes)
			m = applyRefresh(m)
		}
	}
	return m, nil
}

// updateActiveTable meneruskan pesan (arrow key, dll.) ke tabel yang aktif.
func updateActiveTable(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.level {
	case levelMain:
		m.mainTable, cmd = m.mainTable.Update(msg)
	case levelService:
		m.serviceTable, cmd = m.serviceTable.Update(msg)
	}
	return m, cmd
}

// applyRefresh mengambil snapshot terbaru dan memperbarui kedua tabel.
func applyRefresh(m model) model {
	m.groups = buildGroups(m.getStats, m.sortBy, m.filter)
	m.mainTable.SetRows(buildMainRows(m.groups))

	// Jika sedang di halaman service, perbarui data service yang dipilih.
	if m.level == levelService {
		for _, g := range m.groups {
			if g.name == m.selectedGroup.name {
				m.selectedGroup = g
				m.serviceTable.SetRows(buildServiceRows(g.endpoints))
				break
			}
		}
	}

	return m
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	switch m.level {
	case levelMain:
		return mainView(m)
	case levelService:
		return serviceView(m)
	case levelDetail:
		return detailView(m.selectedEntry)
	}
	return ""
}

func mainView(m model) string {
	title := titleStyle.Render("KERNO — HTTP Observer")
	if m.paused {
		title += "  " + pauseStyle.Render("⏸ PAUSED")
	}

	filterLine := ""
	if m.filtering {
		filterLine = "\n  " + filterStyle.Render("/ "+m.filter+"█")
	} else if m.filter != "" {
		filterLine = "\n  " + dimStyle.Render("filter: "+m.filter+"  (esc hapus)")
	}

	footer := dimStyle.Render(fmt.Sprintf(
		"  sort: %s ▼ · s sort · / filter · space pause · enter buka service · q keluar",
		sortName[m.sortBy],
	))

	return "\n" + title + filterLine + "\n" + borderStyle.Render(m.mainTable.View()) + "\n" + footer
}

func serviceView(m model) string {
	title := titleStyle.Render("KERNO") +
		dimStyle.Render(" / ") +
		titleStyle.Render(m.selectedGroup.name)

	footer := dimStyle.Render(
		"  esc kembali · s sort · space pause · enter lihat detail · q keluar",
	)

	return "\n" + title + "\n" + borderStyle.Render(m.serviceTable.View()) + "\n" + footer
}

// detailView menampilkan bar chart latency untuk satu endpoint.
func detailView(e entry) string {
	lats := e.s.Latencies
	p50 := aggregator.Percentile(lats, 50)
	p75 := aggregator.Percentile(lats, 75)
	p90 := aggregator.Percentile(lats, 90)
	p95 := aggregator.Percentile(lats, 95)
	p99 := aggregator.Percentile(lats, 99)

	bar := func(v uint64) string { return renderBar(v, p99, 30) }

	lines := []string{
		"",
		"  " + titleStyle.Render(e.service+" · "+e.method+" "+e.path),
		"",
		fmt.Sprintf("  %-4s  %8s  %s", "P50", aggregator.FmtLatency(p50), bar(p50)),
		fmt.Sprintf("  %-4s  %8s  %s", "P75", aggregator.FmtLatency(p75), bar(p75)),
		fmt.Sprintf("  %-4s  %8s  %s", "P90", aggregator.FmtLatency(p90), bar(p90)),
		fmt.Sprintf("  %-4s  %8s  %s", "P95", aggregator.FmtLatency(p95), bar(p95)),
		fmt.Sprintf("  %-4s  %8s  %s", "P99", aggregator.FmtLatency(p99), bar(p99)),
		"",
		fmt.Sprintf("  Total: %d req · Errors: %d (%.1f%%)", e.s.Count, e.s.ErrCount, e.errPct),
		"",
		dimStyle.Render("  esc kembali"),
	}
	return strings.Join(lines, "\n")
}

// renderBar menggambar progress bar unicode sebanding dengan value/max.
func renderBar(value, max uint64, width int) string {
	if max == 0 {
		return dimStyle.Render(strings.Repeat("░", width))
	}
	filled := int(float64(value) / float64(max) * float64(width))
	if filled > width {
		filled = width
	}
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Render(strings.Repeat("█", filled))
	return bar + dimStyle.Render(strings.Repeat("░", width-filled))
}

// ── Row builders ──────────────────────────────────────────────────────────────

// buildMainRows membangun baris untuk halaman 1 (daftar service).
func buildMainRows(groups []serviceGroup) []table.Row {
	rows := make([]table.Row, 0, len(groups))
	for _, g := range groups {
		rows = append(rows, table.Row{
			g.name,
			fmt.Sprintf("%d", len(g.endpoints)),
			fmt.Sprintf("%d/s", g.totalRPS),
			colorP99(g.maxP99),
			colorErr(g.worstErr),
		})
	}
	return rows
}

// buildServiceRows membangun baris untuk halaman 2 (endpoint satu service).
func buildServiceRows(endpoints []entry) []table.Row {
	rows := make([]table.Row, 0, len(endpoints))
	for _, e := range endpoints {
		rows = append(rows, table.Row{
			e.method + " " + e.path,
			fmt.Sprintf("%d/s", e.rps),
			aggregator.FmtLatency(e.p50),
			colorP99(e.p99),
			colorErr(e.errPct),
		})
	}
	return rows
}

// colorP99 returns a plain string; lipgloss styling inside bubbletea table
// cells conflicts with the row-highlight style and can make the cell blank.
func colorP99(ns uint64) string {
	return aggregator.FmtLatency(ns)
}

func colorErr(errPct float64) string {
	if errPct == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", errPct)
}

// ── Data helpers ──────────────────────────────────────────────────────────────

// buildGroups mengambil snapshot, menerapkan filter, mengelompokkan per service,
// lalu mengurutkan group dan endpoint di dalamnya.
func buildGroups(
	getStats func() map[string]*collector.EndpointStats,
	sortBy sortField,
	filter string,
) []serviceGroup {
	stats := getStats()
	filterLower := strings.ToLower(filter)

	groupMap := make(map[string]*serviceGroup)
	var order []string

	for k, s := range stats {
		parts := strings.Fields(k)
		service, method, path := "", "", ""
		if len(parts) >= 1 {
			service = parts[0]
		}
		if len(parts) >= 2 {
			method = parts[1]
		}
		if len(parts) >= 3 {
			path = parts[2]
		}

		if filterLower != "" {
			if !strings.Contains(strings.ToLower(service+method+path), filterLower) {
				continue
			}
		}

		errPct := 0.0
		if s.Count > 0 {
			errPct = float64(s.ErrCount) / float64(s.Count) * 100
		}

		e := entry{
			key:     k,
			service: service,
			method:  method,
			path:    path,
			s:       s,
			rps:     s.Count / 2,
			p50:     aggregator.Percentile(s.Latencies, 50),
			p99:     aggregator.Percentile(s.Latencies, 99),
			errPct:  errPct,
		}

		if _, ok := groupMap[service]; !ok {
			groupMap[service] = &serviceGroup{name: service}
			order = append(order, service)
		}
		g := groupMap[service]
		g.endpoints = append(g.endpoints, e)
		g.totalRPS += e.rps
		if e.p99 > g.maxP99 {
			g.maxP99 = e.p99
		}
		if e.errPct > g.worstErr {
			g.worstErr = e.errPct
		}
	}

	// Sort endpoint dalam tiap group.
	for _, g := range groupMap {
		sort.Slice(g.endpoints, func(i, j int) bool {
			return sortVal(g.endpoints[i], sortBy) > sortVal(g.endpoints[j], sortBy)
		})
	}

	// Sort groups.
	groups := make([]serviceGroup, 0, len(order))
	for _, name := range order {
		groups = append(groups, *groupMap[name])
	}
	sort.Slice(groups, func(i, j int) bool {
		switch sortBy {
		case sortByP99:
			return groups[i].maxP99 > groups[j].maxP99
		case sortByErr:
			return groups[i].worstErr > groups[j].worstErr
		default:
			return groups[i].totalRPS > groups[j].totalRPS
		}
	})

	return groups
}

func sortVal(e entry, sortBy sortField) float64 {
	switch sortBy {
	case sortByP50:
		return float64(e.p50)
	case sortByP99:
		return float64(e.p99)
	case sortByErr:
		return e.errPct
	default:
		return float64(e.rps)
	}
}

// ── Output ────────────────────────────────────────────────────────────────────

// Output runs the Bubbletea TUI.
type Output struct {
	getStats func() map[string]*collector.EndpointStats
}

// New creates an Output using the provided stats getter.
func New(getStats func() map[string]*collector.EndpointStats) *Output {
	return &Output{getStats: getStats}
}

// Render starts the full-screen TUI and blocks until the user quits.
func (o *Output) Render() {
	mainTable := newTable([]table.Column{
		{Title: "SERVICE", Width: 20},
		{Title: "ENDPOINTS", Width: 10},
		{Title: "REQ/S", Width: 7},
		{Title: "P99 max", Width: 9},
		{Title: "ERR% max", Width: 9},
	})

	serviceTable := newTable([]table.Column{
		{Title: "ENDPOINT", Width: 36},
		{Title: "REQ/S", Width: 7},
		{Title: "P50", Width: 7},
		{Title: "P99", Width: 7},
		{Title: "ERR%", Width: 6},
	})

	p := tea.NewProgram(
		model{
			getStats:     o.getStats,
			mainTable:    mainTable,
			serviceTable: serviceTable,
			sortBy:       sortByCount,
		},
		tea.WithAltScreen(),
	)
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

// newTable membuat table.Model dengan styling default Kerno.
func newTable(columns []table.Column) table.Model {
	t := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
		table.WithFocused(true),
		table.WithHeight(20),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)
	return t
}
