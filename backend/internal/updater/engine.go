package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/overyanosh/homelab-guard/backend/internal/labels"
)

type UpdateResult struct {
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	Updated       bool   `json:"updated"`
	Status        string `json:"status"`
	AIDiagnosis   string `json:"ai_diagnosis,omitempty"`
}

type Engine struct {
	docker      *client.Client
	aiWorkerURL string
	httpClient  *http.Client
}

func NewEngine(dockerCli *client.Client, aiWorkerURL string) *Engine {
	return &Engine{
		docker:      dockerCli,
		aiWorkerURL: strings.TrimRight(aiWorkerURL, "/"),
		httpClient:  &http.Client{Timeout: 45 * time.Second},
	}
}

func (e *Engine) EvaluateAndUpgrade(ctx context.Context, containerID string, force bool) (*UpdateResult, error) {
	inspect, err := e.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspect failed: %w", err)
	}

	cfg := labels.ParseDockerLabels(inspect.Config.Labels)
	name := strings.TrimPrefix(inspect.Name, "/")
	imgRef := inspect.Config.Image

	if !cfg.Enabled && !force {
		return &UpdateResult{ContainerName: name, Image: imgRef, Status: "ignored"}, nil
	}

	pullReader, err := e.docker.ImagePull(ctx, imgRef, image.PullOptions{})
	if err != nil {
		return nil, fmt.Errorf("image pull failed: %w", err)
	}
	_, _ = io.Copy(io.Discard, pullReader)
	_ = pullReader.Close()

	newImg, _, err := e.docker.ImageInspectWithRaw(ctx, imgRef)
	if err != nil {
		return nil, fmt.Errorf("new image inspect failed: %w", err)
	}

	if newImg.ID == inspect.Image && !force {
		return &UpdateResult{ContainerName: name, Image: imgRef, Updated: false, Status: "up-to-date"}, nil
	}

	if cfg.UpdateMode == "notify" && !force {
		return &UpdateResult{ContainerName: name, Image: imgRef, Updated: false, Status: "notify-only"}, nil
	}

	sandboxName := fmt.Sprintf("%s-guard-sandbox-%d", name, time.Now().Unix())
	sandboxID, err := e.startSandbox(ctx, inspect, sandboxName)
	if err != nil {
		return nil, fmt.Errorf("sandbox creation failed: %w", err)
	}
	defer func() {
		_ = e.docker.ContainerRemove(context.Background(), sandboxID, container.RemoveOptions{Force: true})
	}()

	testErr := e.runRegressionTest(ctx, sandboxID, sandboxName, cfg)
	if testErr != nil {
		logs := e.collectContainerLogs(ctx, sandboxID)
		diagnosis := e.requestAIDiagnosis(name, cfg.AIContext, fmt.Sprintf("Test error: %v\n%s", testErr, logs))
		return &UpdateResult{
			ContainerName: name,
			Image:         imgRef,
			Updated:       false,
			Status:        "rolled-back",
			AIDiagnosis:   diagnosis,
		}, nil
	}

	if err := e.promoteToProduction(ctx, inspect, name); err != nil {
		return nil, fmt.Errorf("promotion failed: %w", err)
	}

	return &UpdateResult{
		ContainerName: name,
		Image:         imgRef,
		Updated:       true,
		Status:        "promoted",
	}, nil
}

func (e *Engine) startSandbox(ctx context.Context, inspect dockerTypes.ContainerJSON, sandboxName string) (string, error) {
	if inspect.Config == nil {
		return "", fmt.Errorf("inspect config is nil")
	}
	sandboxCfg := *inspect.Config
	sandboxCfg.Hostname = sandboxName
	sandboxCfg.Labels = map[string]string{"guard.sandbox": "true"}

	roBinds := make([]string, 0, len(inspect.HostConfig.Binds))
	for _, b := range inspect.HostConfig.Binds {
		parts := strings.Split(b, ":")
		if len(parts) >= 2 {
			roBinds = append(roBinds, fmt.Sprintf("%s:%s:ro", parts[0], parts[1]))
		}
	}

	hostCfg := &container.HostConfig{
		Binds:       roBinds,
		NetworkMode: inspect.HostConfig.NetworkMode,
	}

	resp, err := e.docker.ContainerCreate(ctx, &sandboxCfg, hostCfg, &network.NetworkingConfig{}, nil, sandboxName)
	if err != nil {
		return "", err
	}
	if err := e.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return resp.ID, err
	}
	return resp.ID, nil
}

