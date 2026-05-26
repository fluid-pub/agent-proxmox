package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Agent        AgentConfig        `yaml:"agent"`
	Enrollment   EnrollmentConfig   `yaml:"enrollment,omitempty"`
	Proxmox      ProxmoxConfig      `yaml:"proxmox"`
	Controlplane ControlplaneConfig `yaml:"controlplane"`
	Logs         LogsConfig         `yaml:"logs"`
	Skills       SkillsConfig       `yaml:"skills"`
}

// EnrollmentConfig holds optional first-boot enrollment overrides (read before POST /enrollment/enroll).
// Name is the control plane Agent.name (JSON "name" on enroll).
// DeprecatedDisplayName maps YAML key display_name (deprecated; prefer name).
type EnrollmentConfig struct {
	Name                  string `yaml:"name"`
	DeprecatedDisplayName string `yaml:"display_name,omitempty"`
}

type AgentConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Mode    string `yaml:"mode"`
}

type ProxmoxConfig struct {
	BaseURL             string `yaml:"base_url"`
	Node                string `yaml:"node"`
	APITokenID          string `yaml:"api_token_id"`
	APITokenSecret      string `yaml:"api_token_secret"`
	InsecureSkipTLS     bool   `yaml:"insecure_skip_tls_verify"`
	TimeoutSeconds      int    `yaml:"timeout_seconds"`
	TaskPollIntervalSec int    `yaml:"task_poll_interval_seconds"`
}

type ControlplaneConfig struct {
	WebSocketURL string `yaml:"websocket_url"`
	APIVersion   string `yaml:"api_version"`
	Parameters   struct {
		OrganizationUUID string `yaml:"organization_uuid"`
		Token            string `yaml:"token"`
	} `yaml:"parameters"`
	OrganizationUUID string `yaml:"organization_uuid"`
	Token            string `yaml:"token"`
}

type SkillsConfig struct {
	Allowed     []string               `yaml:"allowed"`
	Definitions map[string]interface{} `yaml:"definitions,omitempty"`
}

type LogsConfig struct {
	Enabled   *bool  `yaml:"enabled"`
	Verbosity string `yaml:"verbosity"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	cfg.resolveEnvVars()
	cfg.applyDefaults()
	// Do not call Validate here: first-boot enrollment supplies organization_uuid and token after POST /enrollment/enroll.
	return &cfg, nil
}

func (c *Config) resolveEnvVars() {
	c.Enrollment.Name = resolve(c.Enrollment.Name)
	c.Enrollment.DeprecatedDisplayName = resolve(c.Enrollment.DeprecatedDisplayName)
	c.Proxmox.BaseURL = resolve(c.Proxmox.BaseURL)
	c.Proxmox.Node = resolve(c.Proxmox.Node)
	c.Proxmox.APITokenID = resolve(c.Proxmox.APITokenID)
	c.Proxmox.APITokenSecret = resolve(c.Proxmox.APITokenSecret)

	c.Controlplane.WebSocketURL = resolve(c.Controlplane.WebSocketURL)
	c.Controlplane.APIVersion = resolve(c.Controlplane.APIVersion)
	c.Controlplane.Parameters.OrganizationUUID = resolve(c.Controlplane.Parameters.OrganizationUUID)
	c.Controlplane.Parameters.Token = resolve(c.Controlplane.Parameters.Token)
	c.Controlplane.OrganizationUUID = resolve(c.Controlplane.OrganizationUUID)
	c.Controlplane.Token = resolve(c.Controlplane.Token)

	c.materializeProbeStyleFields()
	c.resolveControlplaneFromRegistrationURL()
}

func (c *Config) materializeProbeStyleFields() {
	if c.Controlplane.Parameters.OrganizationUUID != "" {
		c.Controlplane.OrganizationUUID = c.Controlplane.Parameters.OrganizationUUID
	}
	if c.Controlplane.Parameters.Token != "" {
		c.Controlplane.Token = c.Controlplane.Parameters.Token
	}
}

func (c *Config) resolveControlplaneFromRegistrationURL() {
	if c.Controlplane.WebSocketURL == "" {
		return
	}

	u, err := url.Parse(c.Controlplane.WebSocketURL)
	if err != nil {
		return
	}

	q := u.Query()
	if c.Controlplane.OrganizationUUID == "" {
		c.Controlplane.OrganizationUUID = q.Get("organization_uuid")
	}
	if c.Controlplane.Token == "" {
		c.Controlplane.Token = q.Get("token")
	}

	if q.Has("organization_uuid") || q.Has("token") {
		q.Del("organization_uuid")
		q.Del("token")
		u.RawQuery = q.Encode()
		c.Controlplane.WebSocketURL = u.String()
	}
}

func (c *Config) applyDefaults() {
	if c.Logs.Enabled == nil {
		v := true
		c.Logs.Enabled = &v
	}
	if strings.TrimSpace(c.Logs.Verbosity) == "" {
		c.Logs.Verbosity = "normal"
	}
}

func (c *Config) Validate() error {
	if c.Agent.Mode == "" {
		c.Agent.Mode = "execution"
	}
	if c.Agent.Mode != "execution" {
		return fmt.Errorf("agent.mode must be execution")
	}
	if c.Controlplane.WebSocketURL == "" || c.Controlplane.OrganizationUUID == "" || c.Controlplane.Token == "" {
		return fmt.Errorf("controlplane websocket_url, organization_uuid and token are required")
	}
	if c.Proxmox.BaseURL == "" || c.Proxmox.APITokenID == "" || c.Proxmox.APITokenSecret == "" {
		return fmt.Errorf("proxmox base_url, api_token_id and api_token_secret are required")
	}
	if len(c.Skills.Allowed) == 0 {
		return fmt.Errorf("skills.allowed must contain at least one skill")
	}
	if c.Proxmox.TimeoutSeconds <= 0 {
		c.Proxmox.TimeoutSeconds = 120
	}
	if c.Proxmox.TaskPollIntervalSec <= 0 {
		c.Proxmox.TaskPollIntervalSec = 2
	}
	if _, err := url.ParseRequestURI(c.Proxmox.BaseURL); err != nil {
		return fmt.Errorf("proxmox base_url is invalid: %w", err)
	}
	return nil
}

func resolve(v string) string {
	if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
		key := strings.TrimSuffix(strings.TrimPrefix(v, "${"), "}")
		return os.Getenv(key)
	}
	return v
}

func ParseInt(v interface{}, fallback int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return i
		}
	}
	return fallback
}

func ParseBool(v interface{}, fallback bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(t))
		if err == nil {
			return b
		}
	}
	return fallback
}
