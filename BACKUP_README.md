# Porter Database Backup

This document describes Porter's automatic database backup functionality.

## Overview

Porter **automatically backs up** PostgreSQL databases when the application starts. The backup system:

1. Runs automatically on startup for all databases configured with `db_backup`
2. Reads database credentials from Kubernetes secrets
3. Waits for port forwards to be established
4. Uses `pg_dump` to create SQL backups
5. Stores backups in separate folders per database
6. Creates both plain SQL and compressed (.gz) versions
7. Shows backup status in real-time in the TUI table

## Configuration

Each database port forward can be configured with backup settings in `config.yaml`:

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

### Configuration Fields

- `secret_name`: Name of the Kubernetes secret containing database credentials
- `field_mapping`: Maps credential types to secret field names
  - `host`: Database host (optional, not used for port-forward backups)
  - `database`: Database name
  - `username`: Database username
  - `password`: Database password
  - `connection_string`: Full connection string (optional)

## Prerequisites

1. **PostgreSQL Client Tools**: Ensure `pg_dump` is installed:
   ```bash
   # Ubuntu/Debian
   sudo apt-get install postgresql-client
   
   # macOS
   brew install postgresql
   
   # RHEL/CentOS
   sudo yum install postgresql
   ```

2. **Kubernetes Access**: Valid kubeconfig files with access to the clusters

3. **GZIP**: For compression (usually pre-installed on Linux/macOS)

## Usage

### Running Backups

```bash
# Basic backup with default settings
./porter backup

# Specify custom config file
./porter backup -config /path/to/config.yaml

# Specify custom backup directory
./porter backup -dir /path/to/backups

# Enable verbose logging
./porter backup -verbose

# Adjust timeout for port forwards (default: 120 seconds)
./porter backup -timeout 180
```

### Backup Structure

Backups are organized by database name:

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

## Example Workflow

1. **Update config.yaml** with database backup configurations

2. **Start Porter**:
   ```bash
   ./porter
   ```

3. **Monitor progress in the TUI** - you'll see:
   - Port forwards starting up
   - Backup status changing from "Waiting" → "Pending" → "Running" → "✓ Done"
   - Backup sizes displayed once complete
   - Any errors in the Info column

4. **Verify backups** (in another terminal):
   ```bash
   ls -lh backups/*/
   ```

The backups run in the background while the TUI remains interactive.

## Restoring Databases

To restore a database from a backup:

```bash
# Using plain SQL file
psql -h localhost -p 12049 -U username -d database < backup.sql

# Using compressed file
gunzip -c backup.sql.gz | psql -h localhost -p 12049 -U username -d database
```

Make sure the port forward is active before restoring.

## Troubleshooting

### Port Forward Not Ready

**Error**: `timeout waiting for port forward to become active`

**Solution**: 
- Increase timeout: `./porter backup -timeout 300`
- Check port conflicts: `netstat -tuln | grep <port>`
- Verify service/pod is running in Kubernetes

### Secret Not Found

**Error**: `failed to get secret`

**Solution**:
- Verify secret name matches in cluster
- Check namespace is correct
- Ensure kubeconfig has read access to secrets

### pg_dump Not Found

**Error**: `pg_dump: command not found`

**Solution**: Install PostgreSQL client tools (see Prerequisites)

### Connection Refused

**Error**: `connection refused`

**Solution**:
- Ensure port forward is active
- Verify local port is not blocked by firewall
- Check database is accepting connections

## Automated Backups

You can schedule backups using cron:

```bash
# Daily backup at 2 AM
0 2 * * * cd /path/to/porter && ./porter backup -dir /backups/$(date +\%Y-\%m-\%d)
```

Or use systemd timers for more control.

## Security Notes

- Backups contain sensitive data - store securely
- Use appropriate file permissions (600 or 640)
- Consider encrypting backup files
- Regularly test restore procedures
- Implement retention policies to manage storage

## Configuration Examples

### Minimal Configuration

```yaml
db_backup:
  secret_name: my-db-credentials
  field_mapping:
    database: database
    username: username
    password: password
```

### Full Configuration

```yaml
db_backup:
  secret_name: production-db-secret
  field_mapping:
    host: db_host
    database: db_name
    username: db_user
    password: db_password
    connection_string: conn_string
```

## Build and Install

```bash
# Build porter with backup functionality
cd porter
go build -o porter .

# Make executable
chmod +x porter

# Run backup
./porter backup