package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"
)

func runBackupCommand() {
	// Create a separate flag set for backup command
	backupFlags := flag.NewFlagSet("backup", flag.ExitOnError)
	configPath := backupFlags.String("config", "config.yaml", "Path to configuration file")
	backupDir := backupFlags.String("dir", "backups", "Directory to store backups")
	verbose := backupFlags.Bool("verbose", false, "Enable verbose logging")
	waitTimeout := backupFlags.Int("timeout", 120, "Timeout in seconds to wait for port forwards")

	if len(os.Args) < 2 || os.Args[1] != "backup" {
		return
	}

	backupFlags.Parse(os.Args[2:])

	// Setup logging
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	fmt.Printf("Porter Database Backup Utility\n")
	fmt.Printf("================================\n\n")

	// Load configuration
	slog.Info("Loading configuration", "path", *configPath)
	config, err := LoadConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Count databases to backup
	dbCount := 0
	for _, cluster := range config.Clusters {
		for _, forward := range cluster.Forwards {
			if forward.DBBackup != nil {
				dbCount++
			}
		}
	}

	if dbCount == 0 {
		fmt.Println("No databases configured for backup")
		os.Exit(0)
	}

	fmt.Printf("Found %d database(s) configured for backup\n\n", dbCount)

	// Create backup manager
	slog.Info("Initializing backup manager", "backup_dir", *backupDir)
	backupManager, err := NewBackupManager(config, *backupDir)
	if err != nil {
		slog.Error("Failed to initialize backup manager", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create port-forward manager
	slog.Info("Initializing port-forward manager")
	portManager := NewPortForwardManager(config)
	if err := portManager.Initialize(); err != nil {
		slog.Error("Failed to initialize port-forward manager", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Start port-forwards
	fmt.Println("Starting port forwards...")
	portManager.Start()

	// Wait a bit for port forwards to establish
	fmt.Printf("Waiting %d seconds for port forwards to establish...\n", *waitTimeout)
	time.Sleep(5 * time.Second)

	// Perform backups
	fmt.Println("\nStarting database backups...")
	if err := backupManager.BackupAllDatabases(portManager); err != nil {
		slog.Error("Backup process completed with errors", "error", err)
		portManager.Stop()
		fmt.Fprintf(os.Stderr, "\nBackup completed with errors. Check logs for details.\n")
		os.Exit(1)
	}

	// Stop port-forwards
	fmt.Println("\nStopping port forwards...")
	portManager.Stop()
	time.Sleep(2 * time.Second)

	fmt.Printf("\n✓ All database backups completed successfully!\n")
	fmt.Printf("Backups stored in: %s\n", *backupDir)
}
