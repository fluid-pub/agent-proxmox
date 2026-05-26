package agent

import (
	"testing"

	"fluid/agents/proxmox/internal/config"
	"fluid/agents/proxmox/internal/proxmox"
)

type fakeProxmoxClient struct {
	lastReq        proxmox.CloneRequest
	lastDelNode    string
	lastDelVMID    int
	lastStatusNode string
	lastStatusVMID int
	err            error
	nextID         int
}

func (f *fakeProxmoxClient) CloneAndStartVM(req proxmox.CloneRequest) (proxmox.CloneResult, error) {
	f.lastReq = req
	if f.err != nil {
		return proxmox.CloneResult{}, f.err
	}
	return proxmox.CloneResult{
		CloneTaskUPID: "UPID:clone",
		StartTaskUPID: "UPID:start",
	}, nil
}

func (f *fakeProxmoxClient) NextVMID() (int, error) {
	if f.nextID > 0 {
		return f.nextID, nil
	}
	return 2000, nil
}

func (f *fakeProxmoxClient) ListVMs(node string) ([]proxmox.VMInfo, error) {
	return []proxmox.VMInfo{
		{Node: "pve1", VMID: 1200, Name: "vm-a", Status: "running"},
	}, nil
}

func (f *fakeProxmoxClient) DeleteVM(node string, vmid int) (string, error) {
	f.lastDelNode = node
	f.lastDelVMID = vmid
	if f.err != nil {
		return "", f.err
	}
	return "UPID:delete", nil
}

func (f *fakeProxmoxClient) GetVMStatus(node string, vmid int) (string, error) {
	f.lastStatusNode = node
	f.lastStatusVMID = vmid
	if f.err != nil {
		return "", f.err
	}
	return "running", nil
}

func TestExecuteCloneAndStart(t *testing.T) {
	cfg := &config.Config{
		Proxmox: config.ProxmoxConfig{
			Node: "pve1",
		},
		Skills: config.SkillsConfig{
			Allowed: []string{"proxmox.vm.clone_and_start"},
		},
	}
	fake := &fakeProxmoxClient{}
	a := &Agent{
		cfg:    cfg,
		client: fake,
		allowed: map[string]struct{}{
			"proxmox.vm.clone_and_start": {},
		},
	}

	out, err := a.execute("proxmox.vm.clone_and_start", map[string]interface{}{
		"template_vmid": 9000,
		"new_vmid":      1200,
		"name":          "vm-test",
		"full":          true,
	}, nil)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if out["ok"] != true {
		t.Fatalf("expected ok result")
	}
	if fake.lastReq.Node != "pve1" || fake.lastReq.TemplateVMID != 9000 || fake.lastReq.NewVMID != 1200 {
		t.Fatalf("unexpected request passed to client: %+v", fake.lastReq)
	}
}

func TestExecuteCloneAndStartPassesNetworkAndHardware(t *testing.T) {
	cfg := &config.Config{
		Proxmox: config.ProxmoxConfig{Node: "pve1"},
		Skills:  config.SkillsConfig{Allowed: []string{"proxmox.vm.clone_and_start"}},
	}
	fake := &fakeProxmoxClient{}
	a := &Agent{
		cfg:     cfg,
		client:  fake,
		allowed: map[string]struct{}{"proxmox.vm.clone_and_start": {}},
	}

	_, err := a.execute("proxmox.vm.clone_and_start", map[string]interface{}{
		"template_vmid":           9000,
		"new_vmid":                1200,
		"name":                    "vm-test",
		"full":                    true,
		"memory":                  2048,
		"cores":                   2,
		"sockets":                 1,
		"ip_address":              "10.1.0.222",
		"network_gateway":         "10.1.0.254",
		"ip_prefix":               24,
		"cloud_init_disk_storage": "local-lvm",
	}, nil)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if fake.lastReq.MemoryMB != 2048 || fake.lastReq.Cores != 2 || fake.lastReq.Sockets != 1 {
		t.Fatalf("unexpected hardware in request: %+v", fake.lastReq)
	}
	want := "ip=10.1.0.222/24,gw=10.1.0.254"
	if fake.lastReq.IPConfig0 != want {
		t.Fatalf("unexpected ipconfig0: got %q want %q", fake.lastReq.IPConfig0, want)
	}
	if fake.lastReq.CloudInitDiskStorage != "local-lvm" {
		t.Fatalf("unexpected CloudInitDiskStorage: %q", fake.lastReq.CloudInitDiskStorage)
	}
}

