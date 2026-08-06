package tui

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/projectbluefin/knuckle/internal/bakery"
	"github.com/projectbluefin/knuckle/internal/model"
)

func TestInitSysextList_ZeroOneManyEntries(t *testing.T) {
	tests := []struct {
		name       string
		sysexts    []model.SysextEntry
		cursor     int
		wantReady  bool
		wantTitle  string
		wantOrder  []int
		wantCursor int
	}{
		{
			name:      "zero entries",
			wantReady: false,
		},
		{
			name: "single entry",
			sysexts: []model.SysextEntry{
				{Name: "docker", SupportTier: bakery.TierIntegrated},
			},
			wantReady:  true,
			wantTitle:  "0 selected",
			wantOrder:  []int{0},
			wantCursor: 0,
		},
		{
			name: "many entries sort by tier and keep cursor",
			sysexts: []model.SysextEntry{
				{Name: "other", SupportTier: "", Selected: false},
				{Name: "maintained", SupportTier: bakery.TierMaintained, Selected: true},
				{Name: "integrated", SupportTier: bakery.TierIntegrated, Selected: false},
				{Name: "experimental", SupportTier: bakery.TierExperimental, Selected: true},
			},
			cursor:     2,
			wantReady:  true,
			wantTitle:  "2 selected",
			wantOrder:  []int{2, 1, 3, 0},
			wantCursor: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newTestWizard()
			w.State.Sysexts = tt.sysexts
			m := New(w)
			m.cursor = tt.cursor
			m.initSysextList()

			if m.sysextListReady != tt.wantReady {
				t.Fatalf("sysextListReady = %v, want %v", m.sysextListReady, tt.wantReady)
			}
			if !tt.wantReady {
				return
			}
			if !strings.Contains(m.sysextList.Title, tt.wantTitle) {
				t.Fatalf("Title = %q, want substring %q", m.sysextList.Title, tt.wantTitle)
			}
			if got := m.sysextList.Index(); got != tt.wantCursor {
				t.Fatalf("Index() = %d, want %d", got, tt.wantCursor)
			}
			items := m.sysextList.Items()
			if len(items) != len(tt.wantOrder) {
				t.Fatalf("len(Items()) = %d, want %d", len(items), len(tt.wantOrder))
			}
			for i, wantIdx := range tt.wantOrder {
				si, ok := items[i].(sysextItem)
				if !ok {
					t.Fatalf("item %d has unexpected type %T", i, items[i])
				}
				if si.idx != wantIdx {
					t.Fatalf("item %d idx = %d, want %d", i, si.idx, wantIdx)
				}
			}
		})
	}
}

func TestSysextDelegate_MethodBehavior(t *testing.T) {
	d := newSysextDelegate(func(idx int) bool { return idx == 1 })
	if d.Height() != 2 {
		t.Fatalf("Height() = %d, want 2", d.Height())
	}
	if d.Spacing() != 0 {
		t.Fatalf("Spacing() = %d, want 0", d.Spacing())
	}
	if cmd := d.Update(nil, nil); cmd != nil {
		t.Fatal("Update() returned unexpected command")
	}

	item := sysextItem{idx: 1, entry: model.SysextEntry{Name: "tailscale", Category: "Network", SupportTier: bakery.TierMaintained}}
	if got := item.FilterValue(); !strings.Contains(got, "tailscale") || !strings.Contains(got, "Network") || !strings.Contains(got, bakery.TierMaintained) {
		t.Fatalf("FilterValue() = %q, want name/category/tier content", got)
	}
}

func TestSysextDelegateRender_ListScenarios(t *testing.T) {
	d := newSysextDelegate(func(idx int) bool { return idx == 0 })
	items := []list.Item{
		sysextItem{idx: 0, entry: model.SysextEntry{Name: "docker", Version: "24.0", Category: "Container", SupportTier: bakery.TierIntegrated}},
		sysextItem{idx: 1, entry: model.SysextEntry{Name: "tailscale", Version: "1.50", Category: "Network", SupportTier: bakery.TierMaintained}},
		sysextItem{idx: 2, entry: model.SysextEntry{Name: "custom", Category: "", SupportTier: ""}},
	}
	l := newTestList(items, d)

	var first bytes.Buffer
	d.Render(&first, l, 0, items[0])
	if out := first.String(); !strings.Contains(out, "[✓]") || !strings.Contains(out, bakery.TierIntegrated) {
		t.Fatalf("first render missing checkmark or header: %q", out)
	}

	var second bytes.Buffer
	d.Render(&second, l, 1, items[1])
	if out := second.String(); !strings.Contains(out, bakery.TierMaintained) || strings.Contains(out, "▸") {
		t.Fatalf("second render missing tier change header or unexpectedly current: %q", out)
	}

	var third bytes.Buffer
	d.Render(&third, l, 2, items[2])
	if out := third.String(); !strings.Contains(out, "Other") {
		t.Fatalf("third render should fall back to Other, got: %q", out)
	}
	var invalid bytes.Buffer
	d.Render(&invalid, l, 0, nonSysextItem{})
	if invalid.Len() != 0 {
		t.Fatalf("invalid item should render nothing, got %q", invalid.String())
	}
}

