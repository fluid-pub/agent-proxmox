package agent

import (
	"fmt"
	"strconv"
	"strings"

	coreexec "fluid/agents/core/execution"
	"fluid/agents/core/skillresult"
	"fluid/agents/proxmox/internal/config"
	"fluid/agents/proxmox/internal/proxmox"
)

type proxmoxClient interface {
	CloneAndStartVM(req proxmox.CloneRequest) (proxmox.CloneResult, error)
	NextVMID() (int, error)
	ListVMs(node string) ([]proxmox.VMInfo, error)
	DeleteVM(node string, vmid int) (string, error)
	GetVMStatus(node string, vmid int) (string, error)
}

type Agent struct {
	cfg     *config.Config
	core    *coreexec.Agent
	client  proxmoxClient
	allowed map[string]struct{}
}

func New(cfg *config.Config) (*Agent, error) {
	allowed := make(map[string]struct{}, len(cfg.Skills.Allowed))
	for _, s := range cfg.Skills.Allowed {
		allowed[strings.TrimSpace(s)] = struct{}{}
	}

	client, err := proxmox.NewClient(
		cfg.Proxmox.BaseURL,
		cfg.Proxmox.APITokenID,
		cfg.Proxmox.APITokenSecret,
		cfg.Proxmox.InsecureSkipTLS,
		cfg.Proxmox.TimeoutSeconds,
		cfg.Proxmox.TaskPollIntervalSec,
	)
	if err != nil {
		return nil, err
	}

	return &Agent{
		cfg:     cfg,
		client:  client,
		allowed: allowed,
	}, nil
}

func (a *Agent) Start() error {
	a.core = coreexec.New(coreexec.Config{
		WebSocketURL:     a.cfg.Controlplane.WebSocketURL,
		OrganizationUUID: a.cfg.Controlplane.OrganizationUUID,
		Token:            a.cfg.Controlplane.Token,
		Name:             "proxmox",
		AllowedSkills:    len(a.allowed),
		LogEventsEnabled: a.cfg.Logs.Enabled == nil || *a.cfg.Logs.Enabled,
		LogVerbosity:     a.cfg.Logs.Verbosity,
		RuntimeConfig:    runtimeConfigForControlPlane(a.cfg),
	}, a.execute)
	return a.core.Start()
}

func (a *Agent) Stop() {
	if a.core != nil {
		a.core.Stop()
	}
}

func runtimeConfigForControlPlane(cfg *config.Config) map[string]interface{} {
	skills := map[string]interface{}{
		"allowed": cfg.Skills.Allowed,
	}
	if len(cfg.Skills.Definitions) > 0 {
		skills["definitions"] = cfg.Skills.Definitions
	}
	return map[string]interface{}{
		"agent": map[string]interface{}{
			"mode":    cfg.Agent.Mode,
			"name":    cfg.Agent.Name,
			"version": cfg.Agent.Version,
		},
		"skills": skills,
	}
}

func (a *Agent) execute(skill string, payload map[string]interface{}, _ map[string]interface{}) (map[string]interface{}, error) {
	if _, ok := a.allowed[skill]; !ok {
		return nil, fmt.Errorf("skill not allowed: %s", skill)
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}

	switch skill {
	case "proxmox.vm.list":
		node := strings.TrimSpace(payloadString(payload["node"]))
		vms, err := a.client.ListVMs(node)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]interface{}, 0, len(vms))
		for _, vm := range vms {
			label := fmt.Sprintf("%d - %s (%s)", vm.VMID, vm.Name, vm.Node)
			value := fmt.Sprintf("%s:%d", vm.Node, vm.VMID)
			items = append(items, map[string]interface{}{
				"value":  value,
				"label":  label,
				"node":   vm.Node,
				"vmid":   vm.VMID,
				"name":   vm.Name,
				"status": vm.Status,
			})
		}
		return skillresult.Success(map[string]interface{}{"items": items}), nil

	case "proxmox.vm.delete":
		node, vmid, err := deleteTargetFromPayload(payload)
		if err != nil {
			return nil, err
		}
		deleteUPID, err := a.client.DeleteVM(node, vmid)
		if err != nil {
			return nil, err
		}
		return skillresult.Success(map[string]interface{}{
			"provider":         "proxmox",
			"node":             node,
			"vmid":             vmid,
			"delete_task_upid": deleteUPID,
			"provision_status": "deleted",
		}), nil

	case "proxmox.vm.status":
		node, vmid, err := deleteTargetFromPayload(payload)
		if err != nil {
			return nil, err
		}
		status, err := a.client.GetVMStatus(node, vmid)
		if err != nil {
			return nil, err
		}
		return skillresult.Success(map[string]interface{}{
			"provider": "proxmox",
			"node":     node,
			"vmid":     vmid,
			"status":   status,
		}), nil

	case "proxmox.vm.clone_and_start":
		req, err := cloneRequestFromPayload(a.cfg, payload)
		if err != nil {
			return nil, err
		}
		res, err := a.client.CloneAndStartVM(req)
		if err != nil {
			return nil, err
		}
		return skillresult.Success(map[string]interface{}{
			"provider":         "proxmox",
			"node":             req.Node,
			"template_vmid":    req.TemplateVMID,
			"new_vmid":         res.NewVMID,
			"name":             req.Name,
			"clone_task_upid":  res.CloneTaskUPID,
			"start_task_upid":  res.StartTaskUPID,
			"state":            "started",
			"provision_status": "completed",
		}), nil
	default:
		return nil, fmt.Errorf("unsupported skill: %s", skill)
	}
}

