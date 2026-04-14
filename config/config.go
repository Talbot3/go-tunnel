// Package config provides configuration management.
package config

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/wld/go-tunnel/protocol"
)

// Config represents the main configuration.
type Config struct {
	Version   string            `yaml:"version"`
	LogLevel  string            `yaml:"log_level"`
	Protocols []ProtocolConfig  `yaml:"protocols"`
	TLS       TLSConfig         `yaml:"tls"`
	Options   map[string]string `yaml:"options"`
}

// ProtocolConfig represents a protocol configuration.
type ProtocolConfig struct {
	Name    string            `yaml:"name"`
	Listen  string            `yaml:"listen"`
	Target  string            `yaml:"target"`
	Enabled bool              `yaml:"enabled"`
	Options map[string]string `yaml:"options"`
}

// TLSConfig represents TLS configuration.
type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
	CAFile   string `yaml:"ca_file"`
}

// Manager handles configuration loading and reloading.
type Manager struct {
	mu     sync.RWMutex
	config *Config
	path   string

	// Callbacks
	onReload func(*Config)
}

// NewManager creates a new configuration manager.
func NewManager() *Manager {
	return &Manager{
		config: &Config{
			Version:  "1.0",
			LogLevel: "info",
			Options:  make(map[string]string),
		},
	}
}

// Load loads configuration from a file.
func (m *Manager) Load(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	m.config = &cfg
	m.path = path
	return nil
}

// Save saves configuration to a file.
func (m *Manager) Save(path string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := yaml.Marshal(m.config)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Get returns the current configuration.
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// SetOnReload sets the reload callback.
func (m *Manager) SetOnReload(cb func(*Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onReload = cb
}

// Reload reloads the configuration from the file.
func (m *Manager) Reload() error {
	if m.path == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	oldConfig := m.config
	m.config = &cfg

	if m.onReload != nil {
		m.onReload(oldConfig)
	}

	return nil
}

// ToProtocolConfigs converts to protocol configs.
func (c *Config) ToProtocolConfigs() []protocol.Config {
	configs := make([]protocol.Config, 0, len(c.Protocols))
	for _, p := range c.Protocols {
		configs = append(configs, protocol.Config{
			Name:    p.Name,
			Listen:  p.Listen,
			Target:  p.Target,
			Enabled: p.Enabled,
			Options: p.Options,
		})
	}
	return configs
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		Version:  "1.0",
		LogLevel: "info",
		Protocols: []ProtocolConfig{
			{
				Name:    "tcp",
				Listen:  ":8080",
				Target:  "127.0.0.1:80",
				Enabled: true,
			},
		},
		Options: make(map[string]string),
	}
}
