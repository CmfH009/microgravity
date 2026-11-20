package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

type dockerClient interface {
	ImagePull(ctx context.Context, refStr string, options types.ImagePullOptions) (io.ReadCloser, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *specs.Platform, containerName string) (container.ContainerCreateCreatedBody, error)
	ContainerStart(ctx context.Context, containerID string, options types.ContainerStartOptions) error
	ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.ContainerWaitOKBody, <-chan error)
	ContainerLogs(ctx context.Context, container string, options types.ContainerLogsOptions) (io.ReadCloser, error)
	ContainerRemove(ctx context.Context, containerID string, options types.ContainerRemoveOptions) error
	Close() error
}

var _ dockerClient = (*client.Client)(nil)

var newDockerClient = func() (dockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return cli, nil
}

func runInContainer(ctx context.Context, imageName string, cmd []string) (string, error) {
	cli, err := newDockerClient()
	if err != nil {
		return "", fmt.Errorf("failed to create docker client: %w", err)
	}
	defer cli.Close()

	reader, err := cli.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err == nil {
		if reader != nil {
			_, _ = io.Copy(io.Discard, reader)
			reader.Close()
		}
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "", fmt.Errorf("image pull cancelled/timed out: %w", err)
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:           imageName,
		Cmd:             cmd,
		NetworkDisabled: true, // Prevent external network access
	}, &container.HostConfig{
		Resources: container.Resources{
			Memory:   128 * 1024 * 1024, // 128 MB RAM
			NanoCPUs: 500000000,         // 0.5 CPU limit
		},
	}, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = cli.ContainerRemove(cleanupCtx, resp.ID, types.ContainerRemoveOptions{Force: true})
	}()

	if err := cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return "", fmt.Errorf("error waiting for container: %w", err)
		}
	case <-statusCh:
	case <-ctx.Done():
		return "", fmt.Errorf("context done while waiting for container: %w", ctx.Err())
	}

	out, err := cli.ContainerLogs(context.Background(), resp.ID, types.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %w", err)
	}
	defer out.Close()

	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, out)
	if err != nil {
		return "", fmt.Errorf("failed to copy logs: %w", err)
	}

	result := stdout.String()
	if stderr.Len() > 0 {
		if result != "" {
			result += "\n"
		}
		result += stderr.String()
	}
	return result, nil
}

func RunPython(ctx context.Context, code string) (string, error) {
	return runInContainer(ctx, "python:3.10-slim", []string{"python", "-c", code})
}

func RunNodeJS(ctx context.Context, code string) (string, error) {
	return runInContainer(ctx, "node:20-alpine", []string{"node", "-e", code})
}
