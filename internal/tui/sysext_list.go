package tui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/projectbluefin/knuckle/internal/bakery"
	"github.com/projectbluefin/knuckle/internal/model"
)

// sysextItem wraps a SysextEntry for the bubbles/list interface.
// idx is the index into Wizard.State.Sysexts (stable identity for toggle).
type sysextItem struct {
	idx   int
	entry model.SysextEntry
}

func (i sysextItem) FilterValue() string {
	return i.entry.Name + " " + i.entry.Category + " " + i.entry.SupportTier
}

// sysextDelegate renders sysext items with checkboxes and tier coloring.
//
// Render must emit exactly Height() lines for every item. bubbles/list derives
// items-per-page from the declared height and pads (never truncates) the
// rendered block, so an item that prints more lines than it declares pushes the
// whole list past the bottom of the terminal and leaves the last items of each
// page permanently off screen.
type sysextDelegate struct {
	isSelected func(idx int) bool
	// compact renders one line per item instead of two, for terminals too
	// short to afford a second line (serial consoles, IPMI/iKVM, 80x25).
	compact bool
}

func newSysextDelegate(isSelected func(idx int) bool) sysextDelegate {
	return sysextDelegate{isSelected: isSelected}
}

func (d sysextDelegate) Height() int {
	if d.compact {
		return 1
	}
	return 2
}
func (d sysextDelegate) Spacing() int                            { return 0 }
func (d sysextDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// sysextTierStyle maps a support tier to its badge style.
func sysextTierStyle(tier string) lipgloss.Style {
	color := "252"
	switch tier {
	case bakery.TierIntegrated:
		color = "76" // green
	case bakery.TierMaintained:
		color = "75" // blue
	case bakery.TierExperimental:
		color = "214" // yellow
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

func (d sysextDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(sysextItem)
	if !ok {
		return
	}

	// Checkbox state.
	check := "[ ]"
	if d.isSelected(item.idx) {
		check = "[✓]"
	}

	tierStyle := sysextTierStyle(item.entry.SupportTier)

	// Build version + category + tier description.
	version := item.entry.Version
	if version != "" {
		version = "v" + version
	}
	cat := item.entry.Category
	if cat == "" {
		cat = "Other"
	}
	tier := item.entry.SupportTier
	if tier == "" {
		tier = "Other"
	}

	isCurrent := index == m.Index()
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	prefix := "    "
	rowStyle := dimStyle
	if isCurrent {
		prefix = "  ▸ "
		rowStyle = selectedStyle
	}

	// Rows must not wrap: a wrapped row costs a line the list did not budget
	// for, which is the same overflow as rendering more lines than Height().
	width := m.Width()
	if width <= 0 {
		width = 80
	}
	budget := width - lipgloss.Width(prefix)

	// No trailing newline on the final line: bubbles/list writes the separator
	// between items itself, so ending with one would make every item occupy
	// Height()+1 rows and push the tail of each page off the screen.
	if d.compact {
		// One line: cursor + checkbox + name + version + category + tier. The
		// tier only gets a column when there is room left for a usable row.
		suffix := ""
		if rest := budget - lipgloss.Width(tier) - 1; rest >= 44 {
			suffix = " " + tierStyle.Render(tier)
			budget = rest
		}
		row := fmt.Sprintf("%s %-18s %-14s %s", check, item.entry.Name, version, cat)
		_, _ = fmt.Fprintf(w, "%s%s%s", prefix, rowStyle.Render(fitCell(row, budget)), suffix)
		return
	}

	// Title line: cursor + checkbox + name + version + category
	title := fmt.Sprintf("%s %-22s %-14s  %s", check, item.entry.Name, version, cat)
	_, _ = fmt.Fprintf(w, "%s%s\n", prefix, rowStyle.Render(fitCell(title, budget)))
	_, _ = fmt.Fprintf(w, "      %s", tierStyle.Render(fitCell(tier, width-6)))
}

// fitCell pads or truncates plain (unstyled) text to exactly width columns.
func fitCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) > width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

// initSysextList builds and configures the bubbles/list model for sysext selection.
func (m *Model) initSysextList() {
	sysexts := m.Wizard.State.Sysexts
	if len(sysexts) == 0 {
		m.sysextListReady = false
		return
	}

	// Build items sorted by tier (matching original order).
	tierOrder := []string{bakery.TierIntegrated, bakery.TierMaintained, bakery.TierExperimental, ""}
	tierMap := map[string][]int{}
	for i, ext := range sysexts {
		tierMap[ext.SupportTier] = append(tierMap[ext.SupportTier], i)
	}

	var items []list.Item
	for _, tier := range tierOrder {
		for _, idx := range tierMap[tier] {
			items = append(items, sysextItem{idx: idx, entry: sysexts[idx]})
		}
	}

	compact := m.sysextCompact()
	delegate := m.newSysextListDelegate(compact)

	l := list.New(items, delegate, m.sysextListWidth(), m.sysextListHeight())
	l.Title = m.sysextTitle()
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
	l.Styles.Title = lipgloss.NewStyle().Foreground(lipgloss.Color("170")).Bold(true).MarginLeft(2)

	// Disable keys that conflict with parent model.
	l.KeyMap.ForceQuit.SetEnabled(false)

	m.sysextList = l
	m.sysextListCompact = !compact // force applySysextListDensity to do the work
	m.sysextListReady = true
	m.applySysextListDensity(compact)

	// Sync cursor position.
	if m.cursor > 0 && m.cursor < len(items) {
		m.sysextList.Select(m.cursor)
	}
}

// newSysextListDelegate builds a delegate bound to the current selection state.
func (m *Model) newSysextListDelegate(compact bool) sysextDelegate {
	d := newSysextDelegate(func(idx int) bool {
		return m.Wizard.State.Sysexts[idx].Selected
	})
	d.compact = compact
	return d
}

// sysextTermHeight is the terminal height to size the sysext screen against.
// Before the first WindowSizeMsg the size is unknown, so assume a roomy
// terminal rather than guessing small and cramping the list.
func (m *Model) sysextTermHeight() int {
	if m.height <= 0 {
		return 40
	}
	return m.height
}

// sysextCompact reports whether the terminal is too short for two-line rows.
func (m *Model) sysextCompact() bool {
	return m.sysextTermHeight() < 34
}

// sysextListWidth is the width to hand the list model.
func (m *Model) sysextListWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

// wrappedRows is how many terminal rows s takes at the given width.
func wrappedRows(s string, width int) int {
	if width <= 0 {
		return 1
	}
	return max(1, (lipgloss.Width(s)+width-1)/width)
}

// sysextLayout divides the terminal between the list and the detail panel,
// after subtracting everything else the sysext screen draws: wizard chrome,
// notices, an error message and the footer help. Sizing to the space that
// actually exists is what keeps the pagination indicator and the footer on
// screen, and what makes every entry reachable with the arrow keys.
//
// The list has first call on the space. The panel takes a slice of what is
// left and disappears entirely rather than squeeze the list down to a couple
// of rows — the catalog is what the user came here to read.
func (m *Model) sysextLayout() (listHeight, panelLines int) {
	const (
		minList     = 6 // list title + pagination + a few entries
		minPanel    = 5 // border rows plus the three metadata rows
		maxPanel    = 9
		panelBorder = 2
	)

	h := m.sysextTermHeight()
	width := m.sysextListWidth()

	// Wizard chrome, counting the blank line render() writes after it.
	reserved := lipgloss.Height(m.buildBreadcrumb())
	if m.Wizard.State.NvidiaGPUDetected {
		reserved += 1 + wrappedRows("  "+nvidiaSysextNotice, width)
	}
	if m.refreshing {
		reserved += 2
	}
	if m.err != nil {
		// Blank line plus however many rows the message wraps to.
		reserved += 1 + wrappedRows("\u26a0 "+m.err.Error(), width)
	}
	// Blank line, helpStyle's top margin, and the footer help itself.
	reserved += 2 + wrappedRows(m.sysextHelp(), width)
	// One line of slack: under-filling is invisible, overflowing scrolls the
	// top of the screen away on a console that cannot be scrolled back.
	reserved++

	avail := h - reserved

	panelLines = min(maxPanel, h/5)
	if width < 60 || m.sysextFiltering() {
		// Too narrow to draw, or the user is typing a filter and wants rows.
		panelLines = 0
	}
	if panelLines > 0 {
		panelLines = min(panelLines, avail-panelBorder-minList)
		if panelLines < minPanel {
			panelLines = 0
		}
	}

	panelRows := 0
	if panelLines > 0 {
		panelRows = panelLines + panelBorder
	}
	return max(3, avail-panelRows), panelLines
}

// sysextFiltering reports whether the list is currently taking filter input.
func (m *Model) sysextFiltering() bool {
	return m.sysextListReady && m.sysextList.FilterState() != list.Unfiltered
}

// sysextListHeight is the number of terminal lines the list may occupy.
func (m *Model) sysextListHeight() int {
	h, _ := m.sysextLayout()
	return h
}

// applySysextListDensity switches the list between the roomy two-line layout
// and the one-line layout for short terminals. In compact mode the blank
// padding row under the title goes too — on an 80x25 console that row is an
// entry the user could otherwise see.
func (m *Model) applySysextListDensity(compact bool) {
	if compact == m.sysextListCompact {
		return
	}
	m.sysextListCompact = compact
	m.sysextList.SetDelegate(m.newSysextListDelegate(compact))

	defaults := list.DefaultStyles(true)
	if compact {
		m.sysextList.Styles.TitleBar = defaults.TitleBar.Padding(0, 0, 0, 2)
	} else {
		m.sysextList.Styles.TitleBar = defaults.TitleBar
	}
}

// resizeSysextList re-fits the list to the current terminal size, swapping the
// item delegate when the terminal crosses the compact threshold.
func (m *Model) resizeSysextList() {
	if !m.sysextListReady {
		return
	}
	m.applySysextListDensity(m.sysextCompact())
	// The list's own status bar and help line duplicate the wizard's footer
	// help, so they only earn their four lines while filtering — that is when
	// the match count and the apply/cancel hints are worth having.
	filtering := m.sysextList.FilterState() != list.Unfiltered
	m.sysextList.SetShowStatusBar(filtering)
	m.sysextList.SetShowHelp(filtering)
	m.sysextList.SetSize(m.sysextListWidth(), m.sysextListHeight())
}

// sysextTitle returns the title string with selected count.
func (m *Model) sysextTitle() string {
	selectedCount := 0
	for _, ext := range m.Wizard.State.Sysexts {
		if ext.Selected {
			selectedCount++
		}
	}
	return fmt.Sprintf("System Extensions — %d selected", selectedCount)
}

// sysextListCursorIdx returns the Wizard.State.Sysexts index for the currently
// highlighted item in the list, accounting for filtering.
func (m *Model) sysextListCursorIdx() int {
	if !m.sysextListReady {
		return m.cursor
	}
	item, ok := m.sysextList.SelectedItem().(sysextItem)
	if !ok {
		return m.cursor
	}
	return item.idx
}

// buildSysextItemsFromState rebuilds the list items to reflect current state
// (e.g., after toggling). This preserves the cursor position.
func (m *Model) refreshSysextListTitle() {
	if m.sysextListReady {
		m.sysextList.Title = m.sysextTitle()
	}
}

// sysextListLookup returns the list-internal index for a given Sysexts[] index.
func (m *Model) sysextListLookup(sysextIdx int) int {
	for i, item := range m.sysextList.Items() {
		if si, ok := item.(sysextItem); ok && si.idx == sysextIdx {
			return i
		}
	}
	return 0
}
