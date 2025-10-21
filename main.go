package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	defaultConfigPath = "config.yaml"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", defaultConfigPath, "Path to configuration file")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	logFile := flag.String("log", "", "Log file path (default: stderr, or porter.log if TUI active)")
	flag.Parse()

	// Setup logging
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}

	// Determine log output
	var logOutput *os.File
	var closeLog bool

	if *logFile != "" {
		// Use specified log file
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open log file: %v\n", err)
			os.Exit(1)
		}
		logOutput = f
		closeLog = true
	} else {
		// Default to nanoporter.log to avoid interfering with TUI
		f, err := os.OpenFile("nanoporter.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// Fallback to stderr if can't create log file
			logOutput = os.Stderr
		} else {
			logOutput = f
			closeLog = true
		}
	}

	logger := slog.New(slog.NewTextHandler(logOutput, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	if closeLog {
		defer logOutput.Close()
		if logOutput != os.Stderr {
			fmt.Printf("Logging to: %s\n", logOutput.Name())
		}
	}

	// Load configuration
	slog.Info("Loading configuration", "path", *configPath)
	config, err := LoadConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	slog.Info("Configuration loaded successfully",
		"clusters", len(config.Clusters),
		"check_interval", config.CheckInterval,
		"reconnect_delay", config.ReconnectDelay,
	)

	// Count total forwards
	totalForwards := 0
	for _, cluster := range config.Clusters {
		totalForwards += len(cluster.Forwards)
	}
	slog.Info("Total port-forwards configured", "count", totalForwards)

	// Check for and kill conflicting Porter instances
	slog.Info("Checking for port conflicts")
	if err := CheckAndKillConflictingPorts(config); err != nil {
		slog.Error("Failed to resolve port conflicts", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create port-forward manager
	manager := NewPortForwardManager(config)

	// Initialize all port-forwards
	slog.Info("Initializing port-forward manager")
	if err := manager.Initialize(); err != nil {
		slog.Error("Failed to initialize port-forward manager", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Start port-forwards and monitoring
	slog.Info("Starting port-forwards")
	manager.Start()

	// Setup signal handler for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		slog.Info("Received shutdown signal")
		manager.Stop()
	}()

	// Start TUI
	slog.Info("Starting TUI")
	model := NewTUIModel(manager)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		slog.Error("TUI error", "error", err)
		manager.Stop()
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	slog.Info("Porter shutdown complete")
}