func cloneRequestFromPayload(cfg *config.Config, payload map[string]interface{}) (proxmox.CloneRequest, error) {
	node := strings.TrimSpace(payloadString(payload["node"]))
	if node == "" {
		node = strings.TrimSpace(cfg.Proxmox.Node)
	}
	if node == "" {
		return proxmox.CloneRequest{}, fmt.Errorf("node is required")
	}

	templateVMID := config.ParseInt(payload["template_vmid"], 0)
	if templateVMID <= 0 {
		return proxmox.CloneRequest{}, fmt.Errorf("template_vmid must be a positive integer")
	}

	newVMID := config.ParseInt(payload["new_vmid"], 0)

	name := strings.TrimSpace(payloadString(payload["name"]))
	if name == "" {
		return proxmox.CloneRequest{}, fmt.Errorf("name is required")
	}

	return proxmox.CloneRequest{
		Node:                    node,
		TemplateVMID:            templateVMID,
		NewVMID:                 newVMID,
		Name:                    name,
		TargetNode:              strings.TrimSpace(payloadString(payload["target_node"])),
		Storage:                 strings.TrimSpace(payloadString(payload["storage"])),
		Pool:                    strings.TrimSpace(payloadString(payload["pool"])),
		Full:                    config.ParseBool(payload["full"], true),
		MemoryMB:                config.ParseInt(payload["memory"], 0),
		Cores:                   config.ParseInt(payload["cores"], 0),
		Sockets:                 config.ParseInt(payload["sockets"], 0),
		IPConfig0:               proxmoxIPConfig0FromPayload(payload),
		CloudInitUserData:       strings.TrimSpace(payloadString(payload["cloud_init_user_data"])),
		CloudInitSnippetStorage: strings.TrimSpace(payloadString(payload["cloud_init_snippet_storage"])),
		CloudInitDiskStorage:    strings.TrimSpace(payloadString(payload["cloud_init_disk_storage"])),
	}, nil
}

// proxmoxIPConfig0FromPayload builds the Proxmox API ipconfig0 value (cloud-init network for net0).
func proxmoxIPConfig0FromPayload(payload map[string]interface{}) string {
	ip := strings.TrimSpace(payloadString(payload["ip_address"]))
	if ip == "" {
		return ""
	}
	if strings.EqualFold(ip, "dhcp") {
		return "ip=dhcp"
	}
	gw := strings.TrimSpace(payloadString(payload["network_gateway"]))
	prefix := config.ParseInt(payload["ip_prefix"], 24)
	if prefix <= 0 || prefix > 32 {
		prefix = 24
	}
	var host, cidr string
	if idx := strings.Index(ip, "/"); idx >= 0 {
		host = strings.TrimSpace(ip[:idx])
		cidr = strings.TrimSpace(ip[idx+1:])
		if cidr == "" {
			cidr = strconv.Itoa(prefix)
		}
	} else {
		host = ip
		cidr = strconv.Itoa(prefix)
	}
	if host == "" {
		return ""
	}
	if gw != "" {
		return fmt.Sprintf("ip=%s/%s,gw=%s", host, cidr, gw)
	}
	return fmt.Sprintf("ip=%s/%s", host, cidr)
}

func payloadString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func deleteTargetFromPayload(payload map[string]interface{}) (string, int, error) {
	if vmRef := strings.TrimSpace(payloadString(payload["vm_ref"])); vmRef != "" {
		parts := strings.Split(vmRef, ":")
		if len(parts) != 2 {
			return "", 0, fmt.Errorf("vm_ref must be in format node:vmid")
		}
		node := strings.TrimSpace(parts[0])
		vmid := config.ParseInt(strings.TrimSpace(parts[1]), 0)
		if node == "" || vmid <= 0 {
			return "", 0, fmt.Errorf("vm_ref must contain valid node and vmid")
		}
		return node, vmid, nil
	}

	node := strings.TrimSpace(payloadString(payload["node"]))
	vmid := config.ParseInt(payload["vmid"], 0)
	if node == "" {
		return "", 0, fmt.Errorf("node is required")
	}
	if vmid <= 0 {
		return "", 0, fmt.Errorf("vmid must be a positive integer")
	}
	return node, vmid, nil
}
