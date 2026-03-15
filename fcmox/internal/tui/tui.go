package tui

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	vm "fcmox/internal/vmManager"
)

// ─── Color Palette ───────────────────────────────────────────────────────────
// Tokyo Night inspired theme

var (
	colorBg       = lipgloss.Color("#1A1B26")
	colorBorder   = lipgloss.Color("#3B4261")
	colorBorderHi = lipgloss.Color("#7AA2F7")

	colorText     = lipgloss.Color("#C0CAF5")
	colorTextDim  = lipgloss.Color("#565F89")
	colorTextBold = lipgloss.Color("#E0E6FF")

	colorTeal   = lipgloss.Color("#73DACA")
	colorBlue   = lipgloss.Color("#7AA2F7")
	colorAmber  = lipgloss.Color("#E0AF68")
	colorGreen  = lipgloss.Color("#9ECE6A")
	colorRed    = lipgloss.Color("#F7768E")
	colorPurple = lipgloss.Color("#BB9AF7")
	colorOrange = lipgloss.Color("#FF9E64")
	colorCyan   = lipgloss.Color("#2AC3DE")
)

// ─── Actions ─────────────────────────────────────────────────────────────────

type action struct {
	key  string
	name string
	desc string
	icon string
}

var actions = []action{
	{key: "c", name: "Create", desc: "Spin up a new VM", icon: "＋"},
	{key: "d", name: "Delete", desc: "Remove selected VM", icon: "X"},
	{key: "s", name: "Start", desc: "Boot selected VM", icon: "▶"},
	{key: "p", name: "Pause", desc: "Pause selected VM", icon: "■"},
	{key: "r", name: "Resume", desc: "Resume selected VM", icon: "▶"},
	{key: "l", name: "Logs", desc: "Serial console output", icon: "☰"},
	{key: "x", name: "Shell", desc: "SSH into VM", icon: ">_"},
}

// ─── Focus Area ──────────────────────────────────────────────────────────────

type focus int

const (
	focusVmTable focus = iota
	focusActions
)

// ─── Mode ────────────────────────────────────────────────────────────────────

type mode int

const (
	modeNormal mode = iota
	modeCreateForm
)

// ─── Create Form ─────────────────────────────────────────────────────────────

type createStep int

const (
	createStepKernel createStep = iota
	createStepRootfs
	createStepResources
)

const (
	createFieldCpu = iota
	createFieldMem
	createFieldCount
)

// ─── Model ───────────────────────────────────────────────────────────────────

// tickMsg drives periodic log refresh.
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type Model struct {
	mgr            *vm.VmManager
	selectedVm     int
	selectedAction int
	focus          focus
	mode           mode
	width          int
	height         int
	message        string
	messageIsError bool

	// Cached log lines for the selected VM
	vmLogCache string

	// Create wizard state
	createStep       createStep
	createKernelKeys []string // sorted kernel names
	createRootfsKeys []string // sorted rootfs names
	createSelKernel  int      // cursor in kernel list
	createSelRootfs  int      // cursor in rootfs list
	createInputs     []textinput.Model
	createFocusIdx   int
	createError      string
}

func NewModel(mgr *vm.VmManager) Model {
	// Initialize create form text inputs
	inputs := make([]textinput.Model, createFieldCount)

	inputs[createFieldCpu] = textinput.New()
	inputs[createFieldCpu].Placeholder = "2"
	inputs[createFieldCpu].CharLimit = 3
	inputs[createFieldCpu].Width = 12
	inputs[createFieldCpu].Prompt = ""

	inputs[createFieldMem] = textinput.New()
	inputs[createFieldMem].Placeholder = "512"
	inputs[createFieldMem].CharLimit = 6
	inputs[createFieldMem].Width = 12
	inputs[createFieldMem].Prompt = ""

	return Model{
		mgr:          mgr,
		width:        120,
		createInputs: inputs,
	}
}

// ─── Tea Interface ───────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		// Refresh the log cache for the selected VM.
		m.vmLogCache = m.readSelectedVmLogs(20)
		return m, tickCmd()

	case tea.KeyMsg:
		switch m.mode {
		case modeCreateForm:
			return m.updateCreateForm(msg)
		default:
			return m.handleKey(msg)
		}
	}
	return m, nil
}

