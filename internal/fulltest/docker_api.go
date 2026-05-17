package fulltest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"j5.nz/cc/client"
)

// DockerAPI implements the fulltest API with long-lived Docker containers.
// It is intentionally scoped to the fulltest execution model: images are
// prepared up front, each VM maps to one persistent container, and commands run
// with docker exec so filesystem and process state survives across steps.
type DockerAPI struct {
	ctx        context.Context
	binary     string
	network    string
	images     map[string]string
	containers map[string]struct{}
}

func NewDockerAPI(ctx context.Context, binary string) *DockerAPI {
	if strings.TrimSpace(binary) == "" {
		binary = "docker"
	}
	return &DockerAPI{
		ctx:        ctx,
		binary:     binary,
		network:    "cc-fulltest-" + imageCacheName(strconv.FormatInt(time.Now().UnixNano(), 10))[:20],
		images:     map[string]string{},
		containers: map[string]struct{}{},
	}
}

func (d *DockerAPI) PullImageStream(name string, req client.PullImageRequest, onEvent func(client.ProgressEvent) error) error {
	source, err := dockerSourceString(req)
	if err != nil {
		return err
	}
	if err := emitProgress(onEvent, name, "preparing", ""); err != nil {
		return err
	}
	image := strings.TrimPrefix(source, "docker-image:")
	if strings.HasPrefix(source, "docker-archive:") {
		archive, tag := splitDockerArchiveSource(source)
		if archive == "" {
			return fmt.Errorf("docker archive path is required")
		}
		if err := d.runProgress(onEvent, name, "docker load", "load", "-i", archive); err != nil {
			return err
		}
		image = tag
		if image == "" {
			return fmt.Errorf("docker archive source must include a tag with #tag for Docker backend")
		}
	} else if !strings.HasPrefix(source, "docker-image:") {
		image = source
		if image == "" {
			image = name
		}
		if err := d.ensureImage(image, onEvent, name); err != nil {
			return err
		}
	}
	if image == "" {
		return fmt.Errorf("docker image source is required")
	}
	if name != "" && name != image {
		if err := d.runProgress(onEvent, name, "docker tag", "tag", image, name); err != nil {
			return err
		}
		image = name
	}
	d.images[name] = image
	return emitProgress(onEvent, name, "ready", "")
}

