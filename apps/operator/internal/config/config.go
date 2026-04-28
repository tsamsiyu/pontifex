package config

import (
	"fmt"
	"os"

	"go.uber.org/config"
)

// OperatorConfig is loaded once at startup. ASN is required (no default).
type OperatorConfig struct {
	// Namespace is where the operator runs and where per-overlay Secrets and
	// agent Deployments live.
	Namespace string `yaml:"namespace"`

	// ASN is the cluster's BGP autonomous system number, used as the prefix
	// of every per-overlay community.
	ASN uint32 `yaml:"asn"`

	// AgentImage is the container image used for both gateway and internal
	// agent Deployments.
	AgentImage string `yaml:"agentImage"`

	// GatewayNodeLabel is the label whose value ("primary" / "secondary")
	// distinguishes the two gateway nodes.
	GatewayNodeLabel string `yaml:"gatewayNodeLabel"`

	LogLevel string `yaml:"logLevel"`
}

// Load reads and validates the operator config from a YAML file with env
// expansion.
func Load(path string) (OperatorConfig, error) {
	provider, err := config.NewYAML(
		config.File(path),
		config.Expand(envLookup),
	)
	if err != nil {
		return OperatorConfig{}, fmt.Errorf("load operator config from %q: %w", path, err)
	}
	var cfg OperatorConfig
	if err := provider.Get("").Populate(&cfg); err != nil {
		return OperatorConfig{}, fmt.Errorf("populate operator config: %w", err)
	}
	applyDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return OperatorConfig{}, err
	}
	return cfg, nil
}

func applyDefaults(cfg *OperatorConfig) {
	if cfg.Namespace == "" {
		cfg.Namespace = "pontifex-system"
	}
	if cfg.AgentImage == "" {
		cfg.AgentImage = "ghcr.io/tsamsiyu/pontifex-agent:latest"
	}
	if cfg.GatewayNodeLabel == "" {
		cfg.GatewayNodeLabel = "pontifex.io/gateway-role"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
}

func validate(cfg *OperatorConfig) error {
	if cfg.ASN == 0 {
		return fmt.Errorf("asn is required (set PONTIFEX_ASN)")
	}
	return nil
}

func envLookup(name string) (string, bool) {
	return os.LookupEnv(name)
}
