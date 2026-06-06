package vm

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/client"
)

func TestWorkerStartRequestValidate(t *testing.T) {
	valid := WorkerStartRequest{
		Version:           WorkerProtocolVersion,
		WorkerID:          "worker-1",
		VMID:              "vm-1",
		CacheRoot:         t.TempDir(),
		CoordinatorSocket: "/tmp/ccvmd.sock",
		AuthToken:         "token",
		Create:            &client.CreateInstanceRequest{Image: "alpine"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	both := valid
	both.Blank = &client.StartInstanceRequest{}
	if err := both.Validate(); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("Validate() with two start requests = %v, want exactly one error", err)
	}

	missingSocket := valid
	missingSocket.CoordinatorSocket = ""
	if err := missingSocket.Validate(); err == nil || !strings.Contains(err.Error(), "coordinator socket") {
		t.Fatalf("Validate() missing coordinator socket = %v", err)
	}
}

func TestWorkerCodecRoundTrip(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	clientCodec := NewWorkerCodec(left)
	workerCodec := NewWorkerCodec(right)
	wantHello := WorkerHello{
		Version:  WorkerProtocolVersion,
		WorkerID: "worker-1",
		Backend:  "hvf",
		Capabilities: VMHostCapabilities{
			Backend:       "hvf",
			MaxVMs:        1,
			Locality:      "sidecar",
			SupportsFSRPC: true,
			SupportsL2:    true,
		},
	}
	frame, err := NewWorkerFrame(7, WorkerServiceControl, WorkerFrameHello, wantHello)
	if err != nil {
		t.Fatalf("NewWorkerFrame() error = %v", err)
	}

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- clientCodec.Send(frame)
	}()

	gotFrame, err := workerCodec.Receive()
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if err := <-sendDone; err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotFrame.ID != 7 || gotFrame.Service != WorkerServiceControl || gotFrame.Type != WorkerFrameHello {
		t.Fatalf("frame metadata = %#v, want hello control frame", gotFrame)
	}
	var gotHello WorkerHello
	if err := gotFrame.DecodePayload(&gotHello); err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}
	if gotHello != wantHello {
		t.Fatalf("hello = %#v, want %#v", gotHello, wantHello)
	}
}

func TestSidecarLaunchArgs(t *testing.T) {
	t.Setenv(sidecarModeEnv, "")
	if got := sidecarLaunchArgs(); len(got) != 0 {
		t.Fatalf("sidecarLaunchArgs() = %v, want empty args", got)
	}

	t.Setenv(sidecarModeEnv, "vsh-internal")
	got := sidecarLaunchArgs()
	if len(got) != 1 || got[0] != "--vsh-internal-ccvm" {
		t.Fatalf("sidecarLaunchArgs() = %v, want vsh internal ccvm args", got)
	}
}

func TestSidecarWorkerClientWaitPollsStatus(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	workerClient := &sidecarWorkerClient{conn: left, codec: NewWorkerCodec(left)}
	workerCodec := NewWorkerCodec(right)

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- workerClient.Wait(context.Background(), "alpha")
	}()

	frame, err := workerCodec.Receive()
	if err != nil {
		t.Fatalf("worker Receive() error = %v", err)
	}
	if frame.Type != WorkerFrameStatus {
		t.Fatalf("frame.Type = %q, want status", frame.Type)
	}
	var req WorkerStatusRequest
	if err := frame.DecodePayload(&req); err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}
	if req.ID != "alpha" {
		t.Fatalf("status ID = %q, want alpha", req.ID)
	}
	done, err := NewWorkerFrame(frame.ID, WorkerServiceControl, WorkerFrameDone, WorkerStatusResponse{State: client.InstanceState{ID: "alpha", Status: "stopped"}})
	if err != nil {
		t.Fatalf("NewWorkerFrame(done) error = %v", err)
	}
	if err := workerCodec.Send(done); err != nil {
		t.Fatalf("worker Send(done) error = %v", err)
	}
	if err := <-waitDone; err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestSidecarWorkerClientConsoleHistoryRoundTrip(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	workerClient := &sidecarWorkerClient{conn: left, codec: NewWorkerCodec(left)}
	workerCodec := NewWorkerCodec(right)

	historyDone := make(chan struct {
		history string
		err     error
	}, 1)
	go func() {
		history, err := workerClient.ConsoleHistory(context.Background(), "alpha")
		historyDone <- struct {
			history string
			err     error
		}{history: history, err: err}
	}()

	frame, err := workerCodec.Receive()
	if err != nil {
		t.Fatalf("worker Receive() error = %v", err)
	}
	if frame.Type != WorkerFrameConsole {
		t.Fatalf("frame.Type = %q, want console", frame.Type)
	}
	var req WorkerConsoleRequest
	if err := frame.DecodePayload(&req); err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}
	if req.ID != "alpha" {
		t.Fatalf("console ID = %q, want alpha", req.ID)
	}
	done, err := NewWorkerFrame(frame.ID, WorkerServiceControl, WorkerFrameDone, WorkerConsoleResponse{History: "boot ok\n"})
	if err != nil {
		t.Fatalf("NewWorkerFrame(done) error = %v", err)
	}
	if err := workerCodec.Send(done); err != nil {
		t.Fatalf("worker Send(done) error = %v", err)
	}
	got := <-historyDone
	if got.err != nil {
		t.Fatalf("ConsoleHistory() error = %v", got.err)
	}
	if got.history != "boot ok\n" {
		t.Fatalf("ConsoleHistory() = %q, want boot ok", got.history)
	}
}

func TestSidecarWorkerClientExecStreamReturnsOnContextCancel(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	workerClient := &sidecarWorkerClient{conn: left, codec: NewWorkerCodec(left)}
	workerCodec := NewWorkerCodec(right)
	ctx, cancel := context.WithCancel(context.Background())
	execDone := make(chan error, 1)
	go func() {
		execDone <- workerClient.ExecStream(ctx, "alpha", client.ExecRequest{Command: []string{"sleep", "100"}}, nil, nil)
	}()

	frame, err := workerCodec.Receive()
	if err != nil {
		t.Fatalf("worker Receive() error = %v", err)
	}
	if frame.Type != WorkerFrameExec {
		t.Fatalf("frame.Type = %q, want exec", frame.Type)
	}
	cancel()
	select {
	case err := <-execDone:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("ExecStream() error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ExecStream() did not return after context cancel")
	}
}

func TestPlacementVMHostReleasesCapacityWhenInstanceExits(t *testing.T) {
	firstInst := &fakeInstance{waitCh: make(chan error, 1)}
	secondInst := &fakeInstance{waitCh: make(chan error, 1)}
	starts := 0
	host := &fakeVMHost{
		Backend: fakeBackend{startFn: func(req client.CreateInstanceRequest) (Instance, error) {
			starts++
			if starts == 1 {
				return firstInst, nil
			}
			return secondInst, nil
		}},
		caps: VMHostCapabilities{Backend: "single", MaxVMs: 1},
	}
	mgr := NewManagerWithHosts(host)
	mgr.supports = func() error { return nil }

	if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "one", Image: "alpine"}); err != nil {
		t.Fatalf("Start(one) error = %v", err)
	}
	firstInst.waitCh <- nil

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.StatusOf("one").Status == "stopped" {
			if _, err := mgr.Start(context.Background(), client.CreateInstanceRequest{ID: "two", Image: "busybox"}); err != nil {
				t.Fatalf("Start(two) after exit error = %v", err)
			}
			if starts != 2 {
				t.Fatalf("start count = %d, want 2", starts)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("manager never transitioned to stopped after instance exit")
}