func TestExecuteCloneAndStartPassesCloudInitDiskStorage(t *testing.T) {
	cfg := &config.Config{
		Proxmox: config.ProxmoxConfig{Node: "pve1"},
		Skills:  config.SkillsConfig{Allowed: []string{"proxmox.vm.clone_and_start"}},
	}
	fake := &fakeProxmoxClient{}
	a := &Agent{
		cfg:     cfg,
		client:  fake,
		allowed: map[string]struct{}{"proxmox.vm.clone_and_start": {}},
	}

	_, err := a.execute("proxmox.vm.clone_and_start", map[string]interface{}{
		"template_vmid":           9000,
		"new_vmid":                1200,
		"name":                    "vm-test",
		"full":                    true,
		"ip_address":              "10.1.0.222",
		"network_gateway":         "10.1.0.254",
		"ip_prefix":               24,
		"cloud_init_disk_storage": "ceph-vm",
	}, nil)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if fake.lastReq.CloudInitDiskStorage != "ceph-vm" {
		t.Fatalf("unexpected CloudInitDiskStorage: got %q want ceph-vm", fake.lastReq.CloudInitDiskStorage)
	}
}

func TestExecuteRejectsMissingTemplateVMID(t *testing.T) {
	cfg := &config.Config{
		Proxmox: config.ProxmoxConfig{Node: "pve1"},
		Skills:  config.SkillsConfig{Allowed: []string{"proxmox.vm.clone_and_start"}},
	}
	a := &Agent{
		cfg:     cfg,
		client:  &fakeProxmoxClient{},
		allowed: map[string]struct{}{"proxmox.vm.clone_and_start": {}},
	}

	_, err := a.execute("proxmox.vm.clone_and_start", map[string]interface{}{
		"new_vmid": 1200,
		"name":     "vm-test",
	}, nil)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestExecuteAllowsMissingNewVMID(t *testing.T) {
	cfg := &config.Config{
		Proxmox: config.ProxmoxConfig{Node: "pve1"},
		Skills:  config.SkillsConfig{Allowed: []string{"proxmox.vm.clone_and_start"}},
	}
	fake := &fakeProxmoxClient{}
	a := &Agent{
		cfg:     cfg,
		client:  fake,
		allowed: map[string]struct{}{"proxmox.vm.clone_and_start": {}},
	}

	_, err := a.execute("proxmox.vm.clone_and_start", map[string]interface{}{
		"template_vmid": 9000,
		"name":          "vm-test",
	}, nil)
	if err != nil {
		t.Fatalf("expected no error when new_vmid is omitted, got: %v", err)
	}
}

func TestExecuteListVMs(t *testing.T) {
	cfg := &config.Config{
		Skills: config.SkillsConfig{
			Allowed: []string{"proxmox.vm.list"},
		},
	}
	a := &Agent{
		cfg:     cfg,
		client:  &fakeProxmoxClient{},
		allowed: map[string]struct{}{"proxmox.vm.list": {}},
	}

	out, err := a.execute("proxmox.vm.list", map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("expected ok result")
	}
}

func TestExecuteDeleteVM(t *testing.T) {
	cfg := &config.Config{
		Skills: config.SkillsConfig{
			Allowed: []string{"proxmox.vm.delete"},
		},
	}
	fake := &fakeProxmoxClient{}
	a := &Agent{
		cfg:     cfg,
		client:  fake,
		allowed: map[string]struct{}{"proxmox.vm.delete": {}},
	}

	_, err := a.execute("proxmox.vm.delete", map[string]interface{}{
		"vm_ref": "pve1:1200",
	}, nil)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if fake.lastDelNode != "pve1" || fake.lastDelVMID != 1200 {
		t.Fatalf("unexpected delete target: node=%s vmid=%d", fake.lastDelNode, fake.lastDelVMID)
	}
}

func TestExecuteVMStatus(t *testing.T) {
	cfg := &config.Config{
		Skills: config.SkillsConfig{
			Allowed: []string{"proxmox.vm.status"},
		},
	}
	fake := &fakeProxmoxClient{}
	a := &Agent{
		cfg:     cfg,
		client:  fake,
		allowed: map[string]struct{}{"proxmox.vm.status": {}},
	}

	out, err := a.execute("proxmox.vm.status", map[string]interface{}{
		"vm_ref": "pve1:1200",
	}, nil)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("expected ok result")
	}
	if fake.lastStatusNode != "pve1" || fake.lastStatusVMID != 1200 {
		t.Fatalf("unexpected status target: node=%s vmid=%d", fake.lastStatusNode, fake.lastStatusVMID)
	}
}
