package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// BackupManager handles database backups
type BackupManager struct {
	config     *Config
	backupDir  string
	clientsets map[string]*kubernetes.Clientset // cluster name -> clientset
}

// NewBackupManager creates a new backup manager
func NewBackupManager(config *Config, backupDir string) (*BackupManager, error) {
	if backupDir == "" {
		backupDir = "backups"
	}

	// Create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	manager := &BackupManager{
		config:     config,
		backupDir:  backupDir,
		clientsets: make(map[string]*kubernetes.Clientset),
	}

	// Initialize clientsets for each cluster
	for _, cluster := range config.Clusters {
		_, clientset, err := loadKubeconfig(cluster.Kubeconfig, cluster.Context)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig for cluster %s: %w", cluster.Name, err)
		}
		manager.clientsets[cluster.Name] = clientset
	}

	return manager, nil
}

// DBCredentials contains database connection credentials
type DBCredentials struct {
	Database         string
	Username         string
	Password         string
	ConnectionString string
}

// GetDatabaseCredentials retrieves database credentials from a K8s secret
func (m *BackupManager) GetDatabaseCredentials(clusterName, namespace, secretName string, fieldMapping map[string]string) (*DBCredentials, error) {
	clientset, ok := m.clientsets[clusterName]
	if !ok {
		return nil, fmt.Errorf("clientset not found for cluster: %s", clusterName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretName, err)
	}

	creds := &DBCredentials{}

	// Extract fields using the mapping
	if key, ok := fieldMapping["database"]; ok {
		if val, exists := secret.Data[key]; exists {
			creds.Database = string(val)
		}
	}

	if key, ok := fieldMapping["username"]; ok {
		if val, exists := secret.Data[key]; exists {
			creds.Username = string(val)
		}
	}

	if key, ok := fieldMapping["password"]; ok {
		if val, exists := secret.Data[key]; exists {
			creds.Password = string(val)
		}
	}

	if key, ok := fieldMapping["connection_string"]; ok {
		if val, exists := secret.Data[key]; exists {
			creds.ConnectionString = string(val)
		}
	}

	// If we have a connection string but missing individual fields, parse it
	if creds.ConnectionString != "" && (creds.Database == "" || creds.Username == "" || creds.Password == "") {
		if err := parseConnectionString(creds); err != nil {
			slog.Warn("Failed to parse connection string, will use individual fields", "error", err)
		}
	}

	return creds, nil
}

// parseConnectionString parses a PostgreSQL connection string
// Format: postgres://username:password@host:port/database
func parseConnectionString(creds *DBCredentials) error {
	connStr := creds.ConnectionString

	// Simple parsing for postgres:// URLs
	// postgres://username:password@host:port/database
	if len(connStr) < 11 || connStr[:11] != "postgres://" {
		return fmt.Errorf("invalid connection string format")
	}

	// Remove postgres://
	connStr = connStr[11:]

	// Split by @ to separate credentials from host
	parts := strings.Split(connStr, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid connection string format: missing @")
	}

	// Parse credentials (username:password)
	credsPart := parts[0]
	credsParts := strings.Split(credsPart, ":")
	if len(credsParts) >= 2 {
		if creds.Username == "" {
			creds.Username = credsParts[0]
		}
		if creds.Password == "" {
			creds.Password = strings.Join(credsParts[1:], ":")
		}
	}

	// Parse host and database (host:port/database)
	hostPart := parts[1]
	dbParts := strings.Split(hostPart, "/")
	if len(dbParts) >= 2 {
		if creds.Database == "" {
			creds.Database = dbParts[len(dbParts)-1]
		}
	}

	return nil
}

