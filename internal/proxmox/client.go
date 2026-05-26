package proxmox

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type CloneRequest struct {
	Node         string
	TemplateVMID int
	NewVMID      int
	Name         string
	TargetNode   string
	Storage      string
	Pool         string
	Full         bool
	// MemoryMB, Cores, Sockets are applied via POST /qemu/{vmid}/config after clone (Proxmox units: memory in MiB).
	MemoryMB int
	Cores    int
	Sockets  int
	// IPConfig0 is the Proxmox cloud-init network string, e.g. ip=10.0.0.5/24,gw=10.0.0.1 or ip=dhcp.
	IPConfig0               string
	CloudInitUserData       string
	CloudInitSnippetStorage string
	// CloudInitDiskStorage is the Proxmox storage id for the cloud-init CD drive (ide2=<id>:cloudinit).
	// Required when IPConfig0 or CloudInitUserData is set unless Storage (clone target) is set for the same purpose.
	CloudInitDiskStorage string
}

type CloneResult struct {
	CloneTaskUPID string
	StartTaskUPID string
	NewVMID       int
}

type VMInfo struct {
	Node   string
	VMID   int
	Name   string
	Status string
}

type Client struct {
	baseURL      string
	tokenID      string
	tokenSecret  string
	pollInterval time.Duration
	httpClient   *http.Client
}

func NewClient(baseURL, tokenID, tokenSecret string, insecureSkipTLS bool, timeoutSeconds, pollIntervalSeconds int) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("proxmox base_url is required")
	}
	if strings.TrimSpace(tokenID) == "" || strings.TrimSpace(tokenSecret) == "" {
		return nil, fmt.Errorf("proxmox api token id and secret are required")
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	if pollIntervalSeconds <= 0 {
		pollIntervalSeconds = 2
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkipTLS,
	}

	return &Client{
		baseURL:      strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
		tokenID:      strings.TrimSpace(tokenID),
		tokenSecret:  strings.TrimSpace(tokenSecret),
		pollInterval: time.Duration(pollIntervalSeconds) * time.Second,
		httpClient: &http.Client{
			Timeout:   time.Duration(timeoutSeconds) * time.Second,
			Transport: transport,
		},
	}, nil
}

func (c *Client) NextVMID() (int, error) {
	u := c.baseURL + "/api2/json/cluster/nextid"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("proxmox returned %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Data interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	switch v := payload.Data.(type) {
	case string:
		id, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, fmt.Errorf("invalid nextid value: %q", v)
		}
		return id, nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("unexpected nextid response type")
	}
}

func (c *Client) CloneAndStartVM(req CloneRequest) (CloneResult, error) {
	if strings.TrimSpace(req.Node) == "" {
		return CloneResult{}, fmt.Errorf("node is required")
	}
	if req.TemplateVMID <= 0 {
		return CloneResult{}, fmt.Errorf("template_vmid must be positive")
	}
	if req.NewVMID <= 0 {
		nextID, err := c.NextVMID()
		if err != nil {
			return CloneResult{}, fmt.Errorf("resolve new_vmid: %w", err)
		}
		req.NewVMID = nextID
	}
	if strings.TrimSpace(req.Name) == "" {
		return CloneResult{}, fmt.Errorf("name is required")
	}

	cloneUPID, err := c.cloneVM(req)
	if err != nil {
		return CloneResult{}, err
	}
	if err := c.waitForTask(req.Node, cloneUPID); err != nil {
		return CloneResult{}, fmt.Errorf("clone task failed: %w", err)
	}

	if err := c.applyPostCloneVMConfig(req); err != nil {
		return CloneResult{}, fmt.Errorf("configure cloned vm: %w", err)
	}

	startUPID, err := c.startVM(req.Node, req.NewVMID)
	if err != nil {
		return CloneResult{}, err
	}
	if err := c.waitForTask(req.Node, startUPID); err != nil {
		return CloneResult{}, fmt.Errorf("start task failed: %w", err)
	}

	return CloneResult{CloneTaskUPID: cloneUPID, StartTaskUPID: startUPID, NewVMID: req.NewVMID}, nil
}