func (e *Engine) runRegressionTest(ctx context.Context, sandboxID, sandboxName string, cfg labels.GuardConfig) error {
	deadline := time.Now().Add(cfg.TestTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			insp, err := e.docker.ContainerInspect(ctx, sandboxID)
			if err != nil {
				return err
			}
			if !insp.State.Running {
				return fmt.Errorf("sandbox exited prematurely with code %d", insp.State.ExitCode)
			}

			switch cfg.TestStrategy {
			case "healthcheck":
				if insp.State.Health == nil {
					if time.Since(parseDockerTime(insp.State.StartedAt)) > 5*time.Second {
						return nil
					}
				} else if insp.State.Health.Status == "healthy" {
					return nil
				} else if insp.State.Health.Status == "unhealthy" {
					return fmt.Errorf("docker healthcheck reported unhealthy")
				}

			case "http":
				target := strings.Replace(cfg.TestEndpoint, "localhost", sandboxName, 1)
				target = strings.Replace(target, "127.0.0.1", sandboxName, 1)
				resp, err := e.httpClient.Get(target)
				if err == nil {
					_ = resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						return nil
					}
				}

			case "tcp":
				target := strings.Replace(cfg.TestEndpoint, "localhost", sandboxName, 1)
				conn, err := net.DialTimeout("tcp", target, 2*time.Second)
				if err == nil {
					_ = conn.Close()
					return nil
				}

			case "exec":
				execCfg := container.ExecOptions{Cmd: []string{"sh", "-c", cfg.TestCmd}}
				execID, err := e.docker.ContainerExecCreate(ctx, sandboxID, execCfg)
				if err == nil {
					if err := e.docker.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{}); err == nil {
						execInsp, err := e.docker.ContainerExecInspect(ctx, execID.ID)
						if err == nil && !execInsp.Running && execInsp.ExitCode == 0 {
							return nil
						}
					}
				}
			}
		}
	}
	return fmt.Errorf("regression test (%s) timed out after %s", cfg.TestStrategy, cfg.TestTimeout)
}

func (e *Engine) promoteToProduction(ctx context.Context, inspect dockerTypes.ContainerJSON, name string) error {
	timeout := 15
	_ = e.docker.ContainerStop(ctx, inspect.ID, container.StopOptions{Timeout: &timeout})
	if err := e.docker.ContainerRemove(ctx, inspect.ID, container.RemoveOptions{}); err != nil {
		return err
	}

	netCfg := &network.NetworkingConfig{EndpointsConfig: inspect.NetworkSettings.Networks}
	created, err := e.docker.ContainerCreate(ctx, inspect.Config, inspect.HostConfig, netCfg, nil, name)
	if err != nil {
		return err
	}
	return e.docker.ContainerStart(ctx, created.ID, container.StartOptions{})
}

func (e *Engine) collectContainerLogs(ctx context.Context, containerID string) string {
	reader, err := e.docker.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "80",
	})
	if err != nil {
		return ""
	}
	defer reader.Close()

	var buf bytes.Buffer
	_, _ = stdcopy.StdCopy(&buf, &buf, reader)
	return buf.String()
}

func (e *Engine) requestAIDiagnosis(serviceName, aiContext, logs string) string {
	payload, _ := json.Marshal(map[string]string{
		"service_name":   serviceName,
		"ai_context":     aiContext,
		"container_logs": logs,
	})
	resp, err := e.httpClient.Post(e.aiWorkerURL+"/ai/diagnose-regression", "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Sprintf("AI Worker unreachable: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 240 {
			snippet = snippet[:240] + "…"
		}
		return fmt.Sprintf("AI Worker returned HTTP %d: %s", resp.StatusCode, snippet)
	}

	var parsed struct {
		Analysis string `json:"analysis"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "AI Worker returned an unexpected response"
	}
	if strings.TrimSpace(parsed.Analysis) == "" {
		return "AI Worker returned an empty diagnosis"
	}
	return parsed.Analysis
}

func parseDockerTime(ts string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Now()
	}
	return t
}
