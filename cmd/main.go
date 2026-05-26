package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"fluid/agents/core/enroll"
	coreversion "fluid/agents/core/version"
	"fluid/agents/proxmox/internal/agent"
	"fluid/agents/proxmox/internal/config"
)

const (
	envEnrollmentToken       = "FLUID_ENROLLMENT_TOKEN"
	envControlplaneHTTPBase  = "FLUID_CONTROLPLANE_HTTP_BASE"
	envControlplaneWebSocket = "FLUID_CONTROLPLANE_WEBSOCKET_URL"
)

func enrollmentTokenForLog(secret string) string {
	s := strings.TrimSpace(secret)
	if s == "" {
		return "empty"
	}
	return fmt.Sprintf("present (length=%d)", len(s))
}

func main() {
	coreversion.ExitIfVersionOnly(Version)

	configPath := flag.String("config", "config/agent.yml", "Path to agent YAML configuration")
	credentialsPath := flag.String("credentials", "/etc/fluid/proxmox/credentials.yaml", "Durable credentials (organization_uuid + connection token)")
	enrollmentEnvPath := flag.String("enrollment-env", "/etc/fluid/proxmox/enrollment.env", "systemd EnvironmentFile path removed after enrollment when FLUID_ENROLL_PURGE_ENROLLMENT_SOURCES is true")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	if err := config.MergeCredentialsFromFile(cfg, *credentialsPath); err != nil {
		log.Fatalf("failed to merge credentials: %v", err)
	}

	if ws := strings.TrimSpace(os.Getenv(envControlplaneWebSocket)); ws != "" {
		cfg.Controlplane.WebSocketURL = ws
	}

	enrollSecret := strings.TrimSpace(os.Getenv(envEnrollmentToken))
	httpBase := strings.TrimSpace(os.Getenv(envControlplaneHTTPBase))

	if strings.TrimSpace(cfg.Controlplane.Token) == "" && enrollSecret != "" {
		if httpBase == "" {
			log.Fatalf("enrollment: %s must be set for POST /api/v1/enrollment/enroll (e.g. via %s)",
				envControlplaneHTTPBase, *enrollmentEnvPath)
		}
		extra, err := enroll.ExtraArgsFromEnv()
		if err != nil {
			log.Fatalf("enrollment: %v", err)
		}
		hostname, _ := os.Hostname()
		override := strings.TrimSpace(os.Getenv(enroll.EnvEnrollmentName))
		if override == "" {
			override = strings.TrimSpace(os.Getenv(enroll.EnvEnrollmentNameDeprecated))
		}
		if override == "" {
			override = strings.TrimSpace(cfg.Enrollment.Name)
		}
		if override == "" {
			override = strings.TrimSpace(cfg.Enrollment.DeprecatedDisplayName)
		}
		enrollName := enroll.ResolveExecutionAgentEnrollmentName(hostname, "proxmox", override)
		log.Printf("enrollment: starting exchange hostname=%q name=%q base_url=%s websocket_url=%s enrollment_token=%s",
			hostname,
			enrollName,
			httpBase,
			strings.TrimSpace(cfg.Controlplane.WebSocketURL),
			enrollmentTokenForLog(enrollSecret),
		)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		res, err := enroll.Exchange(ctx, enroll.Params{
			BaseURL:         httpBase,
			EnrollmentToken: enrollSecret,
			Hostname:        hostname,
			Name:            enrollName,
			AgentType:       "proxmox",
			ExtraArgs:       extra,
		})
		if err != nil {
			log.Fatalf("enrollment: exchange failed: %v", err)
		}
		cfg.Controlplane.OrganizationUUID = res.OrganizationUUID
		cfg.Controlplane.Parameters.OrganizationUUID = res.OrganizationUUID
		cfg.Controlplane.Token = res.ConnectionToken
		cfg.Controlplane.Parameters.Token = res.ConnectionToken
		log.Printf("enrollment: control plane accepted request resource_kind=%q resource_id=%q organization_uuid=%q status=%q use_count=%d",
			strings.TrimSpace(res.ResourceKind),
			strings.TrimSpace(res.ResourceID),
			strings.TrimSpace(res.OrganizationUUID),
			strings.TrimSpace(res.Status),
			res.UseCount,
		)
		if err := config.WriteCredentialsFile(*credentialsPath, cfg.Controlplane); err != nil {
			log.Fatalf("enrollment: persist credentials to %q failed: %v", *credentialsPath, err)
		}
		log.Printf("enrollment: wrote credentials to %q", *credentialsPath)
		if enroll.PurgeEnrollmentSources() {
			p := strings.TrimSpace(*enrollmentEnvPath)
			if p != "" {
				if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
					log.Fatalf("enrollment: remove %q failed: %v", p, err)
				}
				log.Printf("enrollment: removed %q; enrollment complete", p)
			}
		} else {
			log.Printf("enrollment: kept enrollment sources (FLUID_ENROLL_PURGE_ENROLLMENT_SOURCES=false)")
		}
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	execAgent, err := agent.New(cfg)
	if err != nil {
		log.Fatalf("failed to initialize proxmox execution agent: %v", err)
	}

	if err := execAgent.Start(); err != nil {
		log.Fatalf("failed to start proxmox execution agent: %v", err)
	}
	log.Printf("proxmox agent process is running, waiting for signals")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Printf("shutdown signal received")
	execAgent.Stop()
}
