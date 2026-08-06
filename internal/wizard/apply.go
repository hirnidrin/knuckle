package wizard

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/projectbluefin/knuckle/internal/model"
)

// UserStepInput is the raw collected input from the User step form.
// LocalKeys are SSH public keys already discovered on the installer host
// (the TUI calls detectLocalSSHKeys()); the wizard merges them with the
// user-pasted ManualKey and applies the result to InstallConfig.
type UserStepInput struct {
	Username     string
	Password     string // plaintext; bcrypted inside ApplyUserStep
	ManualKey    string // semicolon-separated paste from the SSH Public Key input
	LocalKeys    []string
	Hostname     string
	Timezone     string
	GroupsForNew []string // groups applied if the user record is being created
}

// NetworkStepInput is the raw collected input from the Network step form.
type NetworkStepInput struct {
	Mode string // "static" or "dhcp" (anything else ⇒ DHCP)
	DNS  string // comma-separated DNS list from the form
}

// ApplyNetworkStep mutates Config.Network from raw form input.
// Pure state transition — no I/O, no validation (validation happens via
// ValidateCurrentStep at step-transition time).
func (w *Wizard) ApplyNetworkStep(in NetworkStepInput) {
	cfg := &w.State.Config
	if dns := strings.TrimSpace(in.DNS); dns != "" {
		parts := strings.Split(dns, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		cfg.Network.DNS = out
	} else {
		cfg.Network.DNS = nil
	}
	if in.Mode == "static" {
		cfg.Network.Mode = model.NetworkStatic
	} else {
		cfg.Network.Mode = model.NetworkDHCP
	}
}

// ApplyUserStep mutates Config.{Users,SSHKeys,Hostname,Timezone} from raw
// form input. Hashes the password if set. Returns an error only on bcrypt
// failure (e.g. password > 72 bytes).
//
// LocalKeys + ManualKey are merged (deduped, order preserved). Async GitHub
// keys are applied later via ApplyGitHubKeys once the fetch returns.
func (w *Wizard) ApplyUserStep(in UserStepInput) error {
	cfg := &w.State.Config

	if in.Hostname != "" {
		cfg.Hostname = in.Hostname
	}
	if in.Timezone != "" {
		cfg.Timezone = in.Timezone
	} else if cfg.Timezone == "" {
		cfg.Timezone = "UTC"
	}

	if in.Username != "" {
		groups := in.GroupsForNew
		if len(groups) == 0 {
			groups = []string{"sudo", "docker"}
		}
		if len(cfg.Users) == 0 {
			cfg.Users = append(cfg.Users, model.UserConfig{
				Username: in.Username,
				Groups:   groups,
			})
		} else {
			cfg.Users[0].Username = in.Username
		}
	}

	if in.Password != "" && len(cfg.Users) > 0 {
		hash, err := HashPassword(in.Password)
		if err != nil {
			return err
		}
		cfg.Users[0].PasswordHash = hash
	}

	manual := SplitSSHKeys(in.ManualKey)
	merged := MergeSSHKeys(in.LocalKeys, manual)
	if len(merged) > 0 {
		cfg.SSHKeys = merged
		if len(cfg.Users) > 0 {
			cfg.Users[0].SSHKeys = merged
		}
	}
	return nil
}

// ApplyGitHubKeys merges asynchronously-fetched GitHub keys with the local
// host keys and the user's manual paste, replacing Config.SSHKeys with the
// deduped union.
func (w *Wizard) ApplyGitHubKeys(githubKeys []string, localKeys []string, manualKey string) {
	cfg := &w.State.Config
	merged := MergeSSHKeys(localKeys, SplitSSHKeys(manualKey), githubKeys)
	cfg.SSHKeys = merged
	if len(cfg.Users) > 0 {
		cfg.Users[0].SSHKeys = merged
	}
}

// ApplySysexts installs a freshly fetched catalog as the current one, carrying
// the user's selections across by name so a mid-wizard refresh does not clear
// the checkboxes. It returns the names of previously selected extensions that
// are no longer in the catalog (sorted), so the caller can say so rather than
// dropping them silently.
//
// Config.Sysexts is re-pointed at the new slice: State.Sysexts and
// Config.Sysexts must alias the same backing array, or the review screen and
// generated Butane render pre-refresh data.
func (w *Wizard) ApplySysexts(entries []model.SysextEntry) []string {
	selected := make(map[string]bool)
	for _, e := range w.State.Sysexts {
		if e.Selected {
			selected[e.Name] = true
		}
	}

	for i := range entries {
		if selected[entries[i].Name] {
			entries[i].Selected = true
			delete(selected, entries[i].Name)
		}
	}

	w.State.Sysexts = entries
	w.State.Config.Sysexts = entries
	w.State.SysextErr = nil
	w.autoSelectNvidia()

	dropped := make([]string, 0, len(selected))
	for name := range selected {
		dropped = append(dropped, name)
	}
	sort.Strings(dropped)
	return dropped
}

// HasAnyAuthentication reports whether the current Config has at least one
// authentication method (SSH key or user password hash) set. Used by the
// User step to gate the post-fetch advance when GitHub returns 0 keys.
func (w *Wizard) HasAnyAuthentication() bool {
	cfg := &w.State.Config
	if len(cfg.SSHKeys) > 0 {
		return true
	}
	for _, u := range cfg.Users {
		if u.PasswordHash != "" {
			return true
		}
	}
	return false
}

// HashPassword generates a bcrypt hash suitable for the Ignition `passwd`
// field. Fails when the password exceeds bcrypt's 72-byte limit.
func HashPassword(plain string) (string, error) {
	if len(plain) > 72 {
		return "", fmt.Errorf("password too long (max 72 bytes for bcrypt)")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

// SplitSSHKeys splits a semicolon-separated SSH key paste and trims each.
func SplitSSHKeys(input string) []string {
	parts := strings.Split(input, ";")
	keys := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			keys = append(keys, p)
		}
	}
	return keys
}

// MergeSSHKeys merges multiple key lists, dedupes by exact string match, and
// preserves first-seen order.
func MergeSSHKeys(lists ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, list := range lists {
		for _, k := range list {
			if k == "" {
				continue
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}
