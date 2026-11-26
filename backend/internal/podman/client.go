package podman

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	dockerClient "github.com/docker/docker/client"
)

type PodmanClient struct {
	cli *dockerClient.Client
}

func NewPodmanClient() (*PodmanClient, error) {
	// Try multiple socket paths for rootless Podman
	socketPaths := []string{
		"/run/user/" + os.Getenv("UID") + "/podman/podman.sock",
		"/run/podman/podman.sock",
		"/var/run/podman/podman.sock",
		"/run/docker.sock", // Fallback to docker socket
	}

	var lastErr error
	for _, path := range socketPaths {
		if _, err := os.Stat(path); err == nil {
			cli, err := dockerClient.NewClientWithOpts(
				dockerClient.WithHost("unix://"+path),
				dockerClient.WithAPIVersionNegotiation(),
			)
			if err != nil {
				lastErr = err
				continue
			}
			return &PodmanClient{cli: cli}, nil
		}
		lastErr = fmt.Errorf("socket %s not found", path)
	}

	return nil, fmt.Errorf("no accessible container runtime: %w", lastErr)
}

func (p *PodmanClient) ContainerList(ctx context.Context) ([]container.Summary, error) {
	return p.cli.ContainerList(ctx, container.ListOptions{All: true})
}

func (p *PodmanClient) ContainerInspect(ctx context.Context, id string) (container.InspectResponse, error) {
	return p.cli.ContainerInspect(ctx, id)
}

func (p *PodmanClient) ContainerLogs(ctx context.Context, id string, follow bool) (string, error) {
	reader, err := p.cli.ContainerLogs(ctx, id, dockerClient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       "100",
	})
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var buf strings.Builder
	_, _ = io.Copy(&buf, reader)
	return buf.String(), nil
}

func (p *PodmanClient) Close() error {
	return p.cli.Close()
}