// ─── Normal Mode Key Handler ─────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		if m.focus == focusVmTable {
			m.focus = focusActions
		} else {
			m.focus = focusVmTable
		}
		m.message = ""

	case "up", "k":
		if m.focus == focusVmTable {
			if m.selectedVm > 0 {
				m.selectedVm--
			}
		} else {
			if m.selectedAction > 0 {
				m.selectedAction--
			}
		}

	case "down", "j":
		if m.focus == focusVmTable {
			if m.selectedVm < m.mgr.VmCount()-1 {
				m.selectedVm++
			}
		} else {
			if m.selectedAction < len(actions)-1 {
				m.selectedAction++
			}
		}
	case "enter":
		if m.focus == focusActions {
			return m.executeAction()
		}

	case "c":
		if m.focus == focusActions {
			m.selectedAction = 0
			return m.executeAction()
		}
	case "d":
		if m.focus == focusActions {
			m.selectedAction = 1
			return m.executeAction()
		}
	case "s":
		if m.focus == focusActions {
			m.selectedAction = 2
			return m.executeAction()
		}
	case "p":
		if m.focus == focusActions {
			m.selectedAction = 3
			return m.executeAction()
		}
	}

	return m, nil
}

// ─── Create Wizard ───────────────────────────────────────────────────────────

func (m Model) enterCreateForm() (Model, tea.Cmd) {
	m.mode = modeCreateForm
	m.createStep = createStepKernel
	m.createError = ""
	m.createSelKernel = 0
	m.createSelRootfs = 0

	// Build sorted key lists for selection.
	m.createKernelKeys = sortedKeys(m.mgr.Kernels)
	m.createRootfsKeys = sortedKeys(m.mgr.Rootfs)

	// Reset text inputs.
	for i := range m.createInputs {
		m.createInputs[i].SetValue("")
		m.createInputs[i].Blur()
	}

	return m, nil
}

func (m Model) updateCreateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		// Go back one step, or cancel entirely.
		switch m.createStep {
		case createStepKernel:
			m.mode = modeNormal
			m.message = "Create cancelled"
			m.messageIsError = false
			return m, nil
		case createStepRootfs:
			m.createStep = createStepKernel
			m.createError = ""
			return m, nil
		case createStepResources:
			m.createStep = createStepRootfs
			m.createError = ""
			return m, nil
		}
	}

	switch m.createStep {
	case createStepKernel:
		return m.updateKernelSelect(msg)
	case createStepRootfs:
		return m.updateRootfsSelect(msg)
	case createStepResources:
		return m.updateResourceInputs(msg)
	}
	return m, nil
}

// ── Step 1: Kernel selection ─────────────────────────────────────────────────

func (m Model) updateKernelSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.createSelKernel > 0 {
			m.createSelKernel--
		}
	case "down", "j":
		if m.createSelKernel < len(m.createKernelKeys)-1 {
			m.createSelKernel++
		}
	case "enter":
		if len(m.createKernelKeys) == 0 {
			m.createError = "No kernels available"
			return m, nil
		}
		m.createStep = createStepRootfs
		m.createError = ""
	}
	return m, nil
}

// ── Step 2: Rootfs selection ─────────────────────────────────────────────────

func (m Model) updateRootfsSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.createSelRootfs > 0 {
			m.createSelRootfs--
		}
	case "down", "j":
		if m.createSelRootfs < len(m.createRootfsKeys)-1 {
			m.createSelRootfs++
		}
	case "enter":
		if len(m.createRootfsKeys) == 0 {
			m.createError = "No rootfs available"
			return m, nil
		}
		m.createStep = createStepResources
		m.createError = ""
		m.createFocusIdx = createFieldCpu
		m.createInputs[createFieldCpu].Focus()
		return m, m.createInputs[createFieldCpu].Cursor.BlinkCmd()
	}
	return m, nil
}

// ── Step 3: CPU + Memory inputs ──────────────────────────────────────────────

func (m Model) updateResourceInputs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "down":
		m.createFocusIdx = (m.createFocusIdx + 1) % createFieldCount
		return m.updateCreateFocus()
	case "shift+tab", "up":
		m.createFocusIdx = (m.createFocusIdx - 1 + createFieldCount) % createFieldCount
		return m.updateCreateFocus()
	case "enter":
		if m.createFocusIdx == createFieldCount-1 {
			return m.submitCreateForm()
		}
		m.createFocusIdx++
		return m.updateCreateFocus()
	default:
		// Forward to text input.
		var cmd tea.Cmd
		m.createInputs[m.createFocusIdx], cmd = m.createInputs[m.createFocusIdx].Update(msg)
		return m, cmd
	}
}

