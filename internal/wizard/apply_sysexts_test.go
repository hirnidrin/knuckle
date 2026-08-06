package wizard

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/projectbluefin/knuckle/internal/model"
)

func catalog(names ...string) []model.SysextEntry {
	out := make([]model.SysextEntry, 0, len(names))
	for _, n := range names {
		out = append(out, model.SysextEntry{Name: n})
	}
	return out
}

func selectedNames(entries []model.SysextEntry) []string {
	var out []string
	for _, e := range entries {
		if e.Selected {
			out = append(out, e.Name)
		}
	}
	return out
}

// TestApplySysexts_PreservesSelection is the point of the whole refresh path:
// re-fetching mid-wizard must not silently clear the user's checkboxes.
func TestApplySysexts_PreservesSelection(t *testing.T) {
	w := New(nil, nil, nil)
	w.State.Sysexts = []model.SysextEntry{
		{Name: "docker", Selected: true},
		{Name: "cilium"},
		{Name: "tailscale", Selected: true},
	}

	// Fresh catalog: same names, different order, newer versions, no flags set.
	fresh := []model.SysextEntry{
		{Name: "tailscale", Version: "1.102.2"},
		{Name: "docker", Version: "28.0.0"},
		{Name: "cilium", Version: "1.18.0"},
	}

	dropped := w.ApplySysexts(fresh)

	if len(dropped) != 0 {
		t.Errorf("expected nothing dropped, got %v", dropped)
	}
	got := selectedNames(w.State.Sysexts)
	want := []string{"tailscale", "docker"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}
	// The newer versions must be the ones that survive.
	for _, e := range w.State.Sysexts {
		if e.Name == "docker" && e.Version != "28.0.0" {
			t.Errorf("expected the refreshed version, got %q", e.Version)
		}
	}
}

// TestApplySysexts_ReportsDropped covers extensions that were selected but have
// since left the catalog — they must be named, not silently discarded.
func TestApplySysexts_ReportsDropped(t *testing.T) {
	w := New(nil, nil, nil)
	w.State.Sysexts = []model.SysextEntry{
		{Name: "zfs", Selected: true},
		{Name: "docker", Selected: true},
		{Name: "aardvark", Selected: true},
	}

	dropped := w.ApplySysexts(catalog("docker"))

	// Sorted, so the message is stable between refreshes.
	if !reflect.DeepEqual(dropped, []string{"aardvark", "zfs"}) {
		t.Errorf("dropped = %v, want [aardvark zfs]", dropped)
	}
	if got := selectedNames(w.State.Sysexts); !reflect.DeepEqual(got, []string{"docker"}) {
		t.Errorf("selected = %v, want [docker]", got)
	}
}

// TestApplySysexts_SyncsConfig guards the aliasing invariant: State.Sysexts and
// Config.Sysexts must be the same slice, or the review screen and the generated
// Butane render pre-refresh data.
func TestApplySysexts_SyncsConfig(t *testing.T) {
	w := New(nil, nil, nil)
	w.State.Sysexts = catalog("stale")
	w.State.Config.Sysexts = w.State.Sysexts

	w.ApplySysexts(catalog("docker", "cilium"))

	if len(w.State.Config.Sysexts) != 2 {
		t.Fatalf("Config.Sysexts has %d entries, want 2", len(w.State.Config.Sysexts))
	}
	w.State.Sysexts[0].Selected = true
	if !w.State.Config.Sysexts[0].Selected {
		t.Error("Config.Sysexts must alias State.Sysexts after a refresh")
	}
}

func TestApplySysexts_ClearsPriorError(t *testing.T) {
	w := New(nil, nil, nil)
	w.State.SysextErr = errors.New("rate limited")

	w.ApplySysexts(catalog("docker"))

	if w.State.SysextErr != nil {
		t.Errorf("a successful refresh must clear SysextErr, got %v", w.State.SysextErr)
	}
}

// TestApplySysexts_ReAppliesNvidiaAutoSelect covers a refresh on a GPU host:
// the auto-selection has to survive the catalog swap.
func TestApplySysexts_ReAppliesNvidiaAutoSelect(t *testing.T) {
	w := New(nil, nil, nil)
	w.State.NvidiaGPUDetected = true

	w.ApplySysexts(catalog("docker", "nvidia-runtime"))

	if got := selectedNames(w.State.Sysexts); !reflect.DeepEqual(got, []string{"nvidia-runtime"}) {
		t.Errorf("selected = %v, want [nvidia-runtime]", got)
	}
	if w.State.Config.NvidiaDriverVersion != model.DefaultNvidiaDriverSeries {
		t.Errorf("driver series = %q, want the default", w.State.Config.NvidiaDriverVersion)
	}
}

// TestApplySysexts_NvidiaAbsentFromCatalog: a GPU host whose catalog has no
// nvidia-runtime must not get a driver series pinned for nothing.
func TestApplySysexts_NvidiaAbsentFromCatalog(t *testing.T) {
	w := New(nil, nil, nil)
	w.State.NvidiaGPUDetected = true

	w.ApplySysexts(catalog("docker"))

	if len(selectedNames(w.State.Sysexts)) != 0 {
		t.Errorf("expected nothing selected, got %v", selectedNames(w.State.Sysexts))
	}
	if w.State.Config.NvidiaDriverVersion != "" {
		t.Errorf("driver series = %q, want empty", w.State.Config.NvidiaDriverVersion)
	}
}

// TestFetchSysexts_RetainsError: the Sysext step explains an empty list from
// State.SysextErr, so a failed fetch has to record why.
func TestFetchSysexts_RetainsError(t *testing.T) {
	wantErr := errors.New("catalog returned status 403")
	w := New(nil, &mockBakery{err: wantErr}, nil)

	err := w.FetchSysexts(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(w.State.SysextErr, wantErr) {
		t.Errorf("SysextErr = %v, want %v", w.State.SysextErr, wantErr)
	}
}
