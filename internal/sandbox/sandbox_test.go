//go:build !docker

package sandbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"iter"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

type fakeImagePullResponse struct {
	io.ReadCloser
}

func (r fakeImagePullResponse) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}

func (r fakeImagePullResponse) Wait(context.Context) error {
	return nil
}

type fakeDockerClient struct {
	mu sync.Mutex

	containerID string
	stdout      string
	stderr      string

	pullFn    func(ctx context.Context, refStr string, options client.ImagePullOptions) (client.ImagePullResponse, error)
	createErr error
	startErr  error
	waitFn    func(ctx context.Context, containerID string, options client.ContainerWaitOptions) client.ContainerWaitResult
	logsFn    func(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	removeErr error
	closeErr  error

	pulledImage       string
	pullOpts          client.ImagePullOptions
	createdConfig     *container.Config
	createdHostConfig *container.HostConfig
	createdNetConfig  *network.NetworkingConfig
	createdPlatform   *specs.Platform
	createdName       string
	startedID         string
	waitedID          string
	waitedCond        container.WaitCondition
	logsID            string
	logsOpts          client.ContainerLogsOptions
	removedID         string
	removedOpts       client.ContainerRemoveOptions
	removeCalls       int
	closed            bool
}

func writeStdFrame(dst *bytes.Buffer, stream stdcopy.StdType, msg string) error {
	var hdr [8]byte
	hdr[0] = byte(stream)
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(msg)))
	if _, err := dst.Write(hdr[:]); err != nil {
		return err
	}
	_, err := dst.WriteString(msg)
	return err
}

func frameDockerStreams(t *testing.T, stdout, stderr string) io.ReadCloser {
	t.Helper()
	var buf bytes.Buffer
	if stdout != "" {
		if err := writeStdFrame(&buf, stdcopy.Stdout, stdout); err != nil {
			t.Fatalf("failed to frame stdout: %v", err)
		}
	}
	if stderr != "" {
		if err := writeStdFrame(&buf, stdcopy.Stderr, stderr); err != nil {
			t.Fatalf("failed to frame stderr: %v", err)
		}
	}
	return io.NopCloser(&buf)
}

func (f *fakeDockerClient) ImagePull(ctx context.Context, refStr string, options client.ImagePullOptions) (client.ImagePullResponse, error) {
	f.mu.Lock()
	f.pulledImage = refStr
	f.pullOpts = options
	fn := f.pullFn
	f.mu.Unlock()

	if fn != nil {
		return fn(ctx, refStr, options)
	}
	return fakeImagePullResponse{io.NopCloser(strings.NewReader(`{"status":"Pull complete"}`))}, nil
}

func (f *fakeDockerClient) ContainerCreate(_ context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.createdConfig = options.Config
	f.createdHostConfig = options.HostConfig
	f.createdNetConfig = options.NetworkingConfig
	f.createdPlatform = options.Platform
	f.createdName = options.Name

	if f.createErr != nil {
		return client.ContainerCreateResult{}, f.createErr
	}
	id := f.containerID
	if id == "" {
		id = "fake-container-id"
	}
	return client.ContainerCreateResult{ID: id}, nil
}

func (f *fakeDockerClient) ContainerStart(_ context.Context, containerID string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startedID = containerID
	return client.ContainerStartResult{}, f.startErr
}

func (f *fakeDockerClient) ContainerWait(ctx context.Context, containerID string, options client.ContainerWaitOptions) client.ContainerWaitResult {
	f.mu.Lock()
	f.waitedID = containerID
	f.waitedCond = options.Condition
	fn := f.waitFn
	f.mu.Unlock()

	if fn != nil {
		return fn(ctx, containerID, options)
	}

	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)
	statusCh <- container.WaitResponse{StatusCode: 0}
	return client.ContainerWaitResult{Result: statusCh, Error: errCh}
}

func (f *fakeDockerClient) ContainerLogs(_ context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	f.mu.Lock()
	f.logsID = containerID
	f.logsOpts = options
	fn := f.logsFn
	stdout := f.stdout
	stderr := f.stderr
	f.mu.Unlock()

	if fn != nil {
		return fn(context.Background(), containerID, options)
	}

	var buf bytes.Buffer
	if stdout != "" {
		_ = writeStdFrame(&buf, stdcopy.Stdout, stdout)
	}
	if stderr != "" {
		_ = writeStdFrame(&buf, stdcopy.Stderr, stderr)
	}
	return io.NopCloser(&buf), nil
}

