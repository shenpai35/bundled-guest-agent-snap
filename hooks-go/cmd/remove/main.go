// Clean up the systemd overrides and legacy symlinks on the snap's removal

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	inst := os.Getenv("SNAP_INSTANCE_NAME")
	if inst == "" {
		inst = "google-guest-agent"
	}

	// 1. Clean up Legacy Symlinks in /usr/bin
	binaries := []string{
		"google_guest_agent_manager",
		"google_guest_compat_manager",
		"google_metadata_script_runner",
		"gce_workload_cert_refresh",
		"ggactl_plugin",
	}

	for _, bin := range binaries {
		_ = os.Remove(filepath.Join("/usr/bin", bin))
	}

	// 2. Clean up Systemd Override Directories
	// This covers the main agent, the compat manager, and any others created
	apps := []string{"guest-agent", "guest-compat-manager"}
	for _, app := range apps {
		unit := fmt.Sprintf("snap.%s.%s.service", inst, app)
		dir := filepath.Join("/etc/systemd/system", unit+".d")
		
		// Remove the specific config files we created
		_ = os.Remove(filepath.Join(dir, "01-guest-agent-snap-wait-ssh.conf"))
		_ = os.Remove(filepath.Join(dir, "10-exec-path-override.conf"))
		
		// Remove the directory itself if it's empty
		_ = os.Remove(dir)
	}

	// 3. Final systemd refresh
	_ = exec.Command("systemctl", "daemon-reload").Run()
}
