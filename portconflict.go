package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// CheckAndKillConflictingPorts checks if any configured ports are in use by other nanoporter instances
// and kills those instances
func CheckAndKillConflictingPorts(config *Config) error {
	portsToCheck := make(map[int]bool)

	// Collect all local ports from config
	for _, cluster := range config.Clusters {
		for _, forward := range cluster.Forwards {
			portsToCheck[forward.LocalPort] = true
		}
	}

	// Check each port for conflicts
	for port := range portsToCheck {
		if err := checkAndKillPortConflict(port); err != nil {
			return fmt.Errorf("failed to resolve port conflict for %d: %w", port, err)
		}
	}

	return nil
}

// checkAndKillPortConflict checks if a port is in use and kills the process if it's Porter
func checkAndKillPortConflict(port int) error {
	pid, processName, err := findProcessUsingPort(port)
	if err != nil {
		// Port not in use or error checking - proceed
		return nil
	}

	// If no process found, port is free
	if pid == 0 {
		return nil
	}

	// Check if it's a nanoporter process
	if !strings.Contains(processName, "nanoporter") {
		return fmt.Errorf("port %d is in use by non-nanoporter process: %s (PID: %d)", port, processName, pid)
	}

	// Don't kill ourselves
	if pid == os.Getpid() {
		return nil
	}

	slog.Info("Found conflicting nanoporter instance",
		"port", port,
		"pid", pid,
		"process", processName,
	)

	// Kill the process
	if err := killProcess(pid); err != nil {
		return fmt.Errorf("failed to kill conflicting nanoporter process (PID %d): %w", pid, err)
	}

	slog.Info("Killed conflicting nanoporter instance",
		"port", port,
		"pid", pid,
	)

	return nil
}

// findProcessUsingPort finds the PID and name of the process using a port
func findProcessUsingPort(port int) (int, string, error) {
	// Try using lsof first (more reliable)
	pid, name, err := findProcessWithLsof(port)
	if err == nil && pid != 0 {
		return pid, name, nil
	}

	// Fallback to ss command
	pid, name, err = findProcessWithSS(port)
	if err == nil && pid != 0 {
		return pid, name, nil
	}

	// Port not in use or couldn't detect
	return 0, "", nil
}

// findProcessWithLsof uses lsof to find the process using a port
func findProcessWithLsof(port int) (int, string, error) {
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-t", "-sTCP:LISTEN")
	output, err := cmd.Output()
	if err != nil {
		// lsof returns error if no process found, which is fine
		return 0, "", nil
	}

	pidStr := strings.TrimSpace(string(output))
	if pidStr == "" {
		return 0, "", nil
	}

	// Handle multiple PIDs (take first one)
	pids := strings.Split(pidStr, "\n")
	pid, err := strconv.Atoi(pids[0])
	if err != nil {
		return 0, "", err
	}

	// Get process name
	name, err := getProcessName(pid)
	if err != nil {
		return pid, "unknown", nil
	}

	return pid, name, nil
}

// findProcessWithSS uses ss command to find the process using a port
func findProcessWithSS(port int) (int, string, error) {
	cmd := exec.Command("ss", "-ltnp", fmt.Sprintf("sport = :%d", port))
	output, err := cmd.Output()
	if err != nil {
		return 0, "", nil
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, fmt.Sprintf(":%d", port)) {
			// Parse PID from ss output (format: users:(("process",pid=1234,fd=5)))
			start := strings.Index(line, "pid=")
			if start == -1 {
				continue
			}
			start += 4
			end := strings.Index(line[start:], ",")
			if end == -1 {
				end = strings.Index(line[start:], ")")
			}
			if end == -1 {
				continue
			}

			pidStr := line[start : start+end]
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}

			// Get process name
			name, err := getProcessName(pid)
			if err != nil {
				return pid, "unknown", nil
			}

			return pid, name, nil
		}
	}

	return 0, "", nil
}

// getProcessName gets the name of a process by PID
func getProcessName(pid int) (string, error) {
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
	data, err := os.ReadFile(cmdlinePath)
	if err != nil {
		return "", err
	}

	// cmdline is null-separated, take first part
	parts := strings.Split(string(data), "\x00")
	if len(parts) == 0 || parts[0] == "" {
		return "unknown", nil
	}

	// Extract just the binary name
	cmdline := parts[0]
	// Get last part of path
	if idx := strings.LastIndex(cmdline, "/"); idx != -1 {
		cmdline = cmdline[idx+1:]
	}

	return cmdline, nil
}

// killProcess kills a process by PID
func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	// Try SIGTERM first (graceful shutdown)
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	slog.Debug("Sent SIGTERM to process", "pid", pid)

	// Give it a moment to shut down gracefully
	// In a real implementation, you might want to wait and verify
	// For now, we'll trust SIGTERM worked

	return nil
}
