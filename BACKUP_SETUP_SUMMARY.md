# Porter Database Backup - Setup Summary

## Overview

I've successfully implemented an **automatic database backup solution** for Porter that:

1. ✅ Runs automatically on startup (no separate backup command needed)
2. ✅ Shows backup status in real-time in the TUI table
3. ✅ Extends the configuration structure to support database backup settings
4. ✅ Retrieves database credentials from Kubernetes secrets
5. ✅ Waits for port forwards to be established before backing up
6. ✅ Uses `pg_dump` to create SQL backups for each database
7. ✅ Stores backups in separate folders per database
8. ✅ Creates both plain SQL and compressed (.gz) versions

## Files Created/Modified

### Modified Files

1. **`config.go`** - Added backup configuration structures:
   - `DBBackupConfig` struct with `secret_name` and `field_mapping`
   - Extended `ForwardConfig` to include optional `db_backup` field

2. **`config.yaml`** - Added backup configuration for all database services:
   - comp-proj-map-canary-db-pooler
   - comp-profile-canary-db-pooler
   - u5age-canary-db-pooler
   - ba1ance-canary-db-pooler
   - pr1cer-canary-db-pooler
   - pay-by-card-canary-db-pooler
   - progressive-packages-canary-db-pooler
   - h3ssh-canary-db-pooler
   - id3ntix-canary-db-db-rw
   - sshkey-db-pooler

3. **`main.go`** - Integrated automatic backup on startup

4. **`portforward.go`** - Added backup state tracking:
   - `BackupState` enum (Pending, Running, Completed, Failed)
   - Added backup metadata fields to `PortForward` struct
   - Helper methods for updating backup status

5. **`tui.go`** - Enhanced TUI with backup status column:
   - Shows real-time backup progress
   - Displays backup size when completed
   - Shows backup errors inline

### New Files

1. **`backup.go`** - Core backup functionality:
   - `BackupManager` struct for managing backups
   - `GetDatabaseCredentials()` - Retrieves credentials from K8s secrets
   - `WaitForPortForward()` - Waits for port forwards to be active
   - `BackupDatabase()` - Executes pg_dump for a single database
   - `BackupAllDatabases()` - Orchestrates backup of all configured databases

2. **`backup_cmd.go`** - CLI interface for backup command:
   - Parses command-line flags
   - Initializes backup and port-forward managers
   - Executes backup workflow

3. **`BACKUP_README.md`** - Comprehensive documentation:
   - Configuration guide
   - Usage instructions
   - Troubleshooting tips
   - Examples

## Configuration Format

Each database port forward now supports an optional `db_backup` section:

```yaml
forwards:
  - namespace: bizlogic-canary
    service: comp-profile-canary-db-pooler
    type: service
    local_port: 12049
    remote_port: 5432
    db_backup:
      secret_name: comp-profile-canary-db-credentials
      field_mapping:
        host: host
        database: database
        username: username
        password: password
        connection_string: connection-string
```

### Field Mapping

The `field_mapping` allows flexible mapping between the backup system's expected fields and the actual keys in your Kubernetes secrets:

- **host**: Database host (optional, not used for localhost backups)
- **database**: Database name (required)
- **username**: Database username (required)
- **password**: Database password (required)
- **connection_string**: Full connection string (optional)

## Usage

### Basic Backup

```bash
# Build the porter binary
cd porter
go build -o porter .

# Run backup with default settings
./porter backup
```

### Advanced Options

```bash
# Specify custom config file
./porter backup -config /path/to/config.yaml

# Specify custom backup directory
./porter backup -dir /backups/$(date +%Y-%m-%d)

# Enable verbose logging
./porter backup -verbose

# Adjust timeout for port forwards (default: 120 seconds)
./porter backup -timeout 180
```

### Example Output

```
Porter Database Backup Utility
================================

Found 10 database(s) configured for backup

Starting port forwards...
Waiting 120 seconds for port forwards to establish...

Starting database backups...
✓ comp-proj-map-canary-db-pooler backed up (125.4 MB)
✓ comp-profile-canary-db-pooler backed up (89.2 MB)
✓ u5age-canary-db-pooler backed up (45.6 MB)
...

Stopping port forwards...

✓ All database backups completed successfully!
Backups stored in: backups
```

## Backup Structure

Backups are organized by service name with timestamps:

```
backups/
├── comp-profile-canary-db-pooler/
│   ├── comp-profile-canary-db-pooler_2025-11-11_14-30-00.sql
│   └── comp-profile-canary-db-pooler_2025-11-11_14-30-00.sql.gz
├── u5age-canary-db-pooler/
│   ├── u5age-canary-db-pooler_2025-11-11_14-30-15.sql
│   └── u5age-canary-db-pooler_2025-11-11_14-30-15.sql.gz
└── ...
```

## How It Works

### On Startup

1. **Port Forwards Start**: All configured port forwards begin establishing
2. **Backup Thread**: A background goroutine starts the backup process
3. **Status Updates**: Port forwards and backups update their status in real-time
4. **TUI Display**: The table shows live progress for both port forwards and backups

### Backup Process

1. **Wait for Port Forward**: Each backup waits for its port forward to be `Active`
2. **Update Status**: Backup state changes to `Pending`, then `Running`
3. **Retrieve Credentials**: Fetches database credentials from K8s secrets
4. **Execute pg_dump**: Runs backup via localhost using forwarded port
5. **Compress**: Creates gzip compressed version
6. **Complete**: Updates status to `Completed` with size, or `Failed` with error
7. **Continue**: TUI remains active, port forwards keep running

## Prerequisites

1. **PostgreSQL Client**: Install `pg_dump`:
   ```bash
   # Ubuntu/Debian
   sudo apt-get install postgresql-client
   
   # macOS
   brew install postgresql
   ```

2. **Kubernetes Access**: Valid kubeconfig files with permissions to:
   - Read secrets in configured namespaces
   - Create port forwards to services/pods

3. **Disk Space**: Ensure sufficient space for database dumps

## Security Considerations

- Backups contain sensitive data - store in secure locations
- Use appropriate file permissions (e.g., 600)
- Consider encrypting backup files
- Implement retention policies
- Regularly test restore procedures

## Kubernetes Secret Format

The backup system expects secrets to contain the following fields (as defined in `field_mapping`):

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: comp-profile-canary-db-credentials
  namespace: bizlogic-canary
type: Opaque
data:
  host: <base64-encoded-host>
  database: <base64-encoded-database-name>
  username: <base64-encoded-username>
  password: <base64-encoded-password>
  connection-string: <base64-encoded-connection-string>
```

The actual field names can vary - just update the `field_mapping` in your config.

## Next Steps

1. **Verify Secrets**: Ensure all K8s secrets exist with correct field names
2. **Test Backup**: Run a test backup to verify everything works
3. **Schedule Backups**: Set up cron jobs or systemd timers for automated backups
4. **Monitor**: Implement monitoring/alerting for backup failures
5. **Test Restore**: Regularly test restore procedures

## Troubleshooting

### Common Issues

1. **Secret not found**: Verify secret name and namespace in config
2. **Port forward timeout**: Increase timeout or check pod status
3. **pg_dump not found**: Install PostgreSQL client tools
4. **Permission denied**: Check kubeconfig permissions

See `BACKUP_README.md` for detailed troubleshooting guide.

## Example Cron Job

```bash
# Daily backup at 2 AM
0 2 * * * cd /home/user/tools/porter && ./porter backup -dir /backups/$(date +\%Y-\%m-\%d) >> /var/log/porter-backup.log 2>&1