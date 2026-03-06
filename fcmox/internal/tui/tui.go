package tui

import (
	"fmt"
	"strconv"
	"strings"

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
	{key: "d", name: "Delete", desc: "Remove selected VM", icon: "✕"},
	{key: "s", name: "Start", desc: "Boot selected VM", icon: "▶"},
	{key: "p", name: "Stop", desc: "Shutdown selected VM", icon: "■"},
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

const (
	createFieldCpu = iota
	createFieldMem
	createFieldCount
)

// ─── Model ───────────────────────────────────────────────────────────────────

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

	// Create form fields
	createInputs   []textinput.Model
	createFocusIdx int
	createError    string
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
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle window resize in any mode
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}

	switch m.mode {
	case modeCreateForm:
		return m.updateCreateForm(msg)
	default:
		if msg, ok := msg.(tea.KeyMsg); ok {
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

// ─── Create Form Mode ────────────────────────────────────────────────────────

func (m Model) enterCreateForm() (Model, tea.Cmd) {
	m.mode = modeCreateForm
	m.createError = ""
	m.createFocusIdx = createFieldCpu

	// Reset inputs
	for i := range m.createInputs {
		m.createInputs[i].SetValue("")
		m.createInputs[i].Blur()
	}
	m.createInputs[createFieldCpu].Focus()

	return m, m.createInputs[createFieldCpu].Cursor.BlinkCmd()
}

func (m Model) updateCreateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "esc":
			// Cancel create form
			m.mode = modeNormal
			m.message = "Create cancelled"
			m.messageIsError = false
			return m, nil

		case "tab", "down":
			// Move to next field
			m.createFocusIdx = (m.createFocusIdx + 1) % createFieldCount
			return m.updateCreateFocus()

		case "shift+tab", "up":
			// Move to previous field
			m.createFocusIdx = (m.createFocusIdx - 1 + createFieldCount) % createFieldCount
			return m.updateCreateFocus()

		case "enter":
			// If on last field, submit; otherwise advance
			if m.createFocusIdx == createFieldCount-1 {
				return m.submitCreateForm()
			}
			m.createFocusIdx++
			return m.updateCreateFocus()
		}
	}

	// Update the focused text input
	var cmd tea.Cmd
	m.createInputs[m.createFocusIdx], cmd = m.createInputs[m.createFocusIdx].Update(msg)
	return m, cmd
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

	// Create the VM
	created := m.mgr.CreateVm(cpus, memMB)

	// Return to normal mode with success message
	m.mode = modeNormal
	m.message = fmt.Sprintf("✓ Created %s  (%d vCPU, %d MB, IP: %s, TAP: %s)",
		created.Id, created.VmCpuCount, created.VmMemSize, created.Ip, created.TapDev)
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
			id, err := m.mgr.DeleteVm(m.selectedVm)
			if err != nil {
				m.message = fmt.Sprintf("⚠ %v", err)
				m.messageIsError = true
			} else {
				if m.selectedVm >= m.mgr.VmCount() && m.selectedVm > 0 {
					m.selectedVm--
				}
				m.message = fmt.Sprintf("✓ Deleted %s", id)
				m.messageIsError = false
			}
		}

	case "s":
		err := m.mgr.StartVm(m.selectedVm)
		if err != nil {
			m.message = fmt.Sprintf("⚠ %v", err)
			m.messageIsError = true
		} else {
			m.message = fmt.Sprintf("✓ Started %s", m.mgr.Vms[m.selectedVm].Id)
			m.messageIsError = false
		}

	case "p":
		err := m.mgr.StopVm(m.selectedVm)
		if err != nil {
			m.message = fmt.Sprintf("⚠ %v", err)
			m.messageIsError = true
		} else {
			m.message = fmt.Sprintf("✓ Stopped %s", m.mgr.Vms[m.selectedVm].Id)
			m.messageIsError = false
		}

	case "l":
		v := m.mgr.Vms[m.selectedVm]
		m.message = fmt.Sprintf("→ Opening logs for %s (serial: %s)…", v.Id, v.SockPath)
		m.messageIsError = false

	case "x":
		v := m.mgr.Vms[m.selectedVm]
		m.message = fmt.Sprintf("→ SSH into %s@%s…", v.Id, v.Ip)
		m.messageIsError = false
	}

	return m, nil
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	w := m.width
	if w < 40 {
		w = 120
	}

	var sections []string

	// ── HEADER ──
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
		Render(titleText + spacer + lipgloss.NewStyle().
			Foreground(colorTextDim).Render(subtitle) + "  ")

	sections = append(sections, headerBar)

	// ── TOP SECTION: VM TABLE (left) + DETAILS (right) ──
	tableWidth := (w * 55) / 100
	detailWidth := w - tableWidth - 3
	if tableWidth < 40 {
		tableWidth = 40
		detailWidth = w - tableWidth - 3
	}

	tableBorderColor := colorBorder
	tableTitle := " Virtual Machines "
	if m.focus == focusVmTable {
		tableBorderColor = colorBorderHi
		tableTitle = " ▸ Virtual Machines "
	}

	tableContent := m.renderVmTable(tableWidth - 7)

	tablePanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tableBorderColor).
		Width(tableWidth).
		Padding(0, 1).
		Render(
			lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Render(tableTitle) + "\n" +
				tableContent,
		)

	detailContent := m.renderVmDetail(detailWidth - 4)

	detailPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Width(detailWidth).
		Padding(0, 1).
		Render(
			lipgloss.NewStyle().Foreground(colorAmber).Bold(true).Render(" VM Details ") + "\n" +
				detailContent,
		)

	topSection := lipgloss.JoinHorizontal(lipgloss.Top, tablePanel, " ", detailPanel)
	sections = append(sections, topSection)

	// ── SEPARATOR ──
	sepLeft := lipgloss.NewStyle().Foreground(colorBorder).Render("├")
	sepLine := lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", w-2))
	sepRight := lipgloss.NewStyle().Foreground(colorBorder).Render("┤")
	sections = append(sections, sepLeft+sepLine+sepRight)

	// ── BOTTOM SECTION ──
	if m.mode == modeCreateForm {
		sections = append(sections, m.renderCreateForm(w-2))
	} else {
		actionsBorderColor := colorBorder
		actionsTitle := " Actions "
		if m.focus == focusActions {
			actionsBorderColor = colorBorderHi
			actionsTitle = " ▸ Actions "
		}

		actionsContent := m.renderActions(w - 6)

		actionsPanel := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(actionsBorderColor).
			Width(w-2).
			Padding(0, 1).
			Render(
				lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(actionsTitle) + "\n" +
					actionsContent,
			)

		sections = append(sections, actionsPanel)
	}

	// ── Message ──
	if m.message != "" {
		msgStyle := lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
		if m.messageIsError {
			msgStyle = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
		}
		sections = append(sections, "  "+msgStyle.Render(m.message))
	}

	// ── Status bar ──
	var statusHelpText string
	if m.mode == modeCreateForm {
		statusHelpText = "  tab/↑↓ switch field  enter submit/next  esc cancel"
	} else {
		statusHelpText = "  tab switch  ↑↓ navigate  enter execute  q quit"
	}
	statusLeft := lipgloss.NewStyle().Foreground(colorTextDim).Render(statusHelpText)
	vmCount := fmt.Sprintf("%d VMs ", m.mgr.VmCount())
	runCount := m.mgr.RunningCount()
	statusRight := lipgloss.NewStyle().Foreground(colorGreen).Render(
		fmt.Sprintf("%d running", runCount))
	statusMid := lipgloss.NewStyle().Foreground(colorTextDim).Render(" │ ")

	rightBlock := lipgloss.NewStyle().Foreground(colorAmber).Render(vmCount) + statusMid + statusRight + "  "
	spacerLen := max(0, w-lipgloss.Width(statusLeft)-lipgloss.Width(rightBlock))
	statusBar := statusLeft + strings.Repeat(" ", spacerLen) + rightBlock

	sections = append(sections, statusBar)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// ─── Render: Create Form ─────────────────────────────────────────────────────