func (m Model) updateCreateFocus() (Model, tea.Cmd) {
	var cmds []tea.Cmd
	for i := range m.createInputs {
		if i == m.createFocusIdx {
			m.createInputs[i].Focus()
			cmds = append(cmds, m.createInputs[i].Cursor.BlinkCmd())
		} else {
			m.createInputs[i].Blur()
		}
	}
	return m, tea.Batch(cmds...)
}

func (m Model) submitCreateForm() (Model, tea.Cmd) {
	// Parse CPU
	cpuStr := m.createInputs[createFieldCpu].Value()
	if cpuStr == "" {
		cpuStr = m.createInputs[createFieldCpu].Placeholder
	}
	cpus, err := strconv.Atoi(cpuStr)
	if err != nil || cpus < 1 || cpus > 32 {
		m.createError = "CPU must be a number between 1 and 32"
		return m, nil
	}

	// Parse Memory
	memStr := m.createInputs[createFieldMem].Value()
	if memStr == "" {
		memStr = m.createInputs[createFieldMem].Placeholder
	}
	memMB, err := strconv.Atoi(memStr)
	if err != nil || memMB < 128 || memMB > 32768 {
		m.createError = "Memory must be 128-32768 MB"
		return m, nil
	}

	// Resolve selected kernel and rootfs paths.
	kernelName := m.createKernelKeys[m.createSelKernel]
	rootfsName := m.createRootfsKeys[m.createSelRootfs]
	kernelPath := m.mgr.Kernels[kernelName]
	rootfsPath := m.mgr.Rootfs[rootfsName]

	// Create the VM (spawns firecracker process).
	created, err := m.mgr.CreateVm(cpus, memMB, kernelPath, rootfsPath, 0)
	if err != nil {
		m.createError = fmt.Sprintf("create failed: %v", err)
		return m, nil
	}

	// Boot the VM (configure + InstanceStart via API).
	if err := m.mgr.StartVm(created.Id); err != nil {
		m.createError = fmt.Sprintf("boot failed: %v", err)
		return m, nil
	}

	m.mode = modeNormal
	m.message = fmt.Sprintf("✓ Created & booted %s  (%d vCPU, %d MB, kernel: %s, rootfs: %s)",
		created.Id, cpus, memMB, kernelName, rootfsName)
	m.messageIsError = false
	return m, nil
}

