package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// ForwardState represents the state of a port-forward
type ForwardState string

const (
	StateStarting     ForwardState = "starting"
	StateActive       ForwardState = "active"
	StateReconnecting ForwardState = "reconnecting"
	StateFailed       ForwardState = "failed"
	StateStopped      ForwardState = "stopped"
)

// BackupState represents the state of a database backup
type BackupState string

const (
	BackupNone      BackupState = ""
	BackupPending   BackupState = "pending"
	BackupRunning   BackupState = "running"
	BackupCompleted BackupState = "completed"
	BackupFailed    BackupState = "failed"
)

// PortForward manages a single port-forward connection
type PortForward struct {
	Config      ForwardConfig
	ClusterName string
	State       ForwardState
	Error       string
	LastCheck   time.Time
	ReconnectAt time.Time
	RetryCount  int

	// Backup status
	BackupState  BackupState
	BackupError  string
	BackupTime   time.Time
	BackupSizeMB float64

	mu         sync.RWMutex
	client     *kubernetes.Clientset
	restConfig *rest.Config
	stopChan   chan struct{}
	readyChan  chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
}

// PortForwardManager manages all port-forwards
type PortForwardManager struct {
	forwards   []*PortForward
	config     *Config
	mu         sync.RWMutex
	updateChan chan *PortForward
}

// NewPortForwardManager creates a new port-forward manager
func NewPortForwardManager(config *Config) *PortForwardManager {
	return &PortForwardManager{
		forwards:   make([]*PortForward, 0),
		config:     config,
		updateChan: make(chan *PortForward, 100),
	}
}

// Initialize sets up all port-forwards from configuration
func (m *PortForwardManager) Initialize() error {
	for _, cluster := range m.config.Clusters {
		// Load kubeconfig for this cluster
		restConfig, clientset, err := loadKubeconfig(cluster.Kubeconfig, cluster.Context)
		if err != nil {
			return fmt.Errorf("failed to load kubeconfig for cluster %s: %w", cluster.Name, err)
		}

		// Create port-forward instances
		for _, fwdConfig := range cluster.Forwards {
			ctx, cancel := context.WithCancel(context.Background())
			pf := &PortForward{
				Config:      fwdConfig,
				ClusterName: cluster.Name,
				State:       StateStarting,
				client:      clientset,
				restConfig:  restConfig,
				stopChan:    make(chan struct{}),
				readyChan:   make(chan struct{}),
				ctx:         ctx,
				cancel:      cancel,
			}
			m.forwards = append(m.forwards, pf)
		}
	}

	return nil
}

// Start begins all port-forwards and monitoring
func (m *PortForwardManager) Start() {
	// Start each port-forward
	for _, pf := range m.forwards {
		go m.runPortForward(pf)
	}

	// Start health monitor
	go m.healthMonitor()
}

// runPortForward manages the lifecycle of a single port-forward
func (m *PortForwardManager) runPortForward(pf *PortForward) {
	for {
		select {
		case <-pf.ctx.Done():
			pf.setState(StateStopped)
			m.notifyUpdate(pf)
			return
		default:
			if err := m.establishPortForward(pf); err != nil {
				pf.setError(err.Error())
				pf.setState(StateReconnecting)
				m.notifyUpdate(pf)

				// Calculate backoff delay
				delay := m.calculateBackoff(pf.RetryCount)
				pf.mu.Lock()
				pf.ReconnectAt = time.Now().Add(delay)
				pf.RetryCount++
				pf.mu.Unlock()

				slog.Warn("Port-forward failed, will retry",
					"cluster", pf.ClusterName,
					"namespace", pf.Config.Namespace,
					"service", pf.Config.Service,
					"error", err.Error(),
					"retry_in", delay,
					"retry_count", pf.RetryCount,
				)

				select {
				case <-time.After(delay):
					continue
				case <-pf.ctx.Done():
					return
				}
			}
		}
	}
}

// establishPortForward creates a port-forward connection
func (m *PortForwardManager) establishPortForward(pf *PortForward) error {
	// Find the target pod
	podName, err := m.findPod(pf)
	if err != nil {
		return fmt.Errorf("failed to find pod: %w", err)
	}

	// Create port-forward request
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward",
		pf.Config.Namespace, podName)

	hostIP := pf.restConfig.Host
	serverURL, err := url.Parse(hostIP)
	if err != nil {
		return fmt.Errorf("failed to parse API server URL: %w", err)
	}
	serverURL.Path = path

	transport, upgrader, err := spdy.RoundTripperFor(pf.restConfig)
	if err != nil {
		return fmt.Errorf("failed to create SPDY round tripper: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", serverURL)

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})

	ports := []string{fmt.Sprintf("%d:%d", pf.Config.LocalPort, pf.Config.RemotePort)}

	fw, err := portforward.New(dialer, ports, stopChan, readyChan, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create port forwarder: %w", err)
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- fw.ForwardPorts()
	}()

	// Wait for ready or error
	select {
	case <-readyChan:
		pf.setState(StateActive)
		pf.setError("")
		pf.mu.Lock()
		pf.RetryCount = 0
		pf.mu.Unlock()
		m.notifyUpdate(pf)

		slog.Info("Port-forward established",
			"cluster", pf.ClusterName,
			"namespace", pf.Config.Namespace,
			"service", pf.Config.Service,
			"local_port", pf.Config.LocalPort,
			"remote_port", pf.Config.RemotePort,
		)

		// Wait for error or stop
		select {
		case err := <-errChan:
			if err != nil {
				return fmt.Errorf("port-forward error: %w", err)
			}
			return fmt.Errorf("port-forward closed unexpectedly")
		case <-pf.ctx.Done():
			close(stopChan)
			return nil
		}

	case err := <-errChan:
		return err
	case <-time.After(30 * time.Second):
		close(stopChan)
		return fmt.Errorf("timeout waiting for port-forward to be ready")
	}
}

