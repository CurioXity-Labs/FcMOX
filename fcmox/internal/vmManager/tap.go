package vmmanager

import (
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/google/uuid"
)

func createTap(vmID string) (string, string, bool) {
	u := uuid.NewMD5(uuid.NameSpaceDNS, []byte(vmID))
	b := u[:]

	// Use first 6 bytes of UUID, force LAA bit
	mac := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		(b[0]|2)&0xfe, b[1], b[2], b[3], b[4], b[5])

	tapName := fmt.Sprintf("tap-%s", vmID)
	bridge := "fc-br0"

	// 1. Cleanup old junk (Idempotency)
	_ = exec.Command("ip", "tuntap", "del", "dev", tapName, "mode", "tap").Run()

	// 2. Commands defined as a slice for cleaner execution
	commands := [][]string{
		{"ip", "tuntap", "add", "dev", tapName, "mode", "tap"},
		{"ip", "link", "set", tapName, "address", mac}, // Set MAC on Host side too
		{"ip", "link", "set", tapName, "master", bridge},
		{"ip", "link", "set", tapName, "up"},
	}

	for _, cmdArgs := range commands {
		if out, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput(); err != nil {
			slog.Error("Network setup failed", "cmd", cmdArgs, "out", string(out), "err", err)
			return "", "", false
		}
	}

	return tapName, mac, true
}

// deleteTap removes a TAP device. Best-effort; errors are logged but not fatal.
func deleteTap(tapName string) {
	if tapName == "" {
		return
	}
	if out, err := exec.Command("ip", "link", "del", tapName).CombinedOutput(); err != nil {
		slog.Warn("failed to delete TAP", "tap", tapName, "err", err, "output", string(out))
	}
}

// Cleanup stops all running VMs, removing their TAP devices, firecracker
// processes, and sockets. Call this on application shutdown.
func (mgr *VmManager) Cleanup() {
	slog.Info("cleaning up all VMs...")
	for id, vm := range mgr.Vms {
		if vm.Status != VmStatusStopped {
			if err := mgr.StopVm(id); err != nil {
				slog.Warn("cleanup: failed to stop VM", "id", id, "err", err)
			}
		}
	}
	slog.Info("all VMs cleaned up")
}
