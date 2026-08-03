package probe

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/NVIDIA/go-nvlib/pkg/nvpci"

	"github.com/projectbluefin/knuckle/internal/model"
	"github.com/projectbluefin/knuckle/internal/runner"
)

// Prober is the interface for system hardware discovery
type Prober interface {
	ListDisks(ctx context.Context) ([]model.DiskInfo, error)
	ListNetworkInterfaces(ctx context.Context) ([]model.NetworkInterface, error)
}

// SystemProber uses real system commands via the runner
type SystemProber struct {
	Runner runner.Runner
}

func NewSystemProber(r runner.Runner) *SystemProber {
	return &SystemProber{Runner: r}
}

// lsblkOutput matches the JSON output of `lsblk --json --bytes --output NAME,PATH,MODEL,SERIAL,SIZE,TRAN,RM,TYPE,FSTYPE,LABEL,PARTUUID,MOUNTPOINT`
type lsblkOutput struct {
	Blockdevices []lsblkDevice `json:"blockdevices"`
}

type lsblkDevice struct {
	Name       string        `json:"name"`
	Path       string        `json:"path"`
	Model      *string       `json:"model"`
	Serial     *string       `json:"serial"`
	Size       json.Number   `json:"size"`
	Tran       *string       `json:"tran"`
	RM         bool          `json:"rm"`
	Type       string        `json:"type"`
	FSType     *string       `json:"fstype"`
	Label      *string       `json:"label"`
	PartUUID   *string       `json:"partuuid"`
	MountPoint *string       `json:"mountpoint"`
	Children   []lsblkDevice `json:"children,omitempty"`
}

func (p *SystemProber) ListDisks(ctx context.Context) ([]model.DiskInfo, error) {
	result, err := p.Runner.Run(ctx, "lsblk", "--json", "--bytes", "--output", "NAME,PATH,MODEL,SERIAL,SIZE,TRAN,RM,TYPE,FSTYPE,LABEL,PARTUUID,MOUNTPOINT")
	if err != nil {
		return nil, fmt.Errorf("lsblk: %w", err)
	}

	var output lsblkOutput
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		return nil, fmt.Errorf("parsing lsblk output: %w", err)
	}

	// Identify the medium the installer booted from, so it can be excluded
	// below. Resolved once per listing rather than per device.
	bootUUID := bootPartUUID()

	var disks []model.DiskInfo
	for _, dev := range output.Blockdevices {
		if dev.Type != "disk" {
			slog.Debug("disk skipped", "device", dev.Path, "reason", "not a disk", "type", dev.Type)
			continue
		}

		size, _ := dev.Size.Int64()

		// Skip disks smaller than 8GB (flatcar-install minimum)
		if uint64(size) < 8*1024*1024*1024 {
			slog.Debug("disk skipped", "device", dev.Path, "reason", "smaller than 8 GiB", "size", size)
			continue
		}

		// Skip the installer's own medium and disks backing the running system.
		// Transport is deliberately not a criterion: a large USB disk is a
		// legitimate target, and the RM flag is not trustworthy either, since
		// some kernels report internal SATA disks as removable (e.g. when the
		// AHCI port is hotplug-capable). A disk that merely holds an existing
		// OS install stays selectable — only mounted ones are excluded, since
		// overwriting them would break the live environment.
		if reason := inUseReason(dev, bootUUID); reason != "" {
			slog.Debug("disk skipped", "device", dev.Path, "reason", reason)
			continue
		}

		disk := model.DiskInfo{
			DevPath:   dev.Path,
			Path:      dev.Path,
			Model:     deref(dev.Model),
			Serial:    deref(dev.Serial),
			Size:      uint64(size),
			SizeHuman: humanSize(uint64(size)),
			Transport: deref(dev.Tran),
			Removable: dev.RM,
		}

		// Resolve /dev/disk/by-id path for stable identification
		disk.Path = resolveByIDPath(disk.DevPath)

		// Parse partitions from children
		for _, child := range dev.Children {
			if child.Type == "part" {
				childSize, _ := child.Size.Int64()
				disk.Partitions = append(disk.Partitions, model.PartitionInfo{
					Path:       child.Path,
					Label:      deref(child.Label),
					FSType:     deref(child.FSType),
					Size:       uint64(childSize),
					MountPoint: deref(child.MountPoint),
				})
			}
		}

		slog.Debug("disk eligible", "device", disk.DevPath, "size", disk.SizeHuman,
			"transport", disk.Transport, "removable", disk.Removable, "partitions", len(disk.Partitions))
		disks = append(disks, disk)
	}

	return disks, nil
}