func (c *Client) ListVMs(node string) ([]VMInfo, error) {
	u := c.baseURL + "/api2/json/cluster/resources?type=vm"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("proxmox returned %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	filterNode := strings.TrimSpace(node)
	out := make([]VMInfo, 0, len(payload.Data))
	for _, item := range payload.Data {
		itemType, _ := item["type"].(string)
		if itemType != "qemu" {
			continue
		}
		vmNode, _ := item["node"].(string)
		if filterNode != "" && filterNode != vmNode {
			continue
		}
		vmName, _ := item["name"].(string)
		vmStatus, _ := item["status"].(string)
		vmid := 0
		switch v := item["vmid"].(type) {
		case float64:
			vmid = int(v)
		case int:
			vmid = v
		}
		if vmid <= 0 {
			continue
		}
		out = append(out, VMInfo{Node: vmNode, VMID: vmid, Name: vmName, Status: vmStatus})
	}
	return out, nil
}

func (c *Client) DeleteVM(node string, vmid int) (string, error) {
	if strings.TrimSpace(node) == "" {
		return "", fmt.Errorf("node is required")
	}
	if vmid <= 0 {
		return "", fmt.Errorf("vmid must be positive")
	}

	status, err := c.vmStatus(node, vmid)
	if err != nil {
		return "", fmt.Errorf("check vm status: %w", err)
	}
	if strings.EqualFold(status, "running") {
		stopUPID, err := c.stopVM(node, vmid)
		if err != nil {
			return "", fmt.Errorf("stop vm before delete: %w", err)
		}
		if err := c.waitForTask(node, stopUPID); err != nil {
			return "", fmt.Errorf("stop task failed: %w", err)
		}
	}

	path := fmt.Sprintf("/nodes/%s/qemu/%d", url.PathEscape(node), vmid)
	upid, err := c.deleteForTaskUPID(path)
	if err != nil {
		return "", err
	}
	if err := c.waitForTask(node, upid); err != nil {
		return "", fmt.Errorf("delete task failed: %w", err)
	}
	return upid, nil
}

func (c *Client) GetVMStatus(node string, vmid int) (string, error) {
	if strings.TrimSpace(node) == "" {
		return "", fmt.Errorf("node is required")
	}
	if vmid <= 0 {
		return "", fmt.Errorf("vmid must be positive")
	}
	return c.vmStatus(node, vmid)
}

func (c *Client) cloneVM(req CloneRequest) (string, error) {
	form := url.Values{}
	form.Set("newid", strconv.Itoa(req.NewVMID))
	form.Set("name", req.Name)
	if strings.TrimSpace(req.TargetNode) != "" {
		form.Set("target", strings.TrimSpace(req.TargetNode))
	}
	if strings.TrimSpace(req.Storage) != "" {
		form.Set("storage", strings.TrimSpace(req.Storage))
	}
	if strings.TrimSpace(req.Pool) != "" {
		form.Set("pool", strings.TrimSpace(req.Pool))
	}
	if req.Full {
		form.Set("full", "1")
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/clone", url.PathEscape(req.Node), req.TemplateVMID)
	return c.postFormForTaskUPID(path, form)
}

func (c *Client) applyPostCloneVMConfig(req CloneRequest) error {
	form := url.Values{}
	if req.MemoryMB > 0 {
		form.Set("memory", strconv.Itoa(req.MemoryMB))
	}
	if req.Cores > 0 {
		form.Set("cores", strconv.Itoa(req.Cores))
	}
	if req.Sockets > 0 {
		form.Set("sockets", strconv.Itoa(req.Sockets))
	}
	if s := strings.TrimSpace(req.IPConfig0); s != "" {
		form.Set("ipconfig0", s)
	}
	if s := strings.TrimSpace(req.CloudInitUserData); s != "" {
		snippetStorage := strings.TrimSpace(req.CloudInitSnippetStorage)
		if snippetStorage == "" {
			snippetStorage = "local"
		}
		filename := fmt.Sprintf("fluid-vm-%d-user-data.yml", req.NewVMID)
		if err := c.uploadSnippet(req.Node, snippetStorage, filename, s); err != nil {
			return err
		}
		form.Set("cicustom", fmt.Sprintf("user=%s:snippets/%s", snippetStorage, filename))
	}

	needsCloudInitDrive := strings.TrimSpace(req.IPConfig0) != "" || strings.TrimSpace(req.CloudInitUserData) != ""
	if needsCloudInitDrive {
		ciStore := strings.TrimSpace(req.CloudInitDiskStorage)
		if ciStore == "" {
			ciStore = strings.TrimSpace(req.Storage)
		}
		if ciStore == "" {
			return fmt.Errorf(
				"cloud_init_disk_storage or storage is required when ipconfig0 or cloud_init_user_data is set " +
					"(Proxmox needs a Cloud-Init CD drive, e.g. ide2=<storage>:cloudinit)",
			)
		}
		// Without this, the UI shows ipconfig0 but the guest never sees network/user-data ("No CloudInit Drive found").
		form.Set("ide2", ciStore+":cloudinit")
	}

	if len(form) == 0 {
		return nil
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/config", url.PathEscape(req.Node), req.NewVMID)
	return c.postFormNoTask(path, form)
}

func (c *Client) uploadSnippet(node, storage, filename, content string) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("content", "snippets"); err != nil {
		return err
	}

	part, err := writer.CreateFormFile("filename", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, strings.NewReader(content)); err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}

	u := c.baseURL + "/api2/json/nodes/" + url.PathEscape(node) + "/storage/" + url.PathEscape(storage) + "/upload"
	req, err := http.NewRequest(http.MethodPost, u, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *Client) startVM(node string, vmid int) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/status/start", url.PathEscape(node), vmid)
	return c.postFormForTaskUPID(path, url.Values{})
}

func (c *Client) stopVM(node string, vmid int) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/status/stop", url.PathEscape(node), vmid)
	return c.postFormForTaskUPID(path, url.Values{})
}