func (d *DockerAPI) CreateInstanceStreamWithID(id string, req client.CreateInstanceRequest, onEvent func(client.BootEvent) error) (client.InstanceState, error) {
	image := d.images[req.Image]
	if image == "" {
		image = req.Image
	}
	if image == "" {
		return client.InstanceState{}, fmt.Errorf("image is required")
	}
	if err := emitBoot(onEvent, "status", "creating Docker network", ""); err != nil {
		return client.InstanceState{}, err
	}
	if err := d.ensureNetwork(req.Network); err != nil {
		return client.InstanceState{}, err
	}
	_ = d.command("rm", "-f", id).Run()
	args := []string{"run", "-d", "--name", id, "--hostname", id, "--network", d.network, "--network-alias", id}
	if alias := dockerAlias(id); alias != "" && alias != id {
		args = append(args, "--network-alias", alias)
	}
	if req.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", req.MemoryMB))
	}
	if req.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(req.CPUs))
	}
	if len(req.KernelModules) > 0 {
		if err := emitBoot(onEvent, "status", "kernel_modules ignored by Docker backend", ""); err != nil {
			return client.InstanceState{}, err
		}
	}
	if req.Network != nil {
		for _, forward := range req.Network.PortForwards {
			publish, err := dockerPublishArg(forward)
			if err != nil {
				return client.InstanceState{}, err
			}
			args = append(args, "-p", publish)
		}
	}
	for _, share := range req.Shares {
		mount, err := dockerMountArg(share)
		if err != nil {
			return client.InstanceState{}, err
		}
		args = append(args, "--mount", mount)
	}
	args = append(args, "--workdir", "/work", image, "sh", "-c", "trap 'exit 0' TERM INT; tail -f /dev/null & wait")
	if err := emitBoot(onEvent, "status", "starting container", ""); err != nil {
		return client.InstanceState{}, err
	}
	output, err := d.command(args...).CombinedOutput()
	if err != nil {
		return client.InstanceState{}, fmt.Errorf("docker run: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	d.containers[id] = struct{}{}
	ip, err := d.containerIP(id)
	if err != nil {
		return client.InstanceState{}, err
	}
	if err := emitBoot(onEvent, "ready", "", ""); err != nil {
		return client.InstanceState{}, err
	}
	return client.InstanceState{
		ID:          id,
		Status:      "running",
		Image:       image,
		MemoryMB:    req.MemoryMB,
		CPUs:        req.CPUs,
		StartedAt:   time.Now().Format(time.RFC3339),
		NetworkIPv4: ip,
	}, nil
}

func (d *DockerAPI) RunIn(id string, req client.RunRequest) (client.ExecResponse, error) {
	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = 120
	}
	ctx, cancel := context.WithTimeout(d.ctx, time.Duration(timeout*float64(time.Second))+5*time.Second)
	defer cancel()
	args := []string{"exec"}
	if req.WorkDir != "" {
		args = append(args, "-w", req.WorkDir)
	}
	if req.User != "" {
		args = append(args, "-u", req.User)
	}
	for _, env := range req.Env {
		args = append(args, "-e", env)
	}
	args = append(args, id)
	if len(req.Command) == 0 {
		args = append(args, "true")
	} else {
		args = append(args, req.Command...)
	}
	before := d.readContainerUsage(id)
	start := time.Now()
	cmd := exec.CommandContext(ctx, d.binary, args...)
	output, err := cmd.CombinedOutput()
	usage := dockerCommandUsage(cmd.ProcessState, time.Since(start), before, d.readContainerUsage(id))
	if ctx.Err() != nil {
		return client.ExecResponse{
			ExitCode: 124,
			Output:   string(output) + fmt.Sprintf("\n[fulltest] command timed out after %.1fs: %s\n", timeout, strings.Join(req.Command, " ")),
			Usage:    usage,
		}, nil
	}
	if err == nil {
		return client.ExecResponse{ExitCode: 0, Output: string(output), Usage: usage}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return client.ExecResponse{ExitCode: exitErr.ExitCode(), Output: string(output), Usage: usage}, nil
	}
	return client.ExecResponse{ExitCode: 1, Output: string(output) + err.Error(), Usage: usage}, err
}

func (d *DockerAPI) ShutdownInstanceWithID(id string) error {
	output, err := d.command("rm", "-f", id).CombinedOutput()
	delete(d.containers, id)
	if len(d.containers) == 0 && d.network != "" {
		_ = d.command("network", "rm", d.network).Run()
	}
	if err != nil {
		text := strings.TrimSpace(string(output))
		if strings.Contains(text, "No such container") {
			return nil
		}
		return fmt.Errorf("docker rm: %w\n%s", err, text)
	}
	return nil
}

func (d *DockerAPI) ensureImage(image string, onEvent func(client.ProgressEvent) error, artifact string) error {
	if err := d.command("image", "inspect", image).Run(); err == nil {
		return nil
	}
	return d.runProgress(onEvent, artifact, "docker pull", "pull", image)
}

func (d *DockerAPI) ensureNetwork(network *client.NetworkConfig) error {
	if err := d.command("network", "inspect", d.network).Run(); err == nil {
		return nil
	}
	args := []string{"network", "create"}
	if network != nil && network.Enabled && !network.AllowInternet {
		args = append(args, "--internal")
	}
	args = append(args, d.network)
	output, err := d.command(args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network create: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (d *DockerAPI) containerIP(id string) (string, error) {
	output, err := d.command("inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", id).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (d *DockerAPI) runProgress(onEvent func(client.ProgressEvent) error, artifact, status string, args ...string) error {
	if err := emitProgress(onEvent, artifact, status, ""); err != nil {
		return err
	}
	output, err := d.command(args...).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		_ = emitProgress(onEvent, artifact, status, text)
		return fmt.Errorf("docker %s: %w\n%s", args[0], err, text)
	}
	return nil
}

func (d *DockerAPI) command(args ...string) *exec.Cmd {
	return exec.CommandContext(d.ctx, d.binary, args...)
}

type dockerCgroupUsage struct {
	CPUUsec     uint64
	MemoryBytes uint64
	PeakBytes   uint64
}

func (d *DockerAPI) readContainerUsage(id string) dockerCgroupUsage {
	cmd := d.command("exec", id, "sh", "-c", "cat /sys/fs/cgroup/cpu.stat 2>/dev/null; printf 'memory.current '; cat /sys/fs/cgroup/memory.current 2>/dev/null || true; printf 'memory.peak '; cat /sys/fs/cgroup/memory.peak 2>/dev/null || true")
	output, err := cmd.Output()
	if err != nil {
		return dockerCgroupUsage{}
	}
	return parseDockerCgroupUsage(string(output))
}

func parseDockerCgroupUsage(text string) dockerCgroupUsage {
	var usage dockerCgroupUsage
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "usage_usec":
			usage.CPUUsec = value
		case "memory.current":
			usage.MemoryBytes = value
		case "memory.peak":
			usage.PeakBytes = value
		}
	}
	return usage
}

