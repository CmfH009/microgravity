package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type dockerClient interface {
	ImagePull(ctx context.Context, refStr string, options client.ImagePullOptions) (client.ImagePullResponse, error)
	ContainerCreate(ctx context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerWait(ctx context.Context, containerID string, options client.ContainerWaitOptions) client.ContainerWaitResult
	ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	Close() error
}

var _ dockerClient = (*client.Client)(nil)

var newDockerClient = func() (dockerClient, error) {
	cli, err := client.New(client.FromEnv)
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

	reader, err := cli.ImagePull(ctx, imageName, client.ImagePullOptions{})
	if err == nil {
		if reader != nil {
			_, _ = io.Copy(io.Discard, reader)
			reader.Close()
		}
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "", fmt.Errorf("image pull cancelled/timed out: %w", err)
	}

	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:           imageName,
			Cmd:             cmd,
			NetworkDisabled: true, // Prevent external network access
		},
		HostConfig: &container.HostConfig{
			Resources: container.Resources{
				Memory:   128 * 1024 * 1024, // 128 MB RAM
				NanoCPUs: 500000000,         // 0.5 CPU limit
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = cli.ContainerRemove(cleanupCtx, resp.ID, client.ContainerRemoveOptions{Force: true})
	}()

	if _, err := cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	waitRes := cli.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case err := <-waitRes.Error:
		if err != nil {
			return "", fmt.Errorf("error waiting for container: %w", err)
		}
	case <-waitRes.Result:
	case <-ctx.Done():
		return "", fmt.Errorf("context done while waiting for container: %w", ctx.Err())
	}

	out, err := cli.ContainerLogs(context.Background(), resp.ID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
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