// WaitForPortForward waits for a port forward to be active
func WaitForPortForward(pf *PortForward, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		state := pf.GetState()
		if state == StateActive {
			return nil
		}
		if state == StateStopped || state == StateFailed {
			return fmt.Errorf("port forward in invalid state: %s, error: %s", state, pf.GetError())
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("timeout waiting for port forward to become active")
}

// BackupDatabase performs a database backup using pg_dump and returns the size in MB
func (m *BackupManager) BackupDatabase(dbName string, port int, creds *DBCredentials, pf *PortForward) (float64, error) {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	dbBackupDir := filepath.Join(m.backupDir, dbName)

	// Create database-specific backup directory
	if err := os.MkdirAll(dbBackupDir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create database backup directory: %w", err)
	}

	backupFile := filepath.Join(dbBackupDir, fmt.Sprintf("%s_%s.sql", dbName, timestamp))

	slog.Info("Starting database backup",
		"database", dbName,
		"file", backupFile,
	)

	// Build pg_dump command
	// Using localhost and the forwarded port
	cmd := exec.Command("pg_dump",
		"-h", "localhost",
		"-p", fmt.Sprintf("%d", port),
		"-U", creds.Username,
		"-d", creds.Database,
		"-F", "p", // plain text format
		"-f", backupFile,
		"--no-owner",
		"--no-acl",
	)

	// Set password via environment variable
	cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", creds.Password))

	// Capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("pg_dump failed: %w\nOutput: %s", err, string(output))
	}

	// Get file size
	fileInfo, err := os.Stat(backupFile)
	if err != nil {
		return 0, fmt.Errorf("failed to stat backup file: %w", err)
	}

	sizeMB := float64(fileInfo.Size()) / (1024 * 1024)

	slog.Info("Database backup completed",
		"database", dbName,
		"file", backupFile,
		"size_mb", sizeMB,
	)

	// Also create a compressed version
	gzFile := backupFile + ".gz"
	gzCmd := exec.Command("gzip", "-k", backupFile) // -k keeps original
	if err := gzCmd.Run(); err != nil {
		slog.Warn("Failed to compress backup", "error", err)
	} else {
		if gzInfo, err := os.Stat(gzFile); err == nil {
			slog.Info("Compressed backup created",
				"file", gzFile,
				"size_mb", float64(gzInfo.Size())/(1024*1024),
			)
		}
	}

	// Clean up old backups (keep 2 .sql and 5 .sql.gz)
	if err := m.cleanupOldBackups(dbBackupDir); err != nil {
		slog.Warn("Failed to cleanup old backups", "error", err)
	}

	return sizeMB, nil
}

// cleanupOldBackups removes old backup files, keeping only the latest ones
func (m *BackupManager) cleanupOldBackups(dbBackupDir string) error {
	// Read all files in the backup directory
	entries, err := os.ReadDir(dbBackupDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Separate SQL and GZ files
	var sqlFiles []os.DirEntry
	var gzFiles []os.DirEntry

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".sql.gz") {
			gzFiles = append(gzFiles, entry)
		} else if strings.HasSuffix(name, ".sql") {
			sqlFiles = append(sqlFiles, entry)
		}
	}

	// Sort files by modification time (newest first)
	sortByModTime := func(files []os.DirEntry, dir string) error {
		type fileWithTime struct {
			entry   os.DirEntry
			modTime time.Time
		}

		var filesWithTime []fileWithTime
		for _, f := range files {
			info, err := f.Info()
			if err != nil {
				continue
			}
			filesWithTime = append(filesWithTime, fileWithTime{
				entry:   f,
				modTime: info.ModTime(),
			})
		}

		// Sort by modification time (newest first)
		for i := 0; i < len(filesWithTime); i++ {
			for j := i + 1; j < len(filesWithTime); j++ {
				if filesWithTime[i].modTime.Before(filesWithTime[j].modTime) {
					filesWithTime[i], filesWithTime[j] = filesWithTime[j], filesWithTime[i]
				}
			}
		}

		// Update original slice
		for i, f := range filesWithTime {
			if i < len(files) {
				files[i] = f.entry
			}
		}

		return nil
	}

	// Sort SQL files and keep only 2 latest
	if err := sortByModTime(sqlFiles, dbBackupDir); err != nil {
		return err
	}
	if len(sqlFiles) > 2 {
		for _, f := range sqlFiles[2:] {
			filePath := filepath.Join(dbBackupDir, f.Name())
			if err := os.Remove(filePath); err != nil {
				slog.Warn("Failed to remove old SQL backup", "file", filePath, "error", err)
			} else {
				slog.Info("Removed old SQL backup", "file", filePath)
			}
		}
	}

	// Sort GZ files and keep only 5 latest
	if err := sortByModTime(gzFiles, dbBackupDir); err != nil {
		return err
	}
	if len(gzFiles) > 5 {
		for _, f := range gzFiles[5:] {
			filePath := filepath.Join(dbBackupDir, f.Name())
			if err := os.Remove(filePath); err != nil {
				slog.Warn("Failed to remove old GZ backup", "file", filePath, "error", err)
			} else {
				slog.Info("Removed old GZ backup", "file", filePath)
			}
		}
	}

	return nil
}

