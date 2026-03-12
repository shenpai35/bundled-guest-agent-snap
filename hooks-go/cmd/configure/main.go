// Updates the INI styled file (/etc/default/instance_configs.cfg)
// with values read from `snapctl get`. For each mapping/entry if the snap option is set,
// we insert or update `key = value` inside the target [section]. It preserves
// unrelated lines/comments/etc. and only touches the specified keys.
//
// How to use: ship this as a snap hook (as it's done here in `snap/hooks/configure`)
// so `snap set <snap> foo=bar` is reflected in the file

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const CONFIG = "/etc/default/instance_configs.cfg"

func snapData() string {
	if d := os.Getenv("SNAP_DATA"); d != "" {
		return d
	}
	// sensible fallback path if SNAP_DATA is not set
	return "/var/snap/google-guest-agent/current"
}

func logPath() string      { return filepath.Join(snapData(), "configure.log") }
func snapshotPath() string { return filepath.Join(snapData(), "last-config.json") }

type kv struct {
	section string // [section] in instance_configs.cfg (e.g. "InstanceSetup")
	key     string // key in in instance_configs.cfg (e.g. "optimize_local_ssd")
	opt     string // snap option name, e.g. "optimize-local-ssd"
}

// Declares which of the snap options map to which section/key pairs in the .cfg file
// Deliberately sparse/Ubuntu-y for initial testing
var mapping = []kv{
	{"InstanceSetup", "optimize_local_ssd", "optimize-local-ssd"},
	{"InstanceSetup", "set_multiqueue",     "set-multiqueue"},
	{"InstanceSetup", "network_enabled",    "network-enabled"},
}

// Returns the trimmed string value of a snap option (and treats empty values as "unset")
func snapctlGet(opt string) (string, bool) {
	out, err := exec.Command("snapctl", "get", opt).CombinedOutput()
	v := strings.TrimSpace(string(out))
	return v, err == nil && v != ""
}

// Takes the existing file content and checks that that within each
// [section] there is a key/val line. If the key exists, just replace the
// line. If the section exists but the key does not, append it to the end of
// the section. If the section doesn't exist at all append a brand new section
// with the key/values after

func setKeyValuePairs(in []byte, section, key, val string) []byte {
	want := "[" + section + "]"

	// Read the file line by line
	sc := bufio.NewScanner(bytes.NewReader(in))
	var out []string
	inSec := false
	sawSec := false
	wrote := false

	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)

		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			if inSec && !wrote {
				out = append(out, fmt.Sprintf("%s = %s", key, val))
				wrote = true
			}
			inSec = (trim == want)
			if inSec {
				sawSec = true
			}
			out = append(out, line)
			continue
		}

		if inSec {
			if i := strings.Index(line, "="); i >= 0 {
				left := strings.TrimSpace(line[:i])
				if left == key {
					// Replace the entire line with our "normalised" key/value pair
					out = append(out, fmt.Sprintf("%s = %s", key, val))
					wrote = true
					continue
				}
			}
		}

		// Default path/ copy the original unchanged line
		out = append(out, line)
	}

	if !sawSec {
		out = append(out, want)
		out = append(out, fmt.Sprintf("%s = %s", key, val))
	} else if inSec && !wrote {
		out = append(out, fmt.Sprintf("%s = %s", key, val))
	}

	// Add with newlines and ensure it all ends with a trailing newline
	return []byte(strings.Join(out, "\n") + "\n")
}

// Read the current snap options
func snapshotNow() map[string]string {
	current := make(map[string]string, len(mapping))
	for _, m := range mapping {
		if v, ok := snapctlGet(m.opt); ok {
			current[m.opt] = v
		}
	}
	return current
}

func loadSnapshot(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return map[string]string{}
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return map[string]string{}
	}
	return m
}

func saveSnapshot(path string, m map[string]string) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	b, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(path, b, 0644)
}

func diff(prev, current map[string]string) (added, updated, removed [][2]string) {
	seen := map[string]bool{}
	for k, v := range current {
		seen[k] = true
		if old, ok := prev[k]; !ok {
			added = append(added, [2]string{k, v})
		} else if old != v {
			updated = append(updated, [2]string{k, fmt.Sprintf("%s -> %s", old, v)})
		}
	}
	for k, old := range prev {
		if !seen[k] {
			removed = append(removed, [2]string{k, old})
		}
	}
	return
}

func appendLogf(format string, a ...any) {
	_ = os.MkdirAll(filepath.Dir(logPath()), 0755)
	f, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, format, a...)
	if len(format) == 0 || !strings.HasSuffix(format, "\n") {
		fmt.Fprint(f, "\n")
	}
}

func main() {

	appendLogf("=== configure run %s (rev=%s, inst=%s) ===",
		time.Now().Format(time.RFC3339),
		os.Getenv("SNAP_REVISION"),
		os.Getenv("SNAP_INSTANCE_NAME"),
	)

	// Load the previous snapshot and create a new one
	prev := loadSnapshot(snapshotPath())
	current := snapshotNow()

	// Diff and log what (if anything) changed
	added, updated, removed := diff(prev, current)
	if len(added)+len(updated)+len(removed) == 0 {
		appendLogf("No changes detected in the tracked values")
	} else {
		for _, p := range added {
			appendLogf("Added   %s = %s", p[0], p[1])
		}
		for _, p := range updated {
			appendLogf("Updated %s : %s", p[0], p[1])
		}
		for _, p := range removed {
			appendLogf("Removed %s (was %s)", p[0], p[1])
		}
	}

	// Ensure the config file exists with at least one section so there's
	// somewhere to add keys if none exist yet
	if _, err := os.Stat(CONFIG); os.IsNotExist(err) {
		_ = os.MkdirAll("/etc/default", 0755)
		_ = os.WriteFile(CONFIG, []byte("[InstanceSetup]\n"), 0644)
	}

	// Read the current file
	data, _ := os.ReadFile(CONFIG)
	changed := false

	// For each mapped option, read it from snapctl and merge it into the config
	for _, m := range mapping {
		if v, ok := snapctlGet(m.opt); ok {
			data = setKeyValuePairs(data, m.section, m.key, v)
			changed = true
		}
	}

	// If anything did indeed change, write the file and poke the daemon to reload
	if changed {
		_ = os.WriteFile(CONFIG, data, 0644)
		appendLogf("wrote %s with %d mapped keys", CONFIG, len(current))
		_ = exec.Command(
			"systemctl", "try-reload-or-restart", "--no-block",
			fmt.Sprintf("snap.%s.guest-agent.service", os.Getenv("SNAP_INSTANCE_NAME")),
		).Run()
	} else {
		appendLogf("no file changes needed for %s", CONFIG)
	}

	// Keep the snapshot around so the next run can diff against it
	saveSnapshot(snapshotPath(), current)
}

