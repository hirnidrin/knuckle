package tui

import (
	"strings"
	"testing"

	"github.com/projectbluefin/knuckle/internal/model"
)

// fcosDisk mirrors what the prober reports for a disk holding an existing
// Fedora CoreOS install: BIOS boot, ESP, /boot and root.
func fcosDisk() model.DiskInfo {
	return model.DiskInfo{
		DevPath:   "/dev/sda",
		Path:      "/dev/sda",
		Model:     "Crucial_CT240M50",
		SizeHuman: "223.6 GB",
		Transport: "sata",
		Partitions: []model.PartitionInfo{
			{Path: "/dev/sda1", Size: 1048576},
			{Path: "/dev/sda2", Label: "EFI-SYSTEM", FSType: "vfat"},
			{Path: "/dev/sda3", Label: "boot", FSType: "ext4"},
			{Path: "/dev/sda4", Label: "root", FSType: "xfs"},
		},
	}
}

func emptyDisk() model.DiskInfo {
	return model.DiskInfo{
		DevPath:   "/dev/nvme0n1",
		Path:      "/dev/nvme0n1",
		Model:     "KINGSTON SKC3000D4096G",
		SizeHuman: "3.7 TB",
		Transport: "nvme",
	}
}

func storageModel(disks ...model.DiskInfo) *Model {
	w := newTestWizard()
	w.State.CurrentStep = model.StepStorage
	w.State.Config.Channel = "stable"
	w.State.Disks = disks
	m := New(w)
	m.cursor = 0
	return m
}

func TestViewStorage_ShowsPartitionCount(t *testing.T) {
	view := storageModel(fcosDisk(), emptyDisk()).viewStorage()

	if !strings.Contains(view, "4 partitions") {
		t.Errorf("occupied disk should be flagged in the list:\n%s", view)
	}
	if strings.Contains(view, "0 partitions") {
		t.Errorf("empty disk should carry no partition note:\n%s", view)
	}
}

func TestViewStorage_SinglePartitionSingular(t *testing.T) {
	d := emptyDisk()
	d.Partitions = []model.PartitionInfo{{Path: "/dev/nvme0n1p1", FSType: "btrfs"}}

	view := storageModel(d).viewStorage()
	if !strings.Contains(view, "1 partition") {
		t.Errorf("expected singular partition note:\n%s", view)
	}
	if strings.Contains(view, "1 partitions") {
		t.Errorf("partition note should not be pluralised for a single partition:\n%s", view)
	}
}

func TestStorage_OccupiedDiskSelectsWithoutExtraConfirmation(t *testing.T) {
	m := storageModel(fcosDisk())

	_, _ = m.handleEnter()

	if m.err != nil {
		t.Fatalf("selecting an occupied disk should not prompt, got %v", m.err)
	}
	if m.Wizard.State.Config.Disk.DevPath != "/dev/sda" {
		t.Errorf("expected /dev/sda selected, got %q", m.Wizard.State.Config.Disk.DevPath)
	}
	if m.Wizard.State.CurrentStep == model.StepStorage {
		t.Error("expected to advance past StepStorage")
	}
}