// BackupAllDatabases backs up all configured databases
func (m *BackupManager) BackupAllDatabases(manager *PortForwardManager) error {
	slog.Info("Starting database backup process")

	var backupCount int
	var errors []error

	for _, cluster := range m.config.Clusters {
		for _, forward := range cluster.Forwards {
			// Skip forwards without backup configuration
			if forward.DBBackup == nil {
				continue
			}

			slog.Info("Processing database backup",
				"cluster", cluster.Name,
				"namespace", forward.Namespace,
				"service", forward.Service,
			)

			// Find the corresponding port forward
			var pf *PortForward
			for _, f := range manager.GetForwards() {
				if f.ClusterName == cluster.Name &&
					f.Config.Namespace == forward.Namespace &&
					f.Config.Service == forward.Service {
					pf = f
					break
				}
			}

			if pf == nil {
				err := fmt.Errorf("port forward not found for %s/%s/%s",
					cluster.Name, forward.Namespace, forward.Service)
				slog.Error("Port forward not found", "error", err)
				errors = append(errors, err)
				continue
			}

			// Mark backup as pending
			pf.setBackupState(BackupPending)

			// Wait for port forward to be active
			slog.Info("Waiting for port forward to be active",
				"service", forward.Service,
			)

			if err := WaitForPortForward(pf, 60*time.Second); err != nil {
				slog.Error("Port forward not ready", "error", err)
				pf.setBackupState(BackupFailed)
				pf.setBackupError(err.Error())
				errors = append(errors, err)
				continue
			}

			// Mark backup as running
			pf.setBackupState(BackupRunning)

			// Get database credentials from secret
			creds, err := m.GetDatabaseCredentials(
				cluster.Name,
				forward.Namespace,
				forward.DBBackup.SecretName,
				forward.DBBackup.FieldMapping,
			)
			if err != nil {
				slog.Error("Failed to get database credentials", "error", err)
				pf.setBackupState(BackupFailed)
				pf.setBackupError(err.Error())
				errors = append(errors, err)
				continue
			}

			// Perform backup
			dbName := forward.Service
			sizeMB, err := m.BackupDatabase(dbName, forward.LocalPort, creds, pf)
			if err != nil {
				slog.Error("Backup failed",
					"database", dbName,
					"error", err,
				)
				pf.setBackupState(BackupFailed)
				pf.setBackupError(err.Error())
				errors = append(errors, err)
				continue
			}

			// Mark backup as completed
			pf.setBackupCompleted(sizeMB)
			backupCount++
		}
	}

	slog.Info("Database backup process completed",
		"successful", backupCount,
		"failed", len(errors),
	)

	if len(errors) > 0 {
		return fmt.Errorf("backup completed with %d errors (see logs for details)", len(errors))
	}

	return nil
}
