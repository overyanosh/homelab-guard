package proxmox

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	authHeader string
	httpClient *http.Client
}

type ClusterResource struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Node     string  `json:"node"`
	VMID     int     `json:"vmid,omitempty"`
	Name     string  `json:"name,omitempty"`
	Status   string  `json:"status,omitempty"`
	CPU      float64 `json:"cpu,omitempty"`
	MaxCPU   int     `json:"maxcpu,omitempty"`
	Mem      int64   `json:"mem,omitempty"`
	MaxMem   int64   `json:"maxmem,omitempty"`
	Disk     int64   `json:"disk,omitempty"`
	MaxDisk  int64   `json:"maxdisk,omitempty"`
	Uptime   int64   `json:"uptime,omitempty"`
	Template int     `json:"template,omitempty"`
}

type CloneRequest struct {
	NewID       int    `json:"newid"`
	Name        string `json:"name"`
	TargetNode  string `json:"target,omitempty"`
	FullClone   bool   `json:"full"`
	Storage     string `json:"storage,omitempty"`
	Description string `json:"description,omitempty"`
}

type pveEnvelope[T any] struct {
	Data T `json:"data"`
}

func NewClient(endpoint, tokenID, tokenSecret string, insecureTLS bool) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureTLS},
	}
	return &Client{
		baseURL:    strings.TrimRight(endpoint, "/") + "/api2/json",
		authHeader: fmt.Sprintf("PVEAPIToken=%s=%s", tokenID, tokenSecret),
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
	}
}

func (c *Client) doRequest(method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxmox request failed: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("proxmox API error (%d): %s", resp.StatusCode, bytes.TrimSpace(rawBody))
	}

	if out != nil {
		return json.Unmarshal(rawBody, out)
	}
	return nil
}

func (c *Client) GetClusterResources(resourceType string) ([]ClusterResource, error) {
	path := "/cluster/resources"
	if resourceType != "" {
		path += "?type=" + url.QueryEscape(resourceType)
	}
	var env pveEnvelope[[]ClusterResource]
	if err := c.doRequest(http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

func (c *Client) GuestPowerAction(node, guestType string, vmid int, action string) (string, error) {
	switch action {
	case "start", "stop", "shutdown", "reboot", "reset":
	default:
		return "", fmt.Errorf("unsupported power action: %s", action)
	}
	if guestType != "qemu" && guestType != "lxc" {
		return "", fmt.Errorf("invalid guest type: %s", guestType)
	}

	path := fmt.Sprintf("/nodes/%s/%s/%d/status/%s", node, guestType, vmid, action)
	var env pveEnvelope[string]
	if err := c.doRequest(http.MethodPost, path, nil, &env); err != nil {
		return "", err
	}
	return env.Data, nil
}

func (c *Client) CreateSnapshot(node, guestType string, vmid int, snapName, description string, includeRAM bool) (string, error) {
	form := url.Values{}
	form.Set("snapname", snapName)
	form.Set("description", description)
	if guestType == "qemu" && includeRAM {
		form.Set("vmstate", "1")
	}

	path := fmt.Sprintf("/nodes/%s/%s/%d/snapshot", node, guestType, vmid)
	var env pveEnvelope[string]
	if err := c.doRequest(http.MethodPost, path, form, &env); err != nil {
		return "", err
	}
	return env.Data, nil
}

func (c *Client) CloneVM(node string, templateID int, req CloneRequest) (string, error) {
	form := url.Values{}
	form.Set("newid", fmt.Sprintf("%d", req.NewID))
	if req.Name != "" {
		form.Set("name", req.Name)
	}
	if req.TargetNode != "" {
		form.Set("target", req.TargetNode)
	}
	if req.FullClone {
		form.Set("full", "1")
	}
	if req.Storage != "" {
		form.Set("storage", req.Storage)
	}
	if req.Description != "" {
		form.Set("description", req.Description)
	}

	path := fmt.Sprintf("/nodes/%s/qemu/%d/clone", node, templateID)
	var env pveEnvelope[string]
	if err := c.doRequest(http.MethodPost, path, form, &env); err != nil {
		return "", err
	}
	return env.Data, nil
}

func (c *Client) DeleteGuest(node, guestType string, vmid int, purge bool) (string, error) {
	path := fmt.Sprintf("/nodes/%s/%s/%d?destroy-unreferenced-disks=1", node, guestType, vmid)
	if purge {
		path += "&purge=1"
	}
	var env pveEnvelope[string]
	if err := c.doRequest(http.MethodDelete, path, nil, &env); err != nil {
		return "", err
	}
	return env.Data, nil
}