// findPod finds the appropriate pod for port-forwarding
func (m *PortForwardManager) findPod(pf *PortForward) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if pf.Config.Type == "pod" {
		// Direct pod reference
		pod, err := pf.client.CoreV1().Pods(pf.Config.Namespace).Get(ctx, pf.Config.Service, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if pod.Status.Phase != corev1.PodRunning {
			return "", fmt.Errorf("pod is not running: %s", pod.Status.Phase)
		}
		return pod.Name, nil
	}

	// Service reference - find pod via selector
	svc, err := pf.client.CoreV1().Services(pf.Config.Namespace).Get(ctx, pf.Config.Service, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	// List pods matching service selector
	selector := metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: svc.Spec.Selector})
	pods, err := pf.client.CoreV1().Pods(pf.Config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return "", err
	}

	// Find first running pod
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Name, nil
		}
	}

	return "", fmt.Errorf("no running pods found for service %s", pf.Config.Service)
}

// healthMonitor continuously checks port-forward health
func (m *PortForwardManager) healthMonitor() {
	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.RLock()
		forwards := make([]*PortForward, len(m.forwards))
		copy(forwards, m.forwards)
		m.mu.RUnlock()

		for _, pf := range forwards {
			go m.checkHealth(pf)
		}
	}
}

// checkHealth checks if a port-forward is healthy
func (m *PortForwardManager) checkHealth(pf *PortForward) {
	pf.mu.Lock()
	pf.LastCheck = time.Now()
	currentState := pf.State
	pf.mu.Unlock()

	// Only check active forwards
	if currentState != StateActive {
		return
	}

	// Try to connect to local port
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", pf.Config.LocalPort), 2*time.Second)
	if err != nil {
		slog.Warn("Health check failed",
			"cluster", pf.ClusterName,
			"namespace", pf.Config.Namespace,
			"service", pf.Config.Service,
			"error", err.Error(),
		)

		// Trigger reconnection by canceling context
		pf.cancel()

		// Create new context for next attempt
		ctx, cancel := context.WithCancel(context.Background())
		pf.mu.Lock()
		pf.ctx = ctx
		pf.cancel = cancel
		pf.mu.Unlock()

		return
	}
	conn.Close()
}

// calculateBackoff returns the delay for the next reconnection attempt
func (m *PortForwardManager) calculateBackoff(retryCount int) time.Duration {
	if retryCount == 0 {
		return m.config.ReconnectDelay
	}

	// Exponential backoff: 2^n seconds, max 60 seconds
	delay := time.Duration(1<<uint(retryCount)) * time.Second
	if delay > 60*time.Second {
		delay = 60 * time.Second
	}
	return delay
}

// GetForwards returns all port-forwards
func (m *PortForwardManager) GetForwards() []*PortForward {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*PortForward, len(m.forwards))
	copy(result, m.forwards)
	return result
}

// GetUpdateChannel returns the channel for receiving updates
func (m *PortForwardManager) GetUpdateChannel() <-chan *PortForward {
	return m.updateChan
}

// Stop gracefully stops all port-forwards
func (m *PortForwardManager) Stop() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pf := range m.forwards {
		pf.cancel()
	}
}

// notifyUpdate sends an update notification
func (m *PortForwardManager) notifyUpdate(pf *PortForward) {
	select {
	case m.updateChan <- pf:
	default:
		// Channel full, skip update
	}
}

// setState updates the port-forward state
func (pf *PortForward) setState(state ForwardState) {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	pf.State = state
}

// setError updates the error message
func (pf *PortForward) setError(err string) {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	pf.Error = err
}

// setBackupState updates the backup state
func (pf *PortForward) setBackupState(state BackupState) {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	pf.BackupState = state
}

// setBackupError updates the backup error message
func (pf *PortForward) setBackupError(err string) {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	pf.BackupError = err
}

// setBackupCompleted marks backup as completed with metadata
func (pf *PortForward) setBackupCompleted(sizeMB float64) {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	pf.BackupState = BackupCompleted
	pf.BackupTime = time.Now()
	pf.BackupSizeMB = sizeMB
	pf.BackupError = ""
}

// GetState returns the current state (thread-safe)
func (pf *PortForward) GetState() ForwardState {
	pf.mu.RLock()
	defer pf.mu.RUnlock()
	return pf.State
}

// GetError returns the current error (thread-safe)
func (pf *PortForward) GetError() string {
	pf.mu.RLock()
	defer pf.mu.RUnlock()
	return pf.Error
}

// loadKubeconfig loads a kubeconfig file and returns a REST config and clientset
func loadKubeconfig(kubeconfigPath, context string) (*rest.Config, *kubernetes.Clientset, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	configOverrides := &clientcmd.ConfigOverrides{}

	if context != "" {
		configOverrides.CurrentContext = context
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}

	return config, clientset, nil
}