// liveMountPoints are mountpoints that mean the device is backing the running
// system. A device carrying any of them must never be an install target.
var liveMountPoints = map[string]bool{
	"/":        true,
	"/boot":    true,
	"/usr":     true,
	"/sysroot": true,
}

// inUseReason reports why dev is unavailable as an install target, or "" if it
// is available. bootUUID is the PARTUUID of the ESP the installer booted from,
// or "" when it could not be determined. It walks the whole device tree, so
// nested layouts (a mounted filesystem inside LVM or LUKS) are caught, not just
// direct partitions.
func inUseReason(dev lsblkDevice, bootUUID string) string {
	if mp := deref(dev.MountPoint); liveMountPoints[mp] {
		return "mounted at " + mp + " (backs the running system)"
	}
	// The medium knuckle booted from, identified exactly: systemd-boot records
	// the PARTUUID of the ESP it was launched from. Catches installer media of
	// any shape — dd'd stick, IPMI virtual media, a file-copied USB drive.
	if bootUUID != "" && strings.EqualFold(deref(dev.PartUUID), bootUUID) {
		return "holds the ESP the installer booted from"
	}
	// Fallback for boot paths that leave no EFI record (BIOS/CSM, or efivars
	// unreadable): a dd'd hybrid ISO is identifiable by its filesystem.
	if deref(dev.FSType) == "iso9660" {
		return "iso9660 filesystem (installer media)"
	}
	for _, child := range dev.Children {
		if reason := inUseReason(child, bootUUID); reason != "" {
			return reason
		}
	}
	return ""
}

// loaderDevicePartUUIDVar is the efivarfs path of systemd-boot's
// LoaderDevicePartUUID variable, which holds the PARTUUID of the ESP the
// bootloader was launched from — i.e. the installer medium. The GUID suffix is
// systemd's vendor GUID and is stable.
const loaderDevicePartUUIDVar = "/sys/firmware/efi/efivars/LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"

// bootPartUUID returns the PARTUUID of the partition the installer booted from,
// lowercased, or "" when it cannot be determined (BIOS boot, no efivarfs, or a
// bootloader that does not set the variable). A var so tests can supply one.
var bootPartUUID = func() string {
	return bootPartUUIDFrom(loaderDevicePartUUIDVar)
}

// bootPartUUIDFrom is the testable core of bootPartUUID. The efivarfs payload
// is 4 bytes of EFI variable attributes followed by a NUL-terminated UTF-16LE
// string.
func bootPartUUIDFrom(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Debug("boot medium unknown: EFI loader variable unreadable",
			"path", path, "error", err)
		return ""
	}
	const attrLen = 4
	if len(data) < attrLen+2 {
		slog.Debug("boot medium unknown: EFI loader variable too short",
			"path", path, "bytes", len(data))
		return ""
	}
	units := make([]uint16, 0, (len(data)-attrLen)/2)
	for i := attrLen; i+1 < len(data); i += 2 {
		u := binary.LittleEndian.Uint16(data[i:])
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	uuid := strings.ToLower(strings.TrimSpace(string(utf16.Decode(units))))
	slog.Debug("boot medium identified", "partuuid", uuid)
	return uuid
}

func (p *SystemProber) ListNetworkInterfaces(ctx context.Context) ([]model.NetworkInterface, error) {
	result, err := p.Runner.Run(ctx, "ip", "-j", "addr", "show")
	if err != nil {
		return nil, fmt.Errorf("ip addr: %w", err)
	}

	var ipOutput []ipAddrEntry
	if err := json.Unmarshal([]byte(result.Stdout), &ipOutput); err != nil {
		return nil, fmt.Errorf("parsing ip output: %w", err)
	}

	var ifaces []model.NetworkInterface
	for _, entry := range ipOutput {
		// Skip loopback
		if entry.IfName == "lo" {
			continue
		}

		iface := model.NetworkInterface{
			Name:   entry.IfName,
			State:  entry.OperState,
			Driver: entry.LinkType,
		}

		// Extract MAC address
		if entry.Address != "" {
			iface.MAC = entry.Address
		}

		// Extract IP addresses
		for _, addr := range entry.AddrInfo {
			switch addr.Family {
			case "inet":
				iface.IPv4Addrs = append(iface.IPv4Addrs, fmt.Sprintf("%s/%d", addr.Local, addr.PrefixLen))
			case "inet6":
				iface.IPv6Addrs = append(iface.IPv6Addrs, fmt.Sprintf("%s/%d", addr.Local, addr.PrefixLen))
			}
		}

		ifaces = append(ifaces, iface)
	}

	return ifaces, nil
}