func (m Model) executeAction() (Model, tea.Cmd) {
	if m.mgr.VmCount() == 0 && m.selectedAction != 0 {
		m.message = "No VMs available"
		m.messageIsError = true
		return m, nil
	}

	act := actions[m.selectedAction]
	switch act.key {
	case "c":
		return m.enterCreateForm()

	case "d":
		if m.mgr.VmCount() > 0 {
			keys := sortedVmKeys(m.mgr.Vms)
			if m.selectedVm >= len(keys) {
				break
			}
			selID := keys[m.selectedVm]
			if err := m.mgr.StopVm(selID); err != nil {
				// Already stopped is fine — proceed to delete
				_ = err
			}
			if m.selectedVm >= m.mgr.VmCount() && m.selectedVm > 0 {
				m.selectedVm--
			}
			m.message = fmt.Sprintf("✓ Deleted %s", selID)
			m.messageIsError = false
		}

	case "s":
		keys := sortedVmKeys(m.mgr.Vms)
		if m.selectedVm >= len(keys) {
			break
		}
		selID := keys[m.selectedVm]
		v := m.mgr.Vms[selID]
		if v.Status == vm.VmStatusRunning {
			m.message = fmt.Sprintf("⚠ %s is already running", selID)
			m.messageIsError = true
		} else {
			v.Status = vm.VmStatusRunning
			m.message = fmt.Sprintf("✓ Started %s", selID)
			m.messageIsError = false
		}

	case "p":
		keys := sortedVmKeys(m.mgr.Vms)
		if m.selectedVm >= len(keys) {
			break
		}
		selID := keys[m.selectedVm]
		if err := m.mgr.PauseVm(selID); err != nil {
			m.message = fmt.Sprintf("⚠ %v", err)
			m.messageIsError = true
		} else {
			m.message = fmt.Sprintf("✓ Paused %s", selID)
			m.messageIsError = false
		}

	case "r":
		keys := sortedVmKeys(m.mgr.Vms)
		if m.selectedVm >= len(keys) {
			break
		}
		selID := keys[m.selectedVm]
		if err := m.mgr.ResumeVm(selID); err != nil {
			m.message = fmt.Sprintf("⚠ %v", err)
			m.messageIsError = true
		} else {
			m.message = fmt.Sprintf("✓ Resumed %s", selID)
			m.messageIsError = false
		}

	case "l":
		keys := sortedVmKeys(m.mgr.Vms)
		if m.selectedVm < len(keys) {
			v := m.mgr.Vms[keys[m.selectedVm]]
			m.message = fmt.Sprintf("→ Opening logs for %s (serial: %s)…", v.Id, v.SockPath)
			m.messageIsError = false
		}

	case "x":
		keys := sortedVmKeys(m.mgr.Vms)
		if m.selectedVm < len(keys) {
			v := m.mgr.Vms[keys[m.selectedVm]]
			m.message = fmt.Sprintf("→ SSH into %s@%s…", v.Id, v.Ip)
			m.messageIsError = false
		}
	}

	return m, nil
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	w := m.width
	if w < 60 {
		w = 60
	}
	h := m.height
	if h < 24 {
		h = 24
	}

	var sections []string

	// ── HEADER (2 lines) ──
	titleText := "  🔥 fcmox"
	subtitle := "Firecracker VM Manager"
	spacer := strings.Repeat(" ", max(0, w-lipgloss.Width(titleText)-len(subtitle)-6))

	headerBar := lipgloss.NewStyle().
		Foreground(colorTextBold).
		Bold(true).
		Width(w).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false).
		BorderForeground(colorBorder).
		Render(titleText + spacer + lipgloss.NewStyle().Foreground(colorTextDim).Render(subtitle) + "  ")

	sections = append(sections, headerBar)

	// ── HEIGHT BUDGET ──
	// Total height = h
	// Header = 2 lines
	// Bottom Action Panel = roughly 7 lines + 2 borders = 9 lines
	// Message = 1 line + 1 line spacing = 2 lines
	// Statusbar = 1 line
	// Fixed overhead = 14 lines
	// Top section gets the rest
	overhead := 14
	topHeight := h - overhead
	if topHeight < 10 {
		topHeight = 10
	}

	// ── PANEL WIDTHS ──
	// Left box (Table), Right box (Details/Logs)
	// Both have left+right borders = 4 columns of borders total.
	leftInnerW := (w * 50 / 100) - 2
	rightInnerW := w - leftInnerW - 4

	// Left box height
	leftInnerH := topHeight

	// Right box has two stacked panels. Each has top+bottom borders = 4 rows of borders.
	// We want left box total height (leftInnerH + 2) to equal right column total height.
	// Total right column height = (detailH + 2) + (logsH + 2) = detailH + logsH + 4.
	// So detailH + logsH = leftInnerH - 2.
	rightTotalContentH := leftInnerH - 2
	detailInnerH := rightTotalContentH / 2
	logsInnerH := rightTotalContentH - detailInnerH

	// ── RENDER TABLE (LEFT) ──
	tableBorderColor := colorBorder
	tableTitle := " Virtual Machines "
	if m.focus == focusVmTable {
		tableBorderColor = colorBlue
		tableTitle = " ▸ Virtual Machines "
	}

	tableContent := m.renderVmTable(leftInnerW, leftInnerH)
	tablePanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tableBorderColor).
		Width(leftInnerW).
		Height(leftInnerH).
		Render(lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(" "+tableTitle+" ") + "\n" + tableContent)

	// ── RENDER DETAILS (RIGHT TOP) ──
	detailContent := m.renderVmDetail(rightInnerW-1, detailInnerH)
	detailPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Width(rightInnerW).
		Height(detailInnerH).
		Render(lipgloss.NewStyle().Foreground(colorAmber).Bold(true).Render(" VM Details ") + "\n" + detailContent)

	// ── RENDER LOGS (RIGHT BOTTOM) ──
	logsContent := m.renderVmLogs(rightInnerW-1, logsInnerH-1) // -1 for title
	logsPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Width(rightInnerW).
		Height(logsInnerH).
		Render(lipgloss.NewStyle().Foreground(colorOrange).Bold(true).Render(" VM Logs ") + "\n" + logsContent)

	rightColumn := lipgloss.JoinVertical(lipgloss.Left, detailPanel, logsPanel)

	// Join Top Section
	topSection := lipgloss.JoinHorizontal(lipgloss.Top, tablePanel, rightColumn)
	sections = append(sections, topSection)

	// ── BOTTOM SECTION (Actions/Form) ──
	if m.mode == modeCreateForm {
		sections = append(sections, " "+m.renderCreateForm(w-4))
	} else {
		actionsBorderColor := colorBorder
		actionsTitle := " Actions "
		if m.focus == focusActions {
			actionsBorderColor = colorBlue
			actionsTitle = " ▸ Actions "
		}

		actionsContent := m.renderActions(w - 4)
		actionsPanel := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(actionsBorderColor).
			Width(w - 2).
			Render(lipgloss.NewStyle().Foreground(colorPurple).Bold(true).Render(" "+actionsTitle+" ") + "\n" + actionsContent)

		sections = append(sections, actionsPanel)
	}

	// ── MESSAGE ──
	msgLine := ""
	if m.message != "" {
		msgStyle := lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
		if m.messageIsError {
			msgStyle = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
		}
		msgLine = "  " + msgStyle.Render(m.message)
	}
	sections = append(sections, msgLine, "")

	// ── STATUS BAR ──
	var statusHelpText string
	if m.mode == modeCreateForm {
		statusHelpText = "  tab/↑↓ select field  enter next/submit  esc cancel"
	} else {
		statusHelpText = "  tab switch panel  ↑↓ select  enter execute  q quit"
	}
	statusLeft := lipgloss.NewStyle().Foreground(colorTextDim).Render(statusHelpText)
	vmCount := fmt.Sprintf("%d VMs ", m.mgr.VmCount())
	runCount := m.mgr.RunningCount()
	statusRight := lipgloss.NewStyle().Foreground(colorGreen).Render(fmt.Sprintf("%d running", runCount))
	statusMid := lipgloss.NewStyle().Foreground(colorTextDim).Render(" │ ")

	rightBlock := lipgloss.NewStyle().Foreground(colorAmber).Render(vmCount) + statusMid + statusRight + "  "
	spacerLen := max(0, w-lipgloss.Width(statusLeft)-lipgloss.Width(rightBlock))
	statusBar := lipgloss.NewStyle().Background(lipgloss.Color("#1a1a1a")).Render(statusLeft + strings.Repeat(" ", spacerLen) + rightBlock)

	sections = append(sections, statusBar)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// ─── Render: Create Form ─────────────────────────────────────────────────────

