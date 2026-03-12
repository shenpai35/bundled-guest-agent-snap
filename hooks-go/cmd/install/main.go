// Writes a systemd drop-in so the snap services start *after* ssh and network-online

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
		inst = "google-guest-agent" // hard-code the name just in case
	}
  // Create the symlinks first so they exist before systemd tries to use them
	binaries := []string{
		"google_guest_agent_manager",
		"google_guest_compat_manager",
		"google_metadata_script_runner",
	}

	for _, bin := range binaries {
		target := filepath.Join("/usr/bin", bin)
		source := filepath.Join("/snap", inst, "current/bin", bin)
		_ = os.Remove(target)
		_ = os.Symlink(source, target)
	}

	// Override the systemd units to use /usr/bin paths in 'ps aux'
	services := map[string]string{
		"guest-agent":           "/usr/bin/google_guest_agent_manager",
		"guest-compat-manager": "/usr/bin/google_guest_compat_manager",
	}

	for appName, binaryPath := range services {
		unit := fmt.Sprintf("snap.%s.%s.service", inst, appName)
		dir := filepath.Join("/etc/systemd/system", unit+".d")
		_ = os.MkdirAll(dir, 0755)

		conf := filepath.Join(dir, "10-exec-path-override.conf")
		
		// ExecStart= with an empty value clears the existing command
		// The second ExecStart= sets the new path
		content := fmt.Sprintf("[Unit]\nWants=ssh.service network-online.target\nAfter=ssh.service network-online.target\n\n[Service]\nExecStart=\nExecStart=%s\n", binaryPath)
		
		_ = os.WriteFile(conf, []byte(content), 0644)
	}

	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "restart", fmt.Sprintf("snap.%s.guest-agent.service", inst)).Run()
	_ = exec.Command("systemctl", "restart", fmt.Sprintf("snap.%s.guest-compat-manager.service", inst)).Run()
}