func (m Model) renderCreateForm(panelWidth int) string {
	labelStyle := lipgloss.NewStyle().Foreground(colorAmber).Bold(true).Width(14)
	inputBorderFocused := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorderHi).
		Padding(0, 1).
		Width(18)
	inputBorderBlurred := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(18)

	var fields []string

	// CPU field
	cpuLabel := labelStyle.Render("CPU Cores:")
	cpuInput := inputBorderBlurred.Render(m.createInputs[createFieldCpu].View())
	if m.createFocusIdx == createFieldCpu {
		cpuInput = inputBorderFocused.Render(m.createInputs[createFieldCpu].View())
	}
	cpuHint := lipgloss.NewStyle().Foreground(colorTextDim).Render("  (1-32)")
	fields = append(fields, "  "+cpuLabel+cpuInput+cpuHint)

	// Memory field
	memLabel := labelStyle.Render("Memory (MB):")
	memInput := inputBorderBlurred.Render(m.createInputs[createFieldMem].View())
	if m.createFocusIdx == createFieldMem {
		memInput = inputBorderFocused.Render(m.createInputs[createFieldMem].View())
	}
	memHint := lipgloss.NewStyle().Foreground(colorTextDim).Render("  (128-32768)")
	fields = append(fields, "  "+memLabel+memInput+memHint)

	// Error message
	if m.createError != "" {
		fields = append(fields, "  "+lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("⚠ "+m.createError))
	}

	content := strings.Join(fields, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan).
		Width(panelWidth).
		Padding(0, 1).
		Render(
			lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(" ＋ Create New VM ") + "\n\n" +
				content + "\n",
		)
}

