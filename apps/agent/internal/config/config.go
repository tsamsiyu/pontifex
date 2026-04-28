package config

import (
	"fmt"

	"go.uber.org/config"
)

// GatewayConfig is the per-gateway-pod configuration. PublicIP and per-overlay
// WireGuard private keys are intentionally absent: the public IP is read from
// NetworkOverlay.status.gateways (operator-managed), and private keys are
// loaded from <WGKeyDir>/<overlay>/private at reconcile time.
type GatewayConfig struct {
	NodeName     string `yaml:"nodeName"`
	IsSecondary  bool   `yaml:"isSecondary"`
	WGListenPort int    `yaml:"wgListenPort"`
	WGKeyDir     string `yaml:"wgKeyDir"`
	LogLevel     string `yaml:"logLevel"`
}

// InternalNodeConfig is the per-internal-agent-pod configuration. Gateway BGP
// endpoints are not stored here; they come from
// NetworkOverlay.status.gateways.
type InternalNodeConfig struct {
	NodeName string `yaml:"nodeName"`
	Firewall string `yaml:"firewall"` // "auto" | "iptables" | "nftables"
	LogLevel string `yaml:"logLevel"`
}

// LoadGateway loads a GatewayConfig from the given YAML path with env
// expansion (go.uber.org/config).
func LoadGateway(path string) (GatewayConfig, error) {
	provider, err := config.NewYAML(
		config.File(path),
		config.Expand(envLookup),
	)
	if err != nil {
		return GatewayConfig{}, fmt.Errorf("load gateway config from %q: %w", path, err)
	}
	var cfg GatewayConfig
	if err := provider.Get("").Populate(&cfg); err != nil {
		return GatewayConfig{}, fmt.Errorf("populate gateway config: %w", err)
	}
	applyGatewayDefaults(&cfg)
	return cfg, nil
}

// LoadInternalNode loads an InternalNodeConfig from the given YAML path with
// env expansion.
func LoadInternalNode(path string) (InternalNodeConfig, error) {
	provider, err := config.NewYAML(
		config.File(path),
		config.Expand(envLookup),
	)
	if err != nil {
		return InternalNodeConfig{}, fmt.Errorf("load internal-node config from %q: %w", path, err)
	}
	var cfg InternalNodeConfig
	if err := provider.Get("").Populate(&cfg); err != nil {
		return InternalNodeConfig{}, fmt.Errorf("populate internal-node config: %w", err)
	}
	applyInternalNodeDefaults(&cfg)
	return cfg, nil
}

func applyGatewayDefaults(cfg *GatewayConfig) {
	if cfg.WGListenPort == 0 {
		cfg.WGListenPort = 51820
	}
	if cfg.WGKeyDir == "" {
		cfg.WGKeyDir = "/etc/pontifex/wg"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
}

func applyInternalNodeDefaults(cfg *InternalNodeConfig) {
	if cfg.Firewall == "" {
		cfg.Firewall = "auto"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
}