func (m Model) renderCreateForm(panelWidth int) string {
	// Step indicator bar
	steps := []string{"Kernel", "Rootfs", "Resources"}
	var stepParts []string
	for i, name := range steps {
		num := fmt.Sprintf("%d", i+1)
		if createStep(i) == m.createStep {
			stepParts = append(stepParts, lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render("["+num+"] "+name))
		} else if createStep(i) < m.createStep {
			stepParts = append(stepParts, lipgloss.NewStyle().Foreground(colorGreen).Render("✓ "+name))
		} else {
			stepParts = append(stepParts, lipgloss.NewStyle().Foreground(colorTextDim).Render(num+" "+name))
		}
	}
	stepBar := "  " + strings.Join(stepParts, lipgloss.NewStyle().Foreground(colorBorder).Render("  →  "))

	var content string

	switch m.createStep {
	case createStepKernel:
		content = m.renderListSelector("Select Kernel Image", m.createKernelKeys, m.createSelKernel, panelWidth-8)
	case createStepRootfs:
		content = m.renderListSelector("Select Root Filesystem", m.createRootfsKeys, m.createSelRootfs, panelWidth-8)
	case createStepResources:
		content = m.renderResourceInputs(panelWidth - 8)
	}

	// Error message
	if m.createError != "" {
		content += "\n  " + lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("⚠ "+m.createError)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan).
		Width(panelWidth).
		Padding(0, 1).
		Render(
			lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(" ＋ Create New VM ") + "\n" +
				stepBar + "\n\n" +
				content + "\n",
		)
}