// ─── Render: VM Table ────────────────────────────────────────────────────────

func (m Model) renderVmTable(maxWidth int) string {
	if m.mgr.VmCount() == 0 {
		return lipgloss.NewStyle().
			Foreground(colorTextDim).
			Italic(true).
			Padding(1, 2).
			Render("No virtual machines. Press [C] to create one.")
	}

	colId := 7
	colStatus := 12
	colCpu := 5
	colMem := 7
	colIp := 14
	colTap := 7

	hdrStyle := lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	hdr := hdrStyle.Copy().Width(colId).Render("ID") +
		hdrStyle.Copy().Width(colStatus).Render("STATUS") +
		hdrStyle.Copy().Width(colCpu).Render("CPU") +
		hdrStyle.Copy().Width(colMem).Render("MEM") +
		hdrStyle.Copy().Width(colIp).Render("IP") +
		hdrStyle.Copy().Width(colTap).Render("TAP")

	divider := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("─", colId+colStatus+colCpu+colMem+colIp+colTap))

	rows := []string{hdr, divider}

	for i, v := range m.mgr.Vms {
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
			rowStyle.Copy().Width(colId).Render(v.Id) +
			lipgloss.NewStyle().Width(colStatus).Render(statusStr) +
			rowStyle.Copy().Width(colCpu).Render(fmt.Sprintf("%d", v.VmCpuCount)) +
			rowStyle.Copy().Width(colMem).Render(fmt.Sprintf("%dM", v.VmMemSize)) +
			rowStyle.Copy().Width(colIp).Render(v.Ip) +
			rowStyle.Copy().Width(colTap).Render(v.TapDev)

		if isSelected && m.focus == focusVmTable {
			padLen := max(0, maxWidth-lipgloss.Width(row))
			row = row + strings.Repeat(" ", padLen)
		}

		rows = append(rows, row)
	}

	return strings.Join(rows, "\n")
}

// ─── Render: VM Detail ──────────────────────────────────────────────────────

func (m Model) renderVmDetail(maxWidth int) string {
	if m.mgr.VmCount() == 0 {
		return lipgloss.NewStyle().
			Foreground(colorTextDim).
			Italic(true).
			Padding(1, 1).
			Render("Select a VM to\nview details")
	}

	v := m.mgr.Vms[m.selectedVm]

	labelStyle := lipgloss.NewStyle().Foreground(colorTeal).Bold(true).Width(12)
	valueStyle := lipgloss.NewStyle().Foreground(colorText)
	dimValueStyle := lipgloss.NewStyle().Foreground(colorTextDim)

	// Status badge (outline style)
	bracketStyle := lipgloss.NewStyle().Foreground(colorBorder)
	statusBadge := statusBadgeOutline(v.Status, bracketStyle)

	vmTitle := lipgloss.NewStyle().
		Foreground(colorTextBold).
		Bold(true).
		Render(v.Id)

	lines := []string{
		vmTitle + "  " + statusBadge,
		"",
		labelStyle.Render("CPU") + valueStyle.Render(fmt.Sprintf("%d vCPU", v.VmCpuCount)),
		labelStyle.Render("Memory") + valueStyle.Render(fmt.Sprintf("%d MB", v.VmMemSize)),
		labelStyle.Render("IP") + valueStyle.Render(v.Ip),
		labelStyle.Render("MAC") + dimValueStyle.Render(v.MacAddr),
		labelStyle.Render("TAP") + valueStyle.Render(v.TapDev),
		"",
		lipgloss.NewStyle().Foreground(colorPurple).Bold(true).Render("─── Paths ───"),
		labelStyle.Render("Kernel") + dimValueStyle.Render(truncate(v.KernelPath, maxWidth-14)),
		labelStyle.Render("Rootfs") + dimValueStyle.Render(truncate(v.RootfsPath, maxWidth-14)),
		labelStyle.Render("Socket") + dimValueStyle.Render(truncate(v.SockPath, maxWidth-14)),
		"",
		lipgloss.NewStyle().Foreground(colorPurple).Bold(true).Render("─── Boot ────"),
		dimValueStyle.Render(truncate(v.BootArgs, maxWidth-2)),
	}

	return strings.Join(lines, "\n")
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
