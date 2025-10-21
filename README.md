# nanoporter - Kubernetes Port-Forward Manager

A lightweight Go microservice with a Terminal User Interface (TUI) that manages and maintains multiple Kubernetes port-forwards across different clusters. nanoporter automatically monitors connection health and reconnects when failures occur.

## Features

- 🚀 **Multi-Cluster Support**: Manage port-forwards across multiple Kubernetes clusters simultaneously
- 🔄 **Auto-Reconnection**: Automatically detects and reconnects failed port-forwards
- 📊 **Real-time TUI**: Beautiful terminal interface showing live status with full service names
- 🔍 **Health Monitoring**: Continuous health checks with configurable intervals
- 🎯 **Smart Backoff**: Exponential backoff strategy for reconnection attempts
- 🛡️ **Resilient**: Handles pod restarts, network interruptions, and cluster issues gracefully
- ⚙️ **Configurable**: Simple YAML configuration for all settings
- 🔫 **Auto-Kill Conflicts**: Automatically kills other nanoporter instances using the same ports
- 📝 **Clean Logging**: Logs to file by default to keep TUI clean and readable

## Installation

### Prerequisites

- Go 1.21 or later
- Access to Kubernetes clusters with valid kubeconfig files

### Build from Source

```bash
cd /home/user/bbb/porter
go build -o porter .
```

### Install Globally

```bash
go install
```

## Configuration

nanoporter uses a YAML configuration file to define clusters and port-forwards. By default, it looks for `config.yaml` in the current directory.

### Initial Setup

After cloning the repository, create your configuration file:

```bash
# Copy the example configuration
cp config.example.yaml config.yaml

# Edit with your actual cluster details
nano config.yaml  # or use your preferred editor
```

**Important:** The `config.yaml` file is excluded from git to protect your sensitive cluster information. Never commit this file to version control.

### Example Configuration

```yaml
# Global settings
check_interval: 10s  # How often to check port-forward health
reconnect_delay: 5s  # Delay before attempting reconnect

clusters:
  - name: production
    kubeconfig: /home/user/.kube/config
    context: prod-context  # Optional
    
    forwards:
      - namespace: default
        service: api-gateway
        type: service  # "service" or "pod"
        local_port: 8080
        remote_port: 80
      
      - namespace: databases
        service: postgres-primary-0
        type: pod
        local_port: 5432
        remote_port: 5432

  - name: staging
    kubeconfig: /home/user/.kube/staging-config
    
    forwards:
      - namespace: web
        service: frontend
        type: service
        local_port: 3000
        remote_port: 3000
```

See [`config.example.yaml`](./config.example.yaml) for a complete example.

### Configuration Reference

#### Global Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `check_interval` | duration | `10s` | Interval between health checks |
| `reconnect_delay` | duration | `5s` | Initial delay before reconnection |

#### Cluster Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique cluster identifier |
| `kubeconfig` | string | Yes | Path to kubeconfig file |
| `context` | string | No | Specific context to use (uses current-context if omitted) |
| `forwards` | array | Yes | List of port-forward configurations |

#### Forward Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `namespace` | string | Yes | Kubernetes namespace |
| `service` | string | Yes | Service or pod name (used as identifier) |
| `type` | string | Yes | Resource type: `"service"` or `"pod"` |
| `local_port` | int | Yes | Local port to bind (1-65535) |
| `remote_port` | int | Yes | Remote port to forward (1-65535) |

## Usage

### Basic Usage

```bash
# Run with default config (config.yaml)
./porter

# Specify custom config file
./porter -config /path/to/config.yaml

# Enable verbose logging
./porter -verbose
```

### Command-Line Options

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `config.yaml` | Path to configuration file |
| `-verbose` | `false` | Enable verbose/debug logging |
| `-log` | `porter.log` | Log file path (empty string for stderr) |

### TUI Interface

Once started, nanoporter displays a real-time table showing all port-forwards:

```
nanoporter - Kubernetes Port-Forward Manager

Cluster                   Namespace            Service                                  Ports           Status          Info
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
production                default              api-gateway                              8080:80         🟢 Active       checked 2s ago
production                databases            postgres-primary-0                       5432:5432       🟢 Active       checked 1s ago
staging                   web                  frontend-service                         3000:3000       🟡 Reconnecting retry in 3s (attempt 2)
staging                   web                  backend-api-service                      4000:8080       🔴 Failed       pod not found

Press 'q' or Ctrl+C to quit
```

**Note:** Full service names are displayed without truncation. Logs are written to `porter.log` by default to keep the TUI clean.

#### Status Indicators

- 🟢 **Active**: Port-forward is healthy and running
- 🟡 **Reconnecting**: Attempting to reconnect after failure
- 🔴 **Failed**: Connection failed (see error message)
- ⚪ **Starting**: Initial connection in progress
- ⚫ **Stopped**: Port-forward has been stopped

#### Keyboard Controls

- `q` or `Ctrl+C` or `Esc`: Quit application and stop all port-forwards

## How It Works

### Health Monitoring

nanoporter continuously monitors the health of all port-forwards:

1. Every `check_interval` (default: 10s), attempts TCP connection to each local port
2. If connection fails, verifies if the target pod still exists
3. Triggers automatic reconnection if issues detected