func TestSysextTitle(t *testing.T) {
	tests := []struct {
		name    string
		sysexts []model.SysextEntry
		want    string
	}{
		{
			name: "no selections",
			sysexts: []model.SysextEntry{
				{Name: "docker"},
				{Name: "tailscale"},
			},
			want: "System Extensions — 0 selected",
		},
		{
			name: "counts selected entries",
			sysexts: []model.SysextEntry{
				{Name: "docker", Selected: true},
				{Name: "tailscale"},
				{Name: "podman", Selected: true},
			},
			want: "System Extensions — 2 selected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newTestWizard()
			w.State.Sysexts = tt.sysexts
			if got := New(w).sysextTitle(); got != tt.want {
				t.Fatalf("sysextTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── layout invariants ────────────────────────────────────────────────────────

// testSysextEntries builds a catalog large enough to overflow any page.
func testSysextEntries() []model.SysextEntry {
	tiers := []string{bakery.TierIntegrated, bakery.TierMaintained, bakery.TierExperimental, ""}
	entries := make([]model.SysextEntry, 0, 24)
	for i := range 24 {
		entries = append(entries, model.SysextEntry{
			Name:        fmt.Sprintf("ext-%02d", i),
			Version:     "1.2.3",
			Category:    "Networking",
			SupportTier: tiers[i%len(tiers)],
			Description: strings.Repeat("long description text ", 20),
		})
	}
	return entries
}

// TestSysextDelegateRender_LineCountMatchesHeight is the invariant the whole
// sysext screen rests on: bubbles/list sizes pages from Height() and appends
// the separator newline itself, so an item that renders a different number of
// lines silently pushes the tail of every page off the bottom of the terminal.
func TestSysextDelegateRender_LineCountMatchesHeight(t *testing.T) {
	entries := []model.SysextEntry{
		{Name: "docker", Version: "24.0", Category: "Container", SupportTier: bakery.TierIntegrated},
		{Name: "tailscale", Version: "1.50", Category: "Network", SupportTier: bakery.TierMaintained},
		{Name: "custom", SupportTier: ""},
	}
	items := make([]list.Item, len(entries))
	for i, e := range entries {
		items[i] = sysextItem{idx: i, entry: e}
	}

	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprintf("compact=%v", compact), func(t *testing.T) {
			d := newSysextDelegate(func(idx int) bool { return idx == 0 })
			d.compact = compact
			l := newTestList(items, d)

			for i := range items {
				var buf bytes.Buffer
				d.Render(&buf, l, i, items[i])
				got := strings.Count(buf.String(), "\n") + 1
				if got != d.Height() {
					t.Fatalf("item %d rendered %d lines, want Height() = %d: %q",
						i, got, d.Height(), buf.String())
				}
				if strings.HasSuffix(buf.String(), "\n") {
					t.Fatalf("item %d ends with a newline; the list adds the separator itself", i)
				}
			}
		})
	}
}

// TestViewSysext_FitsTerminalHeight covers the reported failure: on an 80x25
// IPMI/iKVM console the screen ran off the bottom, hiding the status line and
// stranding entries that the cursor could never reach.
func TestViewSysext_FitsTerminalHeight(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 24}, {80, 25}, {80, 30}, {80, 45}, {100, 34}, {100, 40}, {120, 50}, {200, 60},
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			for _, withErr := range []bool{false, true} {
				w := newTestWizard()
				w.State.CurrentStep = model.StepSysext
				w.State.Sysexts = testSysextEntries()
				w.State.NvidiaGPUDetected = withErr
				m := New(w)
				m.width, m.height = size.w, size.h
				if withErr {
					m.err = errors.New(strings.Repeat("catalog refresh failed ", 6))
				}
				m.resizeSysextList()

				rows := 0
				for _, line := range strings.Split(m.render(), "\n") {
					rows += max(1, (lipgloss.Width(line)+size.w-1)/size.w)
				}
				if rows > size.h {
					t.Fatalf("err=%v: rendered %d rows, terminal has %d", withErr, rows, size.h)
				}
			}
		})
	}
}

// TestSysextListPagination_ReachesEveryEntry walks the cursor from the first
// entry to the last and requires each one to actually appear on screen — the
// symptom in the bug report was entries that scrolled past between pages.
func TestSysextListPagination_ReachesEveryEntry(t *testing.T) {
	w := newTestWizard()
	w.State.CurrentStep = model.StepSysext
	w.State.Sysexts = testSysextEntries()
	m := New(w)
	m.width, m.height = 80, 25
	m.resizeSysextList()

	for i := range w.State.Sysexts {
		m.sysextList.Select(m.sysextListLookup(i))
		m.cursor = i
		if name := w.State.Sysexts[i].Name; !strings.Contains(m.render(), name) {
			t.Fatalf("entry %d (%s) is not visible when it is the cursor item", i, name)
		}
	}
}

// TestResizeSysextList_SwitchesDensity verifies that a WindowSizeMsg re-fits an
// already-built list rather than leaving it sized for the previous terminal.
func TestResizeSysextList_SwitchesDensity(t *testing.T) {
	w := newTestWizard()
	w.State.CurrentStep = model.StepSysext
	w.State.Sysexts = testSysextEntries()
	m := New(w)

	m.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	if !m.sysextListCompact {
		t.Fatal("80x25 should use the compact one-line layout")
	}
	if got := m.sysextList.Height(); got != m.sysextListHeight() {
		t.Fatalf("list height = %d, want %d after resize", got, m.sysextListHeight())
	}

	m.Update(tea.WindowSizeMsg{Width: 160, Height: 60})
	if m.sysextListCompact {
		t.Fatal("160x60 should use the roomy two-line layout")
	}
	if got := m.sysextList.Height(); got != m.sysextListHeight() {
		t.Fatalf("list height = %d, want %d after resize", got, m.sysextListHeight())
	}
	if got := m.sysextList.Width(); got != 160 {
		t.Fatalf("list width = %d, want 160 after resize", got)
	}
}
