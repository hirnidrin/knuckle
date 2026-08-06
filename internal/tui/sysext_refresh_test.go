package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/projectbluefin/knuckle/internal/bakery"
	"github.com/projectbluefin/knuckle/internal/model"
	"github.com/projectbluefin/knuckle/internal/wizard"
)

// stubBakery is a bakery.Client that returns canned results without network.
type stubBakery struct {
	entries []model.SysextEntry
	err     error
	calls   int
}

func (s *stubBakery) FetchCatalog(ctx context.Context) ([]model.SysextEntry, error) {
	return s.FetchCatalogArch(ctx, "amd64")
}

func (s *stubBakery) FetchCatalogArch(_ context.Context, _ string) ([]model.SysextEntry, error) {
	s.calls++
	return s.entries, s.err
}

// sysextModel builds a Model parked on the Sysext step with the given catalog.
func sysextModel(client bakery.Client, entries ...model.SysextEntry) *Model {
	w := wizard.New(nil, client, nil)
	w.State.CurrentStep = model.StepSysext
	w.State.Sysexts = entries
	w.State.Config.Sysexts = entries
	m := New(w)
	m.initSysextList()
	return m
}

func entry(name string, selected bool) model.SysextEntry {
	return model.SysextEntry{Name: name, Selected: selected}
}

func TestCtrlR_StartsRefresh(t *testing.T) {
	stub := &stubBakery{entries: []model.SysextEntry{entry("docker", false)}}
	m := sysextModel(stub, entry("docker", false))

	updated, cmd := m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	got := updated.(*Model)

	if !got.refreshing {
		t.Fatal("ctrl+r on the Sysext step should mark a refresh in flight")
	}
	if cmd == nil {
		t.Fatal("ctrl+r should return a fetch command")
	}
	msg, ok := cmd().(refreshSysextMsg)
	if !ok {
		t.Fatalf("expected refreshSysextMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Errorf("unexpected error: %v", msg.err)
	}
	if len(msg.entries) != 1 || msg.entries[0].Name != "docker" {
		t.Errorf("unexpected entries: %v", msg.entries)
	}
}

// TestCtrlR_IgnoredWhileInFlight: each refresh costs several GitHub API
// requests against a 60/hour budget, so a key-mash must not fan out.
func TestCtrlR_IgnoredWhileInFlight(t *testing.T) {
	stub := &stubBakery{}
	m := sysextModel(stub, entry("docker", false))
	m.refreshing = true

	_, cmd := m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Error("a second ctrl+r while refreshing must not start another fetch")
	}
	if stub.calls != 0 {
		t.Errorf("bakery called %d times, want 0", stub.calls)
	}
}

func TestCtrlR_IgnoredOffSysextStep(t *testing.T) {
	stub := &stubBakery{}
	m := sysextModel(stub, entry("docker", false))
	m.Wizard.State.CurrentStep = model.StepReview

	updated, cmd := m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Error("ctrl+r outside the Sysext step must be inert")
	}
	if updated.(*Model).refreshing {
		t.Error("ctrl+r outside the Sysext step must not mark a refresh")
	}
}

func TestRefreshSysexts_NoClient(t *testing.T) {
	m := sysextModel(nil)

	msg, ok := m.refreshSysexts()().(refreshSysextMsg)
	if !ok {
		t.Fatal("expected refreshSysextMsg")
	}
	if msg.err == nil {
		t.Error("a nil catalog client should surface as an error, not a panic")
	}
}

func TestRefreshSysexts_PropagatesError(t *testing.T) {
	wantErr := &bakery.RateLimitError{Resets: time.Unix(1786035515, 0)}
	m := sysextModel(&stubBakery{err: wantErr})

	msg := m.refreshSysexts()().(refreshSysextMsg)
	var rl *bakery.RateLimitError
	if !errors.As(msg.err, &rl) {
		t.Fatalf("expected the bakery error to survive, got %v", msg.err)
	}
}

