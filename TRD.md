# Technical Requirements Document: nanoporter - Kubernetes Port-Forward Manager

## 1. Overview

### 1.1 Purpose
nanoporter is a lightweight Go microservice with a Terminal User Interface (TUI) that manages and maintains multiple Kubernetes port-forwards across different clusters. It automatically monitors connection health and reconnects when failures occur.

### 1.2 Scope
- Read configuration from YAML file
- Establish port-forwards to multiple Kubernetes clusters
- Monitor port-forward health continuously
- Auto-reconnect on connection failures or pod restarts
- Provide real-time TUI status interface

## 2. System Architecture

### 2.1 Components

#### 2.1.1 Configuration Manager
- Loads and validates YAML configuration
- Parses kubeconfig file paths
- Validates port-forward definitions

#### 2.1.2 Kubernetes Client Manager
- Initializes clients for each configured cluster
- Manages kubeconfig contexts
- Handles authentication

#### 2.1.3 Port-Forward Manager
- Creates and manages port-forward connections
- Tracks connection states
- Implements reconnection logic

#### 2.1.4 Health Monitor
- Continuously checks port-forward status
- Detects connection failures
- Detects pod restarts/deletions
- Triggers reconnection on failures

#### 2.1.5 TUI Interface
- Displays real-time status of all port-forwards
- Shows cluster, namespace, service, and port information
- Indicates connection health (active/reconnecting/failed)
- Displays error messages and logs

### 2.2 Technology Stack
- **Language**: Go 1.21+
- **Kubernetes Client**: `k8s.io/client-go`
- **TUI Framework**: `github.com/charmbracelet/bubbletea`
- **Configuration**: `gopkg.in/yaml.v3`
- **Logging**: `log/slog` (standard library)

## 3. Configuration Schema

### 3.1 YAML Structure

```yaml
# Global settings
check_interval: 10s  # How often to check port-forward health
reconnect_delay: 5s  # Delay before attempting reconnect

# Kubernetes clusters configuration
clusters:
  - name: production-cluster
    kubeconfig: /home/user/.kube/config-prod
    context: prod-context  # Optional, uses current-context if not specified
    
    # Port-forward definitions for this cluster
    forwards:
      - name: api-service
        namespace: default
        service: api-service  # Service name or pod name
        type: service  # "service" or "pod"
        local_port: 8080
        remote_port: 80
        
      - name: database
        namespace: databases
        service: postgres-primary
        type: pod
        local_port: 5432
        remote_port: 5432

  - name: staging-cluster
    kubeconfig: /home/user/.kube/config-staging
    
    forwards:
      - name: frontend
        namespace: web
        service: frontend-service
        type: service
        local_port: 3000
        remote_port: 3000
```

### 3.2 Configuration Fields

#### Global Settings
- `check_interval`: Duration between health checks (default: 10s)
- `reconnect_delay`: Delay before reconnection attempts (default: 5s)

#### Cluster Configuration
- `name`: Unique cluster identifier
- `kubeconfig`: Absolute path to kubeconfig file
- `context`: (Optional) Specific context to use from kubeconfig

#### Forward Configuration
- `name`: Unique identifier for the port-forward
- `namespace`: Kubernetes namespace
- `service`: Name of the service or pod
- `type`: Resource type ("service" or "pod")
- `local_port`: Local port to bind
- `remote_port`: Remote port to forward

## 4. Functional Requirements

### 4.1 Configuration Loading
- **FR-1.1**: Load configuration from specified YAML file
- **FR-1.2**: Validate all required fields
- **FR-1.3**: Validate port ranges (1-65535)
- **FR-1.4**: Check for duplicate local ports
- **FR-1.5**: Validate kubeconfig file existence

### 4.2 Port-Forward Management
- **FR-2.1**: Establish port-forwards for all configured services
- **FR-2.2**: Support both Service and Pod resource types
- **FR-2.3**: Handle multiple clusters simultaneously
- **FR-2.4**: Prevent port conflicts

### 4.3 Health Monitoring
- **FR-3.1**: Check port-forward status at configured intervals
- **FR-3.2**: Detect TCP connection failures
- **FR-3.3**: Detect pod restarts/deletions
- **FR-3.4**: Detect cluster connectivity issues

### 4.4 Auto-Reconnection
- **FR-4.1**: Automatically reconnect on connection failure
- **FR-4.2**: Implement exponential backoff for repeated failures
- **FR-4.3**: Retry indefinitely until successful or manually stopped
- **FR-4.4**: Update TUI status during reconnection attempts

