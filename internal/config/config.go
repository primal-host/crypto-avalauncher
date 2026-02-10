package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const Version = "0.2.0"

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	DBSSLMode  string
	ListenAddr string
	AdminKey   string

	// Docker / AvalancheGo
	DockerHost     string // DOCKER_HOST, default empty (unix socket)
	AvagoImage     string // AVAGO_IMAGE, default "avaplatform/avalanchego:latest"
	AvagoNetwork   string // AVAGO_NETWORK, default "mainnet"
	AvaxDockerNet  string // AVAX_DOCKER_NETWORK, default "avax"
	HealthInterval string // HEALTH_INTERVAL, default "30s"
}

// Load reads configuration from environment variables.
// Supports _FILE suffix for Docker secrets (e.g. DB_PASSWORD_FILE).
func Load() (*Config, error) {
	c := &Config{
		DBHost:         envOrDefault("DB_HOST", "localhost"),
		DBPort:         envOrDefault("DB_PORT", "5432"),
		DBName:         envOrDefault("DB_NAME", "avalauncher"),
		DBUser:         envOrDefault("DB_USER", "dba_avalauncher"),
		DBSSLMode:      envOrDefault("DB_SSLMODE", "disable"),
		ListenAddr:     envOrDefault("LISTEN_ADDR", ":4321"),
		DockerHost:     os.Getenv("DOCKER_HOST"),
		AvagoImage:     envOrDefault("AVAGO_IMAGE", "avaplatform/avalanchego:latest"),
		AvagoNetwork:   envOrDefault("AVAGO_NETWORK", "mainnet"),
		AvaxDockerNet:  envOrDefault("AVAX_DOCKER_NETWORK", "avax"),
		HealthInterval: envOrDefault("HEALTH_INTERVAL", "30s"),
	}

	pw, err := envOrFile("DB_PASSWORD")
	if err != nil {
		return nil, fmt.Errorf("DB_PASSWORD: %w", err)
	}
	c.DBPassword = pw

	key, err := envOrFile("ADMIN_KEY")
	if err != nil {
		return nil, fmt.Errorf("ADMIN_KEY: %w", err)
	}
	c.AdminKey = key

	return c, nil
}

// DSN returns a PostgreSQL connection string.
func (c *Config) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

// Cluster represents the declarative cluster configuration from cluster.yaml.
type Cluster struct {
	Network string       `yaml:"network"`
	Hosts   []HostConfig `yaml:"hosts"`
	Nodes   []NodeConfig `yaml:"nodes"`
	L1s     []L1Config   `yaml:"l1s"`
}

type HostConfig struct {
	Name string `yaml:"name"`
	SSH  string `yaml:"ssh"`
}

type NodeConfig struct {
	Name        string `yaml:"name"`
	Host        string `yaml:"host"`
	Image       string `yaml:"image"`
	HTTPPort    int    `yaml:"http_port"`
	StakingPort int    `yaml:"staking_port"`
}

type L1Config struct {
	Name         string   `yaml:"name"`
	VM           string   `yaml:"vm"`
	Validators   []string `yaml:"validators"`
}

// LoadCluster reads and parses a cluster.yaml file.
func LoadCluster(path string) (*Cluster, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cluster config: %w", err)
	}
	var c Cluster
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse cluster config: %w", err)
	}
	return &c, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envOrFile reads a value from env var KEY, or from a file at KEY_FILE.
func envOrFile(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	fileKey := key + "_FILE"
	if path := os.Getenv(fileKey); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", fileKey, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return "", nil
}