### Auto-Reconnection

When a port-forward fails:

1. Immediately attempts reconnection after `reconnect_delay`
2. Uses exponential backoff for subsequent failures (2^n seconds, max 60s)
3. Continues retrying indefinitely until successful or manually stopped
4. Resets retry count after successful connection

### Pod Restart Handling

nanoporter automatically detects and handles pod restarts:

1. Health check detects connection failure
2. Verifies pod status via Kubernetes API
3. Finds new pod instance if old one terminated
4. Re-establishes port-forward to new pod

## Troubleshooting

### Port Already in Use by Another Process

If a port is in use by a non-nanoporter process:

```
Error: port 8080 is in use by non-nanoporter process: nginx (PID: 1234)
```

**Solution**:
- Stop the other process using the port, or
- Change the `local_port` in your config to use a different port

### Port Conflict with Another nanoporter Instance

nanoporter automatically detects and kills other nanoporter instances using the same ports:

```
INFO: Found conflicting nanoporter instance port=8080 pid=5678
INFO: Killed conflicting nanoporter instance port=8080 pid=5678
```

This happens automatically on startup - no action needed. nanoporter will kill the old instance and start successfully.

### Duplicate Ports in Config

```
Error: local port 8080 is used by both 'prod/api' and 'staging/app'
```

**Solution**: Ensure all `local_port` values are unique across all clusters and forwards in your config.

### Kubeconfig Not Found

```
Error: kubeconfig file not found for cluster 'production': /path/to/config
```

**Solution**: Verify the kubeconfig path exists and is accessible.

### Connection Refused

If you see repeated "🔴 Failed" status:

1. Verify the service/pod exists: `kubectl get svc/pod -n <namespace>`
2. Check if pod is running: `kubectl get pods -n <namespace>`
3. Verify you have proper RBAC permissions
4. Check cluster connectivity: `kubectl cluster-info`

### High Retry Count

If retry count keeps increasing:

1. Check if target service/pod exists and is healthy
2. Verify network connectivity to cluster
3. Check kubeconfig credentials are valid
4. Review logs in `porter.log` or use `-verbose` flag for detailed errors

### Viewing Logs

Logs are written to `porter.log` by default to avoid interfering with the TUI display:

```bash
# Tail logs in real-time
tail -f porter.log

# View all logs
cat porter.log

# Use custom log file
./porter -log /var/log/porter.log

# Log to stderr (may interfere with TUI formatting)
./porter -log ""
```

## Development

### Project Structure

```
porter/
├── main.go           # Application entry point
├── config.go         # Configuration loading and validation
├── portforward.go    # Port-forward management and health monitoring
├── portconflict.go   # Port conflict detection and resolution
├── tui.go           # Terminal UI implementation
├── TRD.md           # Technical Requirements Document
├── config.example.yaml  # Example configuration
├── README.md        # This file
├── go.mod           # Go module definition
└── go.sum           # Dependency checksums
```

### Dependencies

- `gopkg.in/yaml.v3` - YAML parsing
- `k8s.io/client-go` - Kubernetes client library
- `k8s.io/api` - Kubernetes API types
- `k8s.io/apimachinery` - Kubernetes API machinery
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/lipgloss` - TUI styling

### Building

```bash
# Build for current platform
go build -o porter .

# Build for specific platform
GOOS=linux GOARCH=amd64 go build -o porter-linux .
GOOS=darwin GOARCH=amd64 go build -o porter-macos .
GOOS=windows GOARCH=amd64 go build -o porter.exe .
```

### Testing

```bash
# Run with example config
cp config.example.yaml config.yaml
# Edit config.yaml with your actual cluster details
./porter

# Run with verbose logging
./porter -verbose
```

## Security Considerations

- nanoporter binds port-forwards only to `localhost` (127.0.0.1)
- Kubeconfig files are read but never modified or logged
- No sensitive data is stored or transmitted
- Respects kubeconfig file permissions
- Supports token refresh for cloud providers

## Performance

- **CPU**: Minimal usage when idle (<1%)
- **Memory**: Low footprint (<100MB for 50+ forwards)
- **Network**: Only maintains necessary Kubernetes API connections
- **Health Checks**: Complete within 2 seconds per forward

## Limitations

- Port-forwards are bound to localhost only (no remote access)
- Requires valid kubeconfig with appropriate RBAC permissions
- Local ports must be unique across all configured forwards
- Maximum 65,535 concurrent port-forwards (theoretical limit of TCP ports)

## Future Enhancements

Potential features for future versions:

- Web dashboard alongside TUI
- Prometheus metrics export
- Configuration hot-reload
- Port-forward templates and profiles
- SSH tunnel support
- Custom health check scripts
- History and logging of connection events

## License

This project is part of a larger microservices architecture. See project root for license information.

## Contributing

nanoporter is designed to be simple and focused. When contributing:

1. Follow Go best practices and conventions
2. Keep the TUI simple and informative
3. Ensure backward compatibility with configuration format
4. Add tests for new features
5. Update documentation accordingly

## Support

For issues, questions, or feature requests, please refer to the project's issue tracker or documentation.

---

**nanoporter** - Because port-forwarding should just work. 🚢