func (f *fakeDockerClient) ContainerRemove(_ context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedID = containerID
	f.removedOpts = options
	f.removeCalls++
	return client.ContainerRemoveResult{}, f.removeErr
}

func (f *fakeDockerClient) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return f.closeErr
}

func withFakeDockerClient(t *testing.T, fake *fakeDockerClient) {
	t.Helper()
	withNewDockerClient(t, func() (dockerClient, error) {
		return fake, nil
	})
}

func withNewDockerClient(t *testing.T, fn func() (dockerClient, error)) {
	t.Helper()
	orig := newDockerClient
	newDockerClient = fn
	t.Cleanup(func() {
		newDockerClient = orig
	})
}

func TestRunPython_Success(t *testing.T) {
	fake := &fakeDockerClient{
		containerID: "py-ctr-1",
		logsFn: func(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
			return frameDockerStreams(t, "hello from python\n", ""), nil
		},
	}
	withFakeDockerClient(t, fake)

	code := `print("hello from python")`
	out, err := RunPython(context.Background(), code)
	if err != nil {
		t.Fatalf("RunPython failed: %v", err)
	}
	if !strings.Contains(out, "hello from python") {
		t.Errorf("Expected output to contain 'hello from python', got: %q", out)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.pulledImage != "python:3.10-slim" {
		t.Errorf("Expected pulled image 'python:3.10-slim', got %q", fake.pulledImage)
	}
	if fake.createdConfig == nil {
		t.Fatal("Expected ContainerCreate config to be recorded")
	}
	if fake.createdConfig.Image != "python:3.10-slim" {
		t.Errorf("Expected Image 'python:3.10-slim', got %q", fake.createdConfig.Image)
	}
	wantCmd := []string{"python", "-c", code}
	if !reflect.DeepEqual([]string(fake.createdConfig.Cmd), wantCmd) {
		t.Errorf("Expected Cmd %v, got %v", wantCmd, fake.createdConfig.Cmd)
	}
	if !fake.createdConfig.NetworkDisabled {
		t.Errorf("Expected NetworkDisabled=true")
	}
	if fake.createdHostConfig == nil {
		t.Fatal("Expected HostConfig to be recorded")
	}
	if fake.createdHostConfig.Resources.Memory != 128*1024*1024 {
		t.Errorf("Expected 128MB memory limit, got %d", fake.createdHostConfig.Resources.Memory)
	}
	if fake.createdHostConfig.Resources.NanoCPUs != 500000000 {
		t.Errorf("Expected 0.5 CPU limit (500000000), got %d", fake.createdHostConfig.Resources.NanoCPUs)
	}
	if fake.removeCalls != 1 || fake.removedID != "py-ctr-1" || !fake.removedOpts.Force {
		t.Errorf("Expected ContainerRemove called once with force=true for 'py-ctr-1', got calls=%d id=%q force=%v", fake.removeCalls, fake.removedID, fake.removedOpts.Force)
	}
	if !fake.closed {
		t.Errorf("Expected client Close() to be called")
	}
}

func TestRunNodeJS_Success(t *testing.T) {
	fake := &fakeDockerClient{
		containerID: "node-ctr-1",
		logsFn: func(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
			return frameDockerStreams(t, "hello from nodejs\n", ""), nil
		},
	}
	withFakeDockerClient(t, fake)

	code := `console.log("hello from nodejs")`
	out, err := RunNodeJS(context.Background(), code)
	if err != nil {
		t.Fatalf("RunNodeJS failed: %v", err)
	}
	if !strings.Contains(out, "hello from nodejs") {
		t.Errorf("Expected output to contain 'hello from nodejs', got: %q", out)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.createdConfig == nil {
		t.Fatal("Expected ContainerCreate config to be recorded")
	}
	if fake.createdConfig.Image != "node:20-alpine" {
		t.Errorf("Expected Image 'node:20-alpine', got %q", fake.createdConfig.Image)
	}
	wantCmd := []string{"node", "-e", code}
	if !reflect.DeepEqual([]string(fake.createdConfig.Cmd), wantCmd) {
		t.Errorf("Expected Cmd %v, got %v", wantCmd, fake.createdConfig.Cmd)
	}
	if !fake.createdConfig.NetworkDisabled {
		t.Errorf("Expected NetworkDisabled=true")
	}
	if fake.removeCalls != 1 || !fake.closed {
		t.Errorf("Expected cleanup and close, got removeCalls=%d closed=%v", fake.removeCalls, fake.closed)
	}
}

func TestRunPython_SyntaxError(t *testing.T) {
	fake := &fakeDockerClient{
		containerID: "py-syntax-1",
		logsFn: func(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
			return frameDockerStreams(t, "", "  File \"<string>\", line 1\nSyntaxError: '(' was never closed\n"), nil
		},
	}
	withFakeDockerClient(t, fake)

	out, err := RunPython(context.Background(), `print("hello"`)
	if err != nil {
		t.Fatalf("Expected nil error but got %v. Script failures should return logs as output.", err)
	}
	if !strings.Contains(out, "SyntaxError") {
		t.Errorf("Expected output to contain 'SyntaxError', got: %q", out)
	}
}

func TestRunPython_Timeout(t *testing.T) {
	fake := &fakeDockerClient{
		containerID: "py-timeout-1",
		waitFn: func(_ context.Context, _ string, _ client.ContainerWaitOptions) client.ContainerWaitResult {
			// Leave channels open without sending so runInContainer hits ctx.Done().
			return client.ContainerWaitResult{
				Result: make(chan container.WaitResponse),
				Error:  make(chan error),
			}
		},
	}
	withFakeDockerClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := RunPython(ctx, `import time; time.sleep(10); print("done")`)
	if err == nil {
		t.Fatalf("Expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context done") {
		t.Errorf("Expected error mentioning 'context done', got: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.removeCalls != 1 || fake.removedID != "py-timeout-1" || !fake.removedOpts.Force {
		t.Errorf("Expected ContainerRemove cleanup after timeout, got calls=%d id=%q force=%v", fake.removeCalls, fake.removedID, fake.removedOpts.Force)
	}
	if !fake.closed {
		t.Errorf("Expected client Close() after timeout")
	}
}

func TestRunNodeJS_NoNetwork(t *testing.T) {
	fake := &fakeDockerClient{
		containerID: "node-nonet-1",
		logsFn: func(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
			return frameDockerStreams(t, "", "connect ENETUNREACH 93.184.216.34:80\n"), nil
		},
	}
	withFakeDockerClient(t, fake)

	out, err := RunNodeJS(context.Background(), `require('http').get('http://93.184.216.34')`)
	if err != nil {
		t.Fatalf("Expected err to be nil, got: %v", err)
	}
	if !strings.Contains(out, "ENETUNREACH") {
		t.Errorf("Expected network error in output, got: %q", out)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.createdConfig == nil || !fake.createdConfig.NetworkDisabled {
		t.Errorf("Expected NetworkDisabled=true on ContainerCreate")
	}
}

func TestRunInContainer_CombinedStdoutStderr(t *testing.T) {
	fake := &fakeDockerClient{
		containerID: "both-streams-1",
		logsFn: func(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
			return frameDockerStreams(t, "stdout line", "stderr line"), nil
		},
	}
	withFakeDockerClient(t, fake)

	out, err := RunPython(context.Background(), `print("stdout line")`)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	want := "stdout line\nstderr line"
	if out != want {
		t.Errorf("Expected %q, got %q", want, out)
	}
}

func TestRunInContainer_ErrorPaths(t *testing.T) {
	t.Run("client creation failure", func(t *testing.T) {
		withNewDockerClient(t, func() (dockerClient, error) {
			return nil, errors.New("daemon socket missing")
		})
		_, err := RunPython(context.Background(), `print(1)`)
		if err == nil || !strings.Contains(err.Error(), "failed to create docker client") {
			t.Fatalf("Expected client creation error, got: %v", err)
		}
	})

	t.Run("image pull cancelled", func(t *testing.T) {
		fake := &fakeDockerClient{
			pullFn: func(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
				return nil, context.Canceled
			},
		}
		withFakeDockerClient(t, fake)
		_, err := RunPython(context.Background(), `print(1)`)
		if err == nil || !strings.Contains(err.Error(), "image pull cancelled/timed out") {
			t.Fatalf("Expected image pull cancellation error, got: %v", err)
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if !fake.closed {
			t.Errorf("Expected Close() to be called")
		}
	})

	t.Run("image pull deadline exceeded", func(t *testing.T) {
		fake := &fakeDockerClient{
			pullFn: func(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
				return nil, context.DeadlineExceeded
			},
		}
		withFakeDockerClient(t, fake)
		_, err := RunNodeJS(context.Background(), `console.log(1)`)
		if err == nil || !strings.Contains(err.Error(), "image pull cancelled/timed out") {
			t.Fatalf("Expected image pull timeout error, got: %v", err)
		}
	})

	t.Run("image pull offline falls back to cached image", func(t *testing.T) {
		fake := &fakeDockerClient{
			pullFn: func(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
				return nil, errors.New("registry offline")
			},
			logsFn: func(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
				return frameDockerStreams(t, "cached-ok\n", ""), nil
			},
		}
		withFakeDockerClient(t, fake)
		out, err := RunPython(context.Background(), `print("cached-ok")`)
		if err != nil {
			t.Fatalf("Expected fallback to cached image to succeed, got: %v", err)
		}
		if !strings.Contains(out, "cached-ok") {
			t.Errorf("Expected 'cached-ok', got %q", out)
		}
	})

	t.Run("container create failure", func(t *testing.T) {
		fake := &fakeDockerClient{
			createErr: errors.New("invalid config"),
		}
		withFakeDockerClient(t, fake)
		_, err := RunPython(context.Background(), `print(1)`)
		if err == nil || !strings.Contains(err.Error(), "failed to create container") {
			t.Fatalf("Expected container create error, got: %v", err)
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.removeCalls != 0 {
			t.Errorf("Expected no ContainerRemove when create failed, got %d", fake.removeCalls)
		}
		if !fake.closed {
			t.Errorf("Expected Close() to be called")
		}
	})

	t.Run("container start failure", func(t *testing.T) {
		fake := &fakeDockerClient{
			containerID: "start-fail-ctr",
			startErr:    errors.New("oci runtime error"),
		}
		withFakeDockerClient(t, fake)
		_, err := RunPython(context.Background(), `print(1)`)
		if err == nil || !strings.Contains(err.Error(), "failed to start container") {
			t.Fatalf("Expected container start error, got: %v", err)
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.removeCalls != 1 || fake.removedID != "start-fail-ctr" {
			t.Errorf("Expected ContainerRemove after start failure, got calls=%d id=%q", fake.removeCalls, fake.removedID)
		}
	})

	t.Run("container wait error", func(t *testing.T) {
		fake := &fakeDockerClient{
			containerID: "wait-err-ctr",
			waitFn: func(_ context.Context, _ string, _ client.ContainerWaitOptions) client.ContainerWaitResult {
				statusCh := make(chan container.WaitResponse)
				errCh := make(chan error, 1)
				errCh <- errors.New("wait stream broken")
				return client.ContainerWaitResult{Result: statusCh, Error: errCh}
			},
		}
		withFakeDockerClient(t, fake)
		_, err := RunPython(context.Background(), `print(1)`)
		if err == nil || !strings.Contains(err.Error(), "error waiting for container") {
			t.Fatalf("Expected wait error, got: %v", err)
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.removeCalls != 1 || fake.removedID != "wait-err-ctr" {
			t.Errorf("Expected ContainerRemove after wait error, got calls=%d id=%q", fake.removeCalls, fake.removedID)
		}
	})

	t.Run("container logs error", func(t *testing.T) {
		fake := &fakeDockerClient{
			containerID: "logs-err-ctr",
			logsFn: func(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
				return nil, errors.New("logs unavailable")
			},
		}
		withFakeDockerClient(t, fake)
		_, err := RunPython(context.Background(), `print(1)`)
		if err == nil || !strings.Contains(err.Error(), "failed to get container logs") {
			t.Fatalf("Expected logs error, got: %v", err)
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.removeCalls != 1 || fake.removedID != "logs-err-ctr" {
			t.Errorf("Expected ContainerRemove after logs error, got calls=%d id=%q", fake.removeCalls, fake.removedID)
		}
	})

	t.Run("stdcopy demux error on invalid stream header", func(t *testing.T) {
		fake := &fakeDockerClient{
			containerID: "bad-frame-ctr",
			logsFn: func(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
				// Invalid stream type byte (99) triggers stdcopy.StdCopy unrecognized header error.
				invalid := []byte{99, 0, 0, 0, 0, 0, 0, 1, 'x'}
				return io.NopCloser(bytes.NewReader(invalid)), nil
			},
		}
		withFakeDockerClient(t, fake)
		_, err := RunPython(context.Background(), `print(1)`)
		if err == nil || !strings.Contains(err.Error(), "failed to copy logs") {
			t.Fatalf("Expected failed to copy logs error, got: %v", err)
		}
	})
}
