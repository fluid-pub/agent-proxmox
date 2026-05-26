package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type credentialsFile struct {
	Controlplane ControlplaneConfig `yaml:"controlplane"`
}

// MergeCredentialsFromFile loads durable control plane credentials written after enrollment.
func MergeCredentialsFromFile(cfg *Config, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read credentials file: %w", err)
	}
	var f credentialsFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("decode credentials file: %w", err)
	}
	cp := f.Controlplane
	if strings.TrimSpace(cp.WebSocketURL) != "" {
		cfg.Controlplane.WebSocketURL = strings.TrimSpace(cp.WebSocketURL)
	}
	if strings.TrimSpace(cp.APIVersion) != "" {
		cfg.Controlplane.APIVersion = strings.TrimSpace(cp.APIVersion)
	}
	if strings.TrimSpace(cp.OrganizationUUID) != "" {
		u := strings.TrimSpace(cp.OrganizationUUID)
		cfg.Controlplane.OrganizationUUID = u
		cfg.Controlplane.Parameters.OrganizationUUID = u
	}
	if strings.TrimSpace(cp.Parameters.OrganizationUUID) != "" {
		u := strings.TrimSpace(cp.Parameters.OrganizationUUID)
		cfg.Controlplane.Parameters.OrganizationUUID = u
		cfg.Controlplane.OrganizationUUID = u
	}
	if strings.TrimSpace(cp.Token) != "" {
		t := strings.TrimSpace(cp.Token)
		cfg.Controlplane.Token = t
		cfg.Controlplane.Parameters.Token = t
	}
	if strings.TrimSpace(cp.Parameters.Token) != "" {
		t := strings.TrimSpace(cp.Parameters.Token)
		cfg.Controlplane.Parameters.Token = t
		cfg.Controlplane.Token = t
	}
	cfg.materializeProbeStyleFields()
	return nil
}

// WriteCredentialsFile writes durable WebSocket + organization + connection token (0600, atomic replace).
func WriteCredentialsFile(path string, cp ControlplaneConfig) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("credentials path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir credentials dir: %w", err)
	}
	ws := strings.TrimSpace(cp.WebSocketURL)
	org := strings.TrimSpace(cp.OrganizationUUID)
	if org == "" {
		org = strings.TrimSpace(cp.Parameters.OrganizationUUID)
	}
	tok := strings.TrimSpace(cp.Token)
	if tok == "" {
		tok = strings.TrimSpace(cp.Parameters.Token)
	}
	out, err := yaml.Marshal(&credentialsFile{
		Controlplane: ControlplaneConfig{
			WebSocketURL:     ws,
			APIVersion:       strings.TrimSpace(cp.APIVersion),
			OrganizationUUID: org,
			Token:            tok,
			Parameters: struct {
				OrganizationUUID string `yaml:"organization_uuid"`
				Token            string `yaml:"token"`
			}{OrganizationUUID: org, Token: tok},
		},
	})
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".fluid-credentials-*")
	if err != nil {
		return fmt.Errorf("create temp credentials: %w", err)
	}
	tmpName := tmp.Name()
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp credentials: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("sync temp credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp credentials: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace credentials file: %w", err)
	}
	return nil
}
