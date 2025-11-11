package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the main configuration structure
type Config struct {
	CheckInterval  time.Duration   `yaml:"check_interval"`
	ReconnectDelay time.Duration   `yaml:"reconnect_delay"`
	Clusters       []ClusterConfig `yaml:"clusters"`
}

// ClusterConfig represents a Kubernetes cluster configuration
type ClusterConfig struct {
	Name       string          `yaml:"name"`
	Kubeconfig string          `yaml:"kubeconfig"`
	Context    string          `yaml:"context"`
	Forwards   []ForwardConfig `yaml:"forwards"`
}

// ForwardConfig represents a port-forward configuration
type ForwardConfig struct {
	Namespace  string          `yaml:"namespace"`
	Service    string          `yaml:"service"`
	Type       string          `yaml:"type"` // "service" or "pod"
	LocalPort  int             `yaml:"local_port"`
	RemotePort int             `yaml:"remote_port"`
	DBBackup   *DBBackupConfig `yaml:"db_backup,omitempty"`
}

// DBBackupConfig contains database backup configuration
type DBBackupConfig struct {
	// Kubernetes secret-based credentials (preferred for production)
	SecretName   string            `yaml:"secret_name,omitempty"`
	FieldMapping map[string]string `yaml:"field_mapping,omitempty"` // maps config field names to secret keys

	// Direct credentials (useful for development or when secrets aren't available)
	Database string `yaml:"database,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

// LoadConfig loads and validates the configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Set defaults
	if config.CheckInterval == 0 {
		config.CheckInterval = 10 * time.Second
	}
	if config.ReconnectDelay == 0 {
		config.ReconnectDelay = 5 * time.Second
	}

	// Validate configuration
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// validateConfig performs comprehensive validation of the configuration
func validateConfig(config *Config) error {
	if len(config.Clusters) == 0 {
		return fmt.Errorf("no clusters configured")
	}

	clusterNames := make(map[string]bool)
	localPorts := make(map[int]string)

	for i, cluster := range config.Clusters {
		// Validate cluster name uniqueness
		if cluster.Name == "" {
			return fmt.Errorf("cluster at index %d has no name", i)
		}
		if clusterNames[cluster.Name] {
			return fmt.Errorf("duplicate cluster name: %s", cluster.Name)
		}
		clusterNames[cluster.Name] = true

		// Validate kubeconfig file exists
		if cluster.Kubeconfig == "" {
			return fmt.Errorf("cluster '%s' has no kubeconfig path", cluster.Name)
		}
		if _, err := os.Stat(cluster.Kubeconfig); os.IsNotExist(err) {
			return fmt.Errorf("kubeconfig file not found for cluster '%s': %s", cluster.Name, cluster.Kubeconfig)
		}

		// Validate forwards
		if len(cluster.Forwards) == 0 {
			return fmt.Errorf("cluster '%s' has no port-forwards configured", cluster.Name)
		}

		forwardKeys := make(map[string]bool)
		for _, forward := range cluster.Forwards {
			// Validate namespace
			if forward.Namespace == "" {
				return fmt.Errorf("forward in cluster '%s' has no namespace", cluster.Name)
			}

			// Validate service name
			if forward.Service == "" {
				return fmt.Errorf("forward in cluster '%s' has no service/pod name", cluster.Name)
			}

			// Check for duplicate service/namespace combination within cluster
			forwardKey := cluster.Name + "/" + forward.Namespace + "/" + forward.Service
			if forwardKeys[forwardKey] {
				return fmt.Errorf("duplicate forward for service '%s' in namespace '%s' in cluster '%s'",
					forward.Service, forward.Namespace, cluster.Name)
			}
			forwardKeys[forwardKey] = true

			// Validate type
			if forward.Type != "service" && forward.Type != "pod" {
				return fmt.Errorf("forward for '%s/%s' in cluster '%s' has invalid type '%s' (must be 'service' or 'pod')",
					forward.Namespace, forward.Service, cluster.Name, forward.Type)
			}

			// Validate port ranges
			if forward.LocalPort < 1 || forward.LocalPort > 65535 {
				return fmt.Errorf("forward for '%s/%s' in cluster '%s' has invalid local_port: %d (must be 1-65535)",
					forward.Namespace, forward.Service, cluster.Name, forward.LocalPort)
			}
			if forward.RemotePort < 1 || forward.RemotePort > 65535 {
				return fmt.Errorf("forward for '%s/%s' in cluster '%s' has invalid remote_port: %d (must be 1-65535)",
					forward.Namespace, forward.Service, cluster.Name, forward.RemotePort)
			}

			// Check for duplicate local ports
			if existingForward, exists := localPorts[forward.LocalPort]; exists {
				return fmt.Errorf("local port %d is used by both '%s' and '%s/%s/%s'",
					forward.LocalPort, existingForward, cluster.Name, forward.Namespace, forward.Service)
			}
			localPorts[forward.LocalPort] = fmt.Sprintf("%s/%s/%s", cluster.Name, forward.Namespace, forward.Service)
		}
	}

	return nil
}