// TestRefreshMsg_AppliesCatalog covers the success path end to end: selections
// carried across, list rebuilt, error cleared.
func TestRefreshMsg_AppliesCatalog(t *testing.T) {
	m := sysextModel(nil, entry("docker", true), entry("cilium", false))
	m.refreshing = true
	m.err = errors.New("stale error")

	updated, _ := m.Update(refreshSysextMsg{entries: []model.SysextEntry{
		{Name: "docker", Version: "28.0.0"},
		{Name: "cilium", Version: "1.18.0"},
	}})
	got := updated.(*Model)

	if got.refreshing {
		t.Error("refreshing should be cleared when the result arrives")
	}
	if got.err != nil {
		t.Errorf("a clean refresh should clear the error, got %v", got.err)
	}
	if !got.Wizard.State.Sysexts[0].Selected {
		t.Error("docker should still be selected after the refresh")
	}
	if got.Wizard.State.Sysexts[0].Version != "28.0.0" {
		t.Error("the refreshed version should be in state")
	}
	if !got.sysextListReady {
		t.Error("the list should have been rebuilt")
	}
}

func TestRefreshMsg_ReportsDropped(t *testing.T) {
	m := sysextModel(nil, entry("zfs", true), entry("docker", true))
	m.refreshing = true

	updated, _ := m.Update(refreshSysextMsg{entries: []model.SysextEntry{{Name: "docker"}}})
	got := updated.(*Model)

	if got.err == nil || !strings.Contains(got.err.Error(), "zfs") {
		t.Errorf("expected the dropped extension to be named, got %v", got.err)
	}
}

// TestRefreshMsg_KeepsCatalogOnError: a failed retry must not throw away a list
// the user is midway through selecting.
func TestRefreshMsg_KeepsCatalogOnError(t *testing.T) {
	m := sysextModel(nil, entry("docker", true))
	m.refreshing = true

	rl := &bakery.RateLimitError{Resets: time.Unix(1786035515, 0)}
	updated, _ := m.Update(refreshSysextMsg{err: rl})
	got := updated.(*Model)

	if got.refreshing {
		t.Error("refreshing should be cleared even on failure")
	}
	if len(got.Wizard.State.Sysexts) != 1 || !got.Wizard.State.Sysexts[0].Selected {
		t.Error("a failed refresh must leave the existing selection alone")
	}
	if !errors.Is(got.Wizard.State.SysextErr, rl) {
		t.Errorf("SysextErr = %v, want the fetch error", got.Wizard.State.SysextErr)
	}
}

// TestViewSysextEmpty_ExplainsRateLimit is the payoff for the original bug
// report: the empty screen has to name the cause and the way out.
func TestViewSysextEmpty_ExplainsRateLimit(t *testing.T) {
	m := sysextModel(nil)
	m.Wizard.State.SysextErr = &bakery.RateLimitError{Resets: time.Unix(1786035515, 0)}

	out := m.viewSysext()
	for _, want := range []string{"No extensions available", "rate limit", "GITHUB_TOKEN", "ctrl+r"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty state missing %q in:\n%s", want, out)
		}
	}
}

func TestViewSysextEmpty_GenericError(t *testing.T) {
	m := sysextModel(nil)
	m.Wizard.State.SysextErr = errors.New("dial tcp: lookup api.github.com: no such host")

	out := m.viewSysext()
	if !strings.Contains(out, "no such host") {
		t.Errorf("empty state should carry the underlying error, got:\n%s", out)
	}
}

func TestViewSysextEmpty_NoErrorRecorded(t *testing.T) {
	m := sysextModel(nil)

	out := m.viewSysext()
	if !strings.Contains(out, "ctrl+r") {
		t.Errorf("empty state should still offer a retry, got:\n%s", out)
	}
}

func TestViewSysext_RefreshingIndicator(t *testing.T) {
	m := sysextModel(nil)
	m.refreshing = true
	if !strings.Contains(m.viewSysext(), "retrying") {
		t.Error("an in-flight retry should be visible on the empty screen")
	}

	m2 := sysextModel(nil, entry("docker", false))
	m2.refreshing = true
	if !strings.Contains(m2.viewSysext(), "Refreshing catalog") {
		t.Error("an in-flight refresh should be visible above the list")
	}
}

func TestSysextHelpMentionsRefresh(t *testing.T) {
	m := sysextModel(nil, entry("docker", false))
	if !strings.Contains(m.render(), "ctrl+r refresh") {
		t.Error("the Sysext footer should advertise ctrl+r")
	}

	m.Wizard.State.CurrentStep = model.StepStorage
	if strings.Contains(m.render(), "ctrl+r refresh") {
		t.Error("other steps should not advertise a refresh they do not have")
	}
}