type ipAddrEntry struct {
	IfName    string       `json:"ifname"`
	Address   string       `json:"address"`
	OperState string       `json:"operstate"`
	LinkType  string       `json:"link_type"`
	AddrInfo  []ipAddrInfo `json:"addr_info"`
}

type ipAddrInfo struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	PrefixLen int    `json:"prefixlen"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func humanSize(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// resolveByIDPath finds the /dev/disk/by-id/ symlink for a device path.
// Falls back to devPath if /dev/disk/by-id is unavailable (e.g., in CI).
func resolveByIDPath(devPath string) string {
	return resolveByIDPathIn(devPath, "/dev/disk/by-id/")
}

// resolveByIDPathIn is the testable core of resolveByIDPath. It scans byIDDir
// for symlinks pointing to devPath and returns the first match.
func resolveByIDPathIn(devPath, byIDDir string) string {
	entries, err := os.ReadDir(byIDDir)
	if err != nil {
		slog.Warn("disk identity fallback: /dev/disk/by-id resolution failed, using raw device path",
			"device", devPath, "reason", "directory unreadable", "error", err)
		return devPath
	}
	for _, entry := range entries {
		link := filepath.Join(byIDDir, entry.Name())
		target, err := os.Readlink(link)
		if err != nil {
			continue
		}
		// Resolve symlink target: absolute targets are used as-is;
		// relative targets are resolved from byIDDir.
		var absTarget string
		if filepath.IsAbs(target) {
			absTarget = target
		} else {
			absTarget, _ = filepath.Abs(filepath.Join(byIDDir, target))
		}
		if absTarget == devPath {
			return link
		}
	}
	slog.Warn("disk identity fallback: /dev/disk/by-id resolution failed, using raw device path",
		"device", devPath, "reason", "no matching symlink found")
	return devPath
}

// NvidiaGPUInfo represents a detected NVIDIA GPU.
type NvidiaGPUInfo struct {
	PCIAddress string // e.g. "0000:01:00.0"
	PCIClass   string // e.g. "0x030200" (3D controller)
	DeviceName string // e.g. "GA102 [GeForce RTX 3080]" from PCI IDs database
}

// DetectNvidiaGPUs scans the PCI device tree for NVIDIA GPUs using go-nvlib.
// Uses github.com/NVIDIA/go-nvlib/pkg/nvpci — reads /sys/bus/pci/devices directly,
// no NVIDIA driver required. Returns DeviceName from the embedded PCI IDs database.
// The installer runs on the target machine, so detected GPUs will be present on
// the installed system — safe to use for sysext auto-selection.
func DetectNvidiaGPUs() []NvidiaGPUInfo {
	return nvidiaGPUsFromClient(nvpci.New())
}

// nvidiaGPUsFromClient maps nvpci GPU devices to NvidiaGPUInfo.
// Accepts nvpci.Interface for injection in tests (use nvpci.InterfaceMock).
func nvidiaGPUsFromClient(client nvpci.Interface) []NvidiaGPUInfo {
	gpus, err := client.GetGPUs()
	if err != nil {
		return nil
	}
	result := make([]NvidiaGPUInfo, 0, len(gpus))
	for _, g := range gpus {
		name := g.DeviceName
		if name == nvpci.UnknownDeviceString || name == "" {
			name = fmt.Sprintf("NVIDIA GPU (device 0x%04x)", g.Device)
		}
		result = append(result, NvidiaGPUInfo{
			PCIAddress: g.Address,
			PCIClass:   fmt.Sprintf("0x%06x", g.Class),
			DeviceName: name,
		})
	}
	return result
}