// renderListSelector renders a scrollable list with a highlighted cursor.
func (m Model) renderListSelector(title string, items []string, selected int, maxWidth int) string {
	titleLine := "  " + lipgloss.NewStyle().Foreground(colorAmber).Bold(true).Render(title)

	if len(items) == 0 {
		return titleLine + "\n  " + lipgloss.NewStyle().Foreground(colorTextDim).Italic(true).Render("None available")
	}

	var lines []string
	lines = append(lines, titleLine)

	for i, item := range items {
		if i == selected {
			pointer := lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render("▸ ")
			name := lipgloss.NewStyle().Foreground(colorTextBold).Bold(true).Render(truncate(item, maxWidth-4))
			lines = append(lines, "  "+pointer+name)
		} else {
			name := lipgloss.NewStyle().Foreground(colorText).Render(truncate(item, maxWidth-4))
			lines = append(lines, "    "+name)
		}
	}

	return strings.Join(lines, "\n")
}

// renderResourceInputs renders the CPU and Memory text input fields.
func (m Model) renderResourceInputs(maxWidth int) string {
	labelStyle := lipgloss.NewStyle().Foreground(colorAmber).Bold(true).Width(14)
	inputFocused := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorderHi).
		Padding(0, 1).
		Width(18)
	inputBlurred := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(18)

	// Selected kernel + rootfs summary
	kernelSummary := lipgloss.NewStyle().Foreground(colorTextDim).Render(
		fmt.Sprintf("  Kernel: %s  |  Rootfs: %s",
			m.createKernelKeys[m.createSelKernel],
			m.createRootfsKeys[m.createSelRootfs]))

	var fields []string
	fields = append(fields, kernelSummary)
	fields = append(fields, "")

	// CPU
	cpuLabel := labelStyle.Render("CPU Cores:")
	cpuInput := inputBlurred.Render(m.createInputs[createFieldCpu].View())
	if m.createFocusIdx == createFieldCpu {
		cpuInput = inputFocused.Render(m.createInputs[createFieldCpu].View())
	}
	cpuHint := lipgloss.NewStyle().Foreground(colorTextDim).Render("  (1-32)")
	fields = append(fields, "  "+cpuLabel+cpuInput+cpuHint)

	// Memory
	memLabel := labelStyle.Render("Memory (MB):")
	memInput := inputBlurred.Render(m.createInputs[createFieldMem].View())
	if m.createFocusIdx == createFieldMem {
		memInput = inputFocused.Render(m.createInputs[createFieldMem].View())
	}
	memHint := lipgloss.NewStyle().Foreground(colorTextDim).Render("  (128-32768)")
	fields = append(fields, "  "+memLabel+memInput+memHint)

	return strings.Join(fields, "\n")
}

// ─── Render: VM Table ────────────────────────────────────────────────────────

func (m Model) renderVmTable(maxWidth int, maxLines int) string {
	if m.mgr.VmCount() == 0 {
		return lipgloss.NewStyle().
			Foreground(colorTextDim).
			Italic(true).
			Render("  No virtual machines. Press [C] to create one.")
	}

	colId := 8
	colStatus := 10
	colCpu := 6
	colMem := 8
	colIp := 15
	colTap := 12

	hdrStyle := lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	hdr := "  " + hdrStyle.Copy().Width(colId).Render("ID") +
		hdrStyle.Copy().Width(colStatus).Render("STATUS") +
		hdrStyle.Copy().Width(colCpu).Render("CPU") +
		hdrStyle.Copy().Width(colMem).Render("MEM") +
		hdrStyle.Copy().Width(colIp).Render("IP") +
		hdrStyle.Copy().Width(colTap).Render("TAP")

	divider := "  " + lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("─", colId+colStatus+colCpu+colMem+colIp+colTap-2))

	rows := []string{hdr, divider}

	for i, key := range sortedVmKeys(m.mgr.Vms) {
		v := m.mgr.Vms[key]
		isSelected := i == m.selectedVm

		rowStyle := lipgloss.NewStyle().Foreground(colorText)
		prefix := "  "
		if isSelected {
			rowStyle = lipgloss.NewStyle().Foreground(colorTextBold).Bold(true)
			if m.focus == focusVmTable {
				prefix = lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render("▸ ")
			}
		}

		statusStr := statusColored(v.Status)

		row := prefix +
			rowStyle.Copy().Width(colId).Render(truncate(v.Id, colId-1)) +
			lipgloss.NewStyle().Width(colStatus).Render(statusStr) +
			rowStyle.Copy().Width(colCpu).Render(fmt.Sprintf("%d", v.VmCpuCount)) +
			rowStyle.Copy().Width(colMem).Render(fmt.Sprintf("%dM", v.VmMemSize)) +
			rowStyle.Copy().Width(colIp).Render(truncate(v.Ip, colIp-1)) +
			rowStyle.Copy().Width(colTap).Render(truncate(v.TapDev, colTap-1))

		if isSelected && m.focus == focusVmTable {
			padLen := max(0, maxWidth-lipgloss.Width(row)-2)
			row = row + strings.Repeat(" ", padLen)
		}

		rows = append(rows, row)
		// Stop if we reach maxLines. -1 because we reserve space for one more item or padding.
		if len(rows) >= maxLines-1 {
			break
		}
	}

	return strings.Join(rows, "\n")
}