### 4.5 TUI Interface
- **FR-5.1**: Display all configured port-forwards in a table
- **FR-5.2**: Show real-time status for each forward
- **FR-5.3**: Display cluster name, namespace, service, ports
- **FR-5.4**: Show connection state (🟢 Active, 🟡 Reconnecting, 🔴 Failed)
- **FR-5.5**: Display error messages
- **FR-5.6**: Support keyboard navigation (q to quit)
- **FR-5.7**: Auto-refresh display

## 5. Non-Functional Requirements

### 5.1 Performance
- **NFR-1.1**: Minimal CPU usage when idle
- **NFR-1.2**: Low memory footprint (<100MB)
- **NFR-1.3**: Health checks complete within 2 seconds

### 5.2 Reliability
- **NFR-2.1**: Graceful handling of network interruptions
- **NFR-2.2**: No data loss on restart
- **NFR-2.3**: Clean shutdown on SIGTERM/SIGINT

### 5.3 Maintainability
- **NFR-3.1**: Clear error messages
- **NFR-3.2**: Structured logging with levels
- **NFR-3.3**: Modular code architecture

### 5.4 Usability
- **NFR-4.1**: Simple YAML configuration
- **NFR-4.2**: Intuitive TUI interface
- **NFR-4.3**: Helpful error messages

## 6. Implementation Details

### 6.1 Port-Forward Implementation
Port-forwards are established using the Kubernetes client-go library's `portforward` package. Each forward runs in a separate goroutine.

### 6.2 Health Check Algorithm
1. Attempt TCP connection to localhost:local_port
2. If connection succeeds, mark as healthy
3. If connection fails, verify pod still exists
4. If pod missing/restarted, trigger reconnection
5. If cluster unreachable, mark as failed

### 6.3 Reconnection Strategy
- Initial retry: After `reconnect_delay`
- Subsequent retries: Exponential backoff (2^n seconds, max 60s)
- Maximum retries: Unlimited (until manual stop)

### 6.4 Concurrency Model
- Each port-forward runs in dedicated goroutine
- Health checks run in separate goroutines
- TUI updates via channel communication
- Thread-safe state management using sync.RWMutex

## 7. Error Handling

### 7.1 Configuration Errors
- Invalid YAML syntax → Exit with error message
- Missing kubeconfig → Exit with error message
- Invalid port range → Exit with error message
- Duplicate local ports → Exit with error message

### 7.2 Runtime Errors
- Cluster unreachable → Mark as failed, retry
- Pod not found → Retry with backoff
- Port already in use → Exit with error
- Connection lost → Auto-reconnect

## 8. Logging

### 8.1 Log Levels
- **DEBUG**: Detailed connection state changes
- **INFO**: Port-forward started/stopped, health checks
- **WARN**: Reconnection attempts, temporary failures
- **ERROR**: Persistent failures, configuration errors

### 8.2 Log Format
Structured logging using slog with fields:
- timestamp
- level
- cluster
- namespace
- service
- message

## 9. Future Enhancements

### 9.1 Potential Features
- Web dashboard alongside TUI
- Metrics export (Prometheus)
- Configuration hot-reload
- Port-forward templates
- Multiple configuration profiles
- SSH tunnel support
- Custom health check scripts

## 10. Security Considerations

### 10.1 Kubeconfig Security
- Never log sensitive kubeconfig data
- Respect kubeconfig file permissions
- Support token refresh for cloud providers

### 10.2 Local Port Binding
- Bind only to localhost (127.0.0.1)
- No remote access to forwarded ports
- Validate port ownership

## 11. Testing Strategy

### 11.1 Unit Tests
- Configuration parsing and validation
- Health check logic
- Reconnection backoff algorithm

### 11.2 Integration Tests
- Port-forward establishment
- Auto-reconnection scenarios
- Multi-cluster handling

### 11.3 Manual Testing
- Pod restart scenarios
- Network interruption
- Cluster downtime
- TUI rendering

## 12. Deployment

### 12.1 Binary Distribution
- Single statically-linked binary
- No external dependencies
- Cross-platform support (Linux, macOS, Windows)

### 12.2 Configuration
- Default config path: `./config.yaml`
- Support `-config` flag for custom path
- Environment variable support for overrides

## 13. Success Criteria

- ✅ Successfully manages 10+ concurrent port-forwards
- ✅ Auto-reconnects within 10 seconds of failure
- ✅ TUI updates in real-time
- ✅ Handles pod restarts seamlessly
- ✅ Clean shutdown with no orphaned processes
- ✅ Clear documentation and examples