func (c *Client) postFormForTaskUPID(path string, form url.Values) (string, error) {
	u := c.baseURL + "/api2/json" + path
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("proxmox returned %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if strings.TrimSpace(payload.Data) == "" {
		return "", fmt.Errorf("missing task upid in response")
	}
	return payload.Data, nil
}

func (c *Client) postFormNoTask(path string, form url.Values) error {
	u := c.baseURL + "/api2/json" + path
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) deleteForTaskUPID(path string) (string, error) {
	u := c.baseURL + "/api2/json" + path
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("proxmox returned %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if strings.TrimSpace(payload.Data) == "" {
		return "", fmt.Errorf("missing task upid in response")
	}
	return payload.Data, nil
}

func (c *Client) waitForTask(node, upid string) error {
	deadline := time.Now().Add(c.httpClient.Timeout)
	for {
		status, exitStatus, err := c.taskStatus(node, upid)
		if err != nil {
			return err
		}
		if status == "stopped" {
			if strings.EqualFold(exitStatus, "OK") {
				return nil
			}
			return fmt.Errorf("exitstatus=%s", exitStatus)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for task completion")
		}
		time.Sleep(c.pollInterval)
	}
}

func (c *Client) taskStatus(node, upid string) (string, string, error) {
	u := c.baseURL + "/api2/json/nodes/" + url.PathEscape(node) + "/tasks/" + url.PathEscape(upid) + "/status"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("proxmox returned %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Data struct {
			Status     string `json:"status"`
			ExitStatus string `json:"exitstatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", fmt.Errorf("decode response: %w", err)
	}
	return payload.Data.Status, payload.Data.ExitStatus, nil
}

func (c *Client) vmStatus(node string, vmid int) (string, error) {
	u := c.baseURL + "/api2/json/nodes/" + url.PathEscape(node) + "/qemu/" + strconv.Itoa(vmid) + "/status/current"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("proxmox returned %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return payload.Data.Status, nil
}