// ─── Render: VM Detail ──────────────────────────────────────────────────────

func (m Model) renderVmDetail(maxWidth int, maxLines int) string {
	if m.mgr.VmCount() == 0 {
		return lipgloss.NewStyle().
			Foreground(colorTextDim).
			Italic(true).
			Render("  Select a VM to view details")
	}

	v := m.mgr.Vms[sortedVmKeys(m.mgr.Vms)[m.selectedVm]]

	labelStyle := lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Width(10)
	valueStyle := lipgloss.NewStyle().Foreground(colorText)
	dimValueStyle := lipgloss.NewStyle().Foreground(colorTextDim)

	// Status badge
	bracketStyle := lipgloss.NewStyle().Foreground(colorBorder)
	statusBadge := statusBadgeOutline(v.Status, bracketStyle)

	vmTitle := lipgloss.NewStyle().Foreground(colorTextBold).Bold(true).Render("  " + v.Id)

	lines := []string{
		vmTitle + "  " + statusBadge,
		"",
		"  " + labelStyle.Render("CPU") + valueStyle.Render(fmt.Sprintf("%d vCPU", v.VmCpuCount)),
		"  " + labelStyle.Render("Memory") + valueStyle.Render(fmt.Sprintf("%d MB", v.VmMemSize)),
		"  " + labelStyle.Render("IP") + valueStyle.Render(v.Ip),
		"  " + labelStyle.Render("MAC") + dimValueStyle.Render(v.MacAddr),
		"  " + labelStyle.Render("TAP") + valueStyle.Render(v.TapDev),
		"",
		"  " + lipgloss.NewStyle().Foreground(colorPurple).Bold(true).Render("── Paths ──"),
		"  " + labelStyle.Render("Kernel") + dimValueStyle.Render(truncate(v.KernelPath, maxWidth-14)),
		"  " + labelStyle.Render("Rootfs") + dimValueStyle.Render(truncate(v.RootfsPath, maxWidth-14)),
		"  " + labelStyle.Render("Socket") + dimValueStyle.Render(truncate(v.SockPath, maxWidth-14)),
		"",
		"  " + lipgloss.NewStyle().Foreground(colorPurple).Bold(true).Render("── Boot ──"),
		"  " + dimValueStyle.Render(truncate(v.BootArgs, maxWidth-4)),
	}

	// Truncate output lines to never overflow the container height
	if len(lines) > maxLines-1 {
		lines = lines[:maxLines-1]
	}

	return strings.Join(lines, "\n")
}

// ─── Render: VM Logs ─────────────────────────────────────────────────────────

// readSelectedVmLogs reads the last N lines from the selected VM's log file.
func (m Model) readSelectedVmLogs(maxLines int) string {
	if m.mgr.VmCount() == 0 {
		return ""
	}
	keys := sortedVmKeys(m.mgr.Vms)
	if m.selectedVm >= len(keys) {
		return ""
	}
	v := m.mgr.Vms[keys[m.selectedVm]]
	if v.LogPath == "" {
		return ""
	}

	data, err := os.ReadFile(v.LogPath)
	if err != nil {
		return ""
	}

	allLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	start := len(allLines) - maxLines
	if start < 0 {
		start = 0
	}
	return strings.Join(allLines[start:], "\n")
}