func dockerCommandUsage(state *os.ProcessState, wall time.Duration, before, after dockerCgroupUsage) *client.ResourceUsage {
	usage := &client.ResourceUsage{WallSeconds: wall.Seconds()}
	if after.CPUUsec >= before.CPUUsec && after.CPUUsec > 0 {
		usage.CPUSeconds = float64(after.CPUUsec-before.CPUUsec) / 1_000_000
	}
	if after.PeakBytes > 0 {
		usage.MaxRSSBytes = after.PeakBytes
	}
	if after.MemoryBytes > 0 {
		usage.MemoryBytes = after.MemoryBytes
	}
	if state != nil {
		if usage.CPUSeconds == 0 {
			usage.UserSeconds = state.UserTime().Seconds()
			usage.SystemSeconds = state.SystemTime().Seconds()
			usage.CPUSeconds = usage.UserSeconds + usage.SystemSeconds
		}
		if usage.MaxRSSBytes == 0 {
			if raw, ok := state.SysUsage().(*syscall.Rusage); ok && raw != nil && raw.Maxrss > 0 {
				usage.MaxRSSBytes = uint64(raw.Maxrss) * 1024
			}
		}
	}
	return usage
}

func dockerSourceString(req client.PullImageRequest) (string, error) {
	if strings.TrimSpace(req.Source) != "" {
		return strings.TrimSpace(req.Source), nil
	}
	if req.SourceRef != nil {
		source, err := req.SourceString()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(source), nil
	}
	return "", fmt.Errorf("docker image source is required")
}

func splitDockerArchiveSource(source string) (string, string) {
	value := strings.TrimPrefix(source, "docker-archive:")
	if index := strings.LastIndex(value, "#"); index >= 0 {
		return value[:index], value[index+1:]
	}
	return value, ""
}

func dockerMountArg(share client.ShareMount) (string, error) {
	if strings.TrimSpace(share.Source) == "" || strings.TrimSpace(share.Mount) == "" {
		return "", fmt.Errorf("share source and mount are required")
	}
	source, err := filepath.Abs(share.Source)
	if err != nil {
		return "", err
	}
	parts := []string{"type=bind", "src=" + source, "dst=" + share.Mount}
	if !share.Writable {
		parts = append(parts, "readonly")
	}
	return strings.Join(parts, ","), nil
}

func dockerPublishArg(forward client.PortForward) (string, error) {
	if forward.GuestPort <= 0 {
		return "", fmt.Errorf("port forward guest_port is required")
	}
	protocol := strings.ToLower(strings.TrimSpace(forward.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	guest := strconv.Itoa(forward.GuestPort)
	if forward.HostPort <= 0 {
		return guest + "/" + protocol, nil
	}
	host := strconv.Itoa(forward.HostPort)
	if strings.TrimSpace(forward.HostAddr) != "" {
		host = strings.TrimSpace(forward.HostAddr) + ":" + host
	}
	return host + ":" + guest + "/" + protocol, nil
}

func dockerAlias(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) == 0 {
		return id
	}
	return parts[len(parts)-1]
}

func emitProgress(onEvent func(client.ProgressEvent) error, artifact, status, errText string) error {
	if onEvent == nil {
		return nil
	}
	return onEvent(client.ProgressEvent{Artifact: artifact, Status: status, Error: errText})
}

func emitBoot(onEvent func(client.BootEvent) error, kind, message, errText string) error {
	if onEvent == nil {
		return nil
	}
	return onEvent(client.BootEvent{Kind: kind, Message: message, Error: errText})
}