func (m Model) renderVmLogs(maxWidth int, maxLines int) string {
	if m.mgr.VmCount() == 0 {
		return lipgloss.NewStyle().
			Foreground(colorTextDim).
			Italic(true).
			Padding(1, 1).
			Render("No VM selected")
	}

	if m.vmLogCache == "" {
		return lipgloss.NewStyle().
			Foreground(colorTextDim).
			Italic(true).
			Render("  No log output yet…")
	}

	// Truncate each line to fit the panel width, and limit total lines.
	logLines := strings.Split(m.vmLogCache, "\n")
	if len(logLines) > maxLines {
		logLines = logLines[len(logLines)-maxLines:]
	}
	var truncated []string
	for _, line := range logLines {
		truncated = append(truncated, "  "+truncate(line, maxWidth-3))
	}

	return lipgloss.NewStyle().
		Foreground(colorTextDim).
		Render(strings.Join(truncated, "\n"))
}

// ─── Render: Actions ─────────────────────────────────────────────────────────

func (m Model) renderActions(maxWidth int) string {
	cols := 3
	colWidth := maxWidth / cols

	var allRendered []string
	var currentRow []string

	for i, act := range actions {
		isSelected := i == m.selectedAction && m.focus == focusActions

		bracket := lipgloss.NewStyle().Foreground(colorBorder)
		var rendered string
		if isSelected {
			keyBadge := bracket.Render("[") +
				lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render(act.key) +
				bracket.Render("]")
			nameStr := lipgloss.NewStyle().Foreground(colorTextBold).Bold(true).Render(act.name)
			descStr := lipgloss.NewStyle().Foreground(colorText).Render(act.desc)
			selectBar := lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render("│ ")
			rendered = lipgloss.NewStyle().
				Width(colWidth-1).
				Padding(0, 1).
				Render(fmt.Sprintf("%s%s %s %s\n%s   %s", selectBar, act.icon, keyBadge, nameStr, selectBar, descStr))
		} else {
			keyBadge := bracket.Render("[") +
				lipgloss.NewStyle().Foreground(colorBlue).Render(act.key) +
				bracket.Render("]")
			nameStr := lipgloss.NewStyle().Foreground(colorText).Render(act.name)
			descStr := lipgloss.NewStyle().Foreground(colorTextDim).Render(act.desc)
			rendered = lipgloss.NewStyle().
				Width(colWidth-1).
				Padding(0, 1).
				Render(fmt.Sprintf("  %s %s %s\n     %s", act.icon, keyBadge, nameStr, descStr))
		}

		currentRow = append(currentRow, rendered)

		if len(currentRow) == cols || i == len(actions)-1 {
			allRendered = append(allRendered, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
			currentRow = nil
		}
	}

	return strings.Join(allRendered, "\n")
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// sortedVmKeys returns map keys sorted alphabetically for stable iteration.
func sortedVmKeys(vms map[string]*vm.Vm) []string {
	keys := make([]string, 0, len(vms))
	for k := range vms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeys returns sorted keys of a generic string map.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// firstKey returns the first (alphabetically) key in a string map, or "".
func firstKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return keys[0]
}

func statusColored(s vm.VmStatus) string {
	switch s {
	case vm.VmStatusRunning:
		return lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render(s.Display())
	case vm.VmStatusStopped:
		return lipgloss.NewStyle().Foreground(colorTextDim).Render(s.Display())
	case vm.VmStatusCreating:
		return lipgloss.NewStyle().Foreground(colorOrange).Render(s.Display())
	case vm.VmStatusError:
		return lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(s.Display())
	default:
		return s.Display()
	}
}

func statusBadgeOutline(s vm.VmStatus, bracket lipgloss.Style) string {
	switch s {
	case vm.VmStatusRunning:
		return bracket.Render("[") +
			lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render(" ● RUNNING ") +
			bracket.Render("]")
	case vm.VmStatusStopped:
		return bracket.Render("[") +
			lipgloss.NewStyle().Foreground(colorTextDim).Render(" ○ STOPPED ") +
			bracket.Render("]")
	case vm.VmStatusCreating:
		return bracket.Render("[") +
			lipgloss.NewStyle().Foreground(colorOrange).Bold(true).Render(" ◌ CREATING ") +
			bracket.Render("]")
	case vm.VmStatusError:
		return bracket.Render("[") +
			lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(" ✗ ERROR ") +
			bracket.Render("]")
	default:
		return bracket.Render("[") + s.Display() + bracket.Render("]")
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

// ─── Entry Point ─────────────────────────────────────────────────────────────

func Run(mgr *vm.VmManager) error {
	p := tea.NewProgram(NewModel(mgr), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
