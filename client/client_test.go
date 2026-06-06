package client

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestClientRunEvents(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req ExecRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				t.Fatalf("JSON.Receive(req) error = %v", err)
			}
			if len(req.Command) != 2 || req.Command[0] != "echo" || req.Command[1] != "hello" {
				t.Fatalf("req = %#v", req)
			}
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "stdout", Stream: "stdout", Output: "hello", Data: []byte("hello")}); err != nil {
				t.Fatalf("JSON.Send(output) error = %v", err)
			}
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "exit", ExitCode: 0}); err != nil {
				t.Fatalf("JSON.Send(exit) error = %v", err)
			}
		},
	})
	defer ts.Close()

	c := NewClient(ts.URL, func() (net.Conn, error) {
		return nil, nil
	})
	c.client = *ts.Client()

	events, err := c.ExecEvents(ExecRequest{
		Command: []string{"echo", "hello"},
	})
	if err != nil {
		t.Fatalf("ExecEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].Kind != "stdout" || events[0].Output != "hello" {
		t.Fatalf("events[0] = %#v", events[0])
	}
	if events[1].Kind != "exit" || events[1].ExitCode != 0 {
		t.Fatalf("events[1] = %#v", events[1])
	}
}

func TestPullImageRequestSourceStringDockerArchive(t *testing.T) {
	req := PullImageRequest{
		SourceRef: &ImageSource{
			Type: "docker-archive",
			Path: "C:/tmp/tool.tar#example/tool:latest",
		},
	}
	source, err := req.SourceString()
	if err != nil {
		t.Fatalf("SourceString() error = %v", err)
	}
	if source != "docker-archive:C:/tmp/tool.tar#example/tool:latest" {
		t.Fatalf("SourceString() = %q", source)
	}
}

func TestPullImageRequestJSONIncludesArchitecture(t *testing.T) {
	req := PullImageRequest{Source: "ubuntu", Architecture: "amd64"}
	buf, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal(PullImageRequest) error = %v", err)
	}
	if !strings.Contains(string(buf), `"architecture":"amd64"`) {
		t.Fatalf("PullImageRequest JSON = %s, want architecture", string(buf))
	}
	var got PullImageRequest
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("Unmarshal(PullImageRequest) error = %v", err)
	}
	if got.Source != "ubuntu" || got.Architecture != "amd64" {
		t.Fatalf("PullImageRequest = %#v, want source ubuntu arch amd64", got)
	}
}

func TestClientEscapesImageNamesInPath(t *testing.T) {
	mux := http.NewServeMux()
	var got []string
	mux.HandleFunc("GET /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		got = append(got, "get:"+r.PathValue("image"))
		_ = json.NewEncoder(w).Encode(ImageState{Name: r.PathValue("image"), Status: "available"})
	})
	mux.HandleFunc("POST /image/{image}", func(w http.ResponseWriter, r *http.Request) {
		got = append(got, "post:"+r.PathValue("image"))
		_ = json.NewEncoder(w).Encode(ProgressEvent{Status: "downloaded", Artifact: r.PathValue("image")})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := NewClient(ts.URL, func() (net.Conn, error) {
		return nil, nil
	})
	c.client = *ts.Client()

	image := "kalilinux/kali-rolling"
	if _, err := c.GetImage(image); err != nil {
		t.Fatalf("GetImage(%q) error = %v", image, err)
	}
	if err := c.PullImageStream(image, PullImageRequest{Source: image}, func(ProgressEvent) error { return nil }); err != nil {
		t.Fatalf("PullImageStream(%q) error = %v", image, err)
	}
	want := strings.Join([]string{"get:" + image, "post:" + image}, "\n")
	if strings.Join(got, "\n") != want {
		t.Fatalf("requests = %#v, want %s", got, want)
	}
}

func TestClientExecStream(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req ExecRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				t.Fatalf("JSON.Receive(req) error = %v", err)
			}
			for _, event := range []ExecEvent{
				{Kind: "stdout", Stream: "stdout", Output: "he", Data: []byte("he")},
				{Kind: "stderr", Stream: "stderr", Output: "llo", Data: []byte("llo")},
				{Kind: "exit", ExitCode: 0},
			} {
				if err := websocket.JSON.Send(ws, event); err != nil {
					t.Fatalf("JSON.Send(event) error = %v", err)
				}
			}
		},
	})
	defer ts.Close()

	c := NewClient(ts.URL, func() (net.Conn, error) {
		return nil, nil
	})
	c.client = *ts.Client()

	var got []ExecEvent
	if err := c.ExecStream(ExecRequest{Command: []string{"echo", "hello"}}, nil, func(event ExecEvent) error {
		got = append(got, event)
		return nil
	}); err != nil {
		t.Fatalf("ExecStream() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("event count = %d, want 3", len(got))
	}
	if got[0].Kind != "stdout" || string(got[0].Data) != "he" || got[1].Kind != "stderr" || string(got[1].Data) != "llo" || got[2].Kind != "exit" {
		t.Fatalf("events = %#v", got)
	}
}

func TestClientRunStream(t *testing.T) {
	var gotRequest bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = true
		if r.URL.Path != "/vm/run" || r.URL.Query().Get("stream") != "1" {
			t.Fatalf("request URL = %s, want /vm/run?stream=1", r.URL.String())
		}
		if r.Header.Get("Accept") != "application/x-ndjson" {
			t.Fatalf("Accept = %q, want application/x-ndjson", r.Header.Get("Accept"))
		}
		var req RunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(req) error = %v", err)
		}
		if req.ID != "work" || req.Image != "ubuntu" || !req.TTY || req.Cols != 120 || req.Rows != 40 {
			t.Fatalf("req = %#v", req)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := json.NewEncoder(w)
		_ = enc.Encode(ExecEvent{Kind: "stdout", Output: "Linux\n", Data: []byte("Linux\n")})
		_ = enc.Encode(ExecEvent{Kind: "exit", ExitCode: 0})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, func() (net.Conn, error) { return nil, nil })
	c.client = *ts.Client()

	var events []ExecEvent
	err := c.RunStreamIn("work", RunRequest{
		Image:   "ubuntu",
		Command: []string{"uname", "-a"},
		TTY:     true,
		Cols:    120,
		Rows:    40,
	}, func(event ExecEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("RunStreamIn() error = %v", err)
	}
	if !gotRequest {
		t.Fatal("server did not receive request")
	}
	if len(events) != 2 || events[0].Output != "Linux\n" || events[1].Kind != "exit" {
		t.Fatalf("events = %#v", events)
	}
}

func TestClientDownloadKernelStream(t *testing.T) {
	var gotRequest bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = true
		if r.URL.Path != "/kernel/download" || r.URL.Query().Get("stream") != "1" {
			t.Fatalf("request URL = %s, want /kernel/download?stream=1", r.URL.String())
		}
		if got := r.Header.Get("Accept"); got != "application/x-ndjson" {
			t.Fatalf("Accept = %q, want application/x-ndjson", got)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(ProgressEvent{Status: "downloading", Artifact: "kernel", BytesDownloaded: 1, BytesTotal: 2})
		_ = json.NewEncoder(w).Encode(ProgressEvent{Status: "downloaded", Artifact: "kernel", BytesDownloaded: 2, BytesTotal: 2})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, nil)
	c.client = *ts.Client()

	var events []ProgressEvent
	if err := c.DownloadKernelStream(DownloadRequest{}, func(event ProgressEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("DownloadKernelStream() error = %v", err)
	}
	if !gotRequest || len(events) != 2 || events[1].Status != "downloaded" {
		t.Fatalf("events = %#v, gotRequest=%v", events, gotRequest)
	}
}

func TestClientPullImageStreamErrorEvent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(ProgressEvent{Status: "error", Artifact: "alpine", Error: "boom"})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, nil)
	c.client = *ts.Client()

	err := c.PullImageStream("alpine", PullImageRequest{Source: "alpine.simg"}, nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("PullImageStream() error = %v, want boom", err)
	}
}

func TestClientExecStreamSendsStdinCloseWhenInputsClose(t *testing.T) {
	stdinClosed := make(chan struct{}, 1)
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req ExecRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				t.Fatalf("JSON.Receive(req) error = %v", err)
			}
			var input ExecInput
			if err := websocket.JSON.Receive(ws, &input); err != nil {
				t.Fatalf("JSON.Receive(input) error = %v", err)
			}
			if input.Kind != "stdin_close" {
				t.Fatalf("input = %#v, want stdin_close", input)
			}
			stdinClosed <- struct{}{}
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "exit", ExitCode: 0}); err != nil {
				t.Fatalf("JSON.Send(exit) error = %v", err)
			}
		},
	})
	defer ts.Close()

	c := NewClient(ts.URL, func() (net.Conn, error) {
		return nil, nil
	})
	c.client = *ts.Client()

	inputs := make(chan ExecInput)
	close(inputs)
	if err := c.ExecStream(ExecRequest{Command: []string{"true"}}, inputs, nil); err != nil {
		t.Fatalf("ExecStream() error = %v", err)
	}
	select {
	case <-stdinClosed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stdin_close")
	}
}

func TestClientCreateInstanceStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vm" || r.URL.Query().Get("stream") != "1" {
			t.Fatalf("request URL = %s, want /vm?stream=1", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(BootEvent{Kind: "status", Message: "starting VM"})
		_ = json.NewEncoder(w).Encode(BootEvent{Kind: "ready", State: InstanceState{Status: "running", Image: "alpine"}})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, nil)
	c.client = *ts.Client()

	var events []BootEvent
	state, err := c.CreateInstanceStream(CreateInstanceRequest{Image: "alpine"}, func(event BootEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("CreateInstanceStream() error = %v", err)
	}
	if state.Status != "running" || state.Image != "alpine" || len(events) != 2 {
		t.Fatalf("state = %#v events = %#v", state, events)
	}
}

func TestClientCreateInstanceStreamRequiresReadyEvent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(BootEvent{Kind: "status", Message: "starting VM"})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, nil)
	c.client = *ts.Client()

	_, err := c.CreateInstanceStream(CreateInstanceRequest{Image: "alpine"}, nil)
	if err == nil || err.Error() != "boot stream ended before ready" {
		t.Fatalf("CreateInstanceStream() error = %v, want ready error", err)
	}
}

func TestClientNamedInstanceHTTPHelpers(t *testing.T) {
	var sawStatus, sawList, sawShutdown, sawForward, sawCreate, sawFlush, sawConsole, sawSave, sawDelete bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vm/status":
			sawStatus = true
			if got := r.URL.Query().Get("id"); got != "alpha beta" {
				t.Fatalf("status id = %q, want alpha beta", got)
			}
			_ = json.NewEncoder(w).Encode(InstanceState{ID: "alpha beta", Status: "running"})
		case "/vm":
			if r.Method == http.MethodGet {
				sawList = true
				_ = json.NewEncoder(w).Encode([]InstanceState{{ID: "alpha", Status: "running"}})
				return
			}
			sawCreate = true
			var req CreateInstanceRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			if req.ID != "alpha" || req.Image != "alpine" {
				t.Fatalf("create request = %#v, want id alpha image alpine", req)
			}
			_ = json.NewEncoder(w).Encode(InstanceState{ID: "alpha", Status: "running", Image: "alpine"})
		case "/vm/shutdown":
			sawShutdown = true
			if got := r.URL.Query().Get("id"); got != "alpha" {
				t.Fatalf("shutdown id = %q, want alpha", got)
			}
			_ = json.NewEncoder(w).Encode(InstanceState{ID: "alpha", Status: "stopped"})
		case "/vm/forward":
			sawForward = true
			if got := r.URL.Query().Get("id"); got != "alpha" {
				t.Fatalf("forward id = %q, want alpha", got)
			}
			var forward PortForward
			if err := json.NewDecoder(r.Body).Decode(&forward); err != nil {
				t.Fatalf("decode forward request: %v", err)
			}
			if forward.HostPort != 8080 || forward.GuestPort != 80 {
				t.Fatalf("forward = %#v, want 8080:80", forward)
			}
			_ = json.NewEncoder(w).Encode(forward)
		case "/vm/alpha beta/save":
			sawSave = true
			var req SaveImageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode save request: %v", err)
			}
			if req.Name != "saved tag" || req.Image != "alpine" {
				t.Fatalf("save request = %#v, want saved tag from alpine", req)
			}
			_ = json.NewEncoder(w).Encode(ImageState{Name: req.Name, Status: "downloaded"})
		case "/vm/alpha beta/flush":
			sawFlush = true
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "flushed"})
		case "/vm/console":
			if r.URL.Query().Get("id") != "alpha beta" {
				t.Fatalf("console id = %q", r.URL.Query().Get("id"))
			}
			sawConsole = true
			_ = json.NewEncoder(w).Encode(ConsoleHistoryResponse{History: "serial ok\n"})
		case "/image/saved tag":
			if r.Method != http.MethodDelete {
				t.Fatalf("delete method = %s, want DELETE", r.Method)
			}
			sawDelete = true
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	c := NewClient(ts.URL, nil)
	c.client = *ts.Client()

	if state, err := c.InstanceStatusOf("alpha beta"); err != nil || state.ID != "alpha beta" {
		t.Fatalf("InstanceStatusOf() = %#v, %v", state, err)
	}
	if statuses, err := c.InstanceStatuses(); err != nil || len(statuses) != 1 || statuses[0].ID != "alpha" {
		t.Fatalf("InstanceStatuses() = %#v, %v", statuses, err)
	}
	if state, err := c.CreateInstanceWithID("alpha", CreateInstanceRequest{Image: "alpine"}); err != nil || state.ID != "alpha" {
		t.Fatalf("CreateInstanceWithID() = %#v, %v", state, err)
	}
	if err := c.ShutdownInstanceWithID("alpha"); err != nil {
		t.Fatalf("ShutdownInstanceWithID() error = %v", err)
	}
	if err := c.AddPortForwardTo("alpha", PortForward{HostPort: 8080, GuestPort: 80}); err != nil {
		t.Fatalf("AddPortForwardTo() error = %v", err)
	}
	if state, err := c.SaveInstanceImage("alpha beta", SaveImageRequest{Name: "saved tag", Image: "alpine"}); err != nil || state.Name != "saved tag" {
		t.Fatalf("SaveInstanceImage() = %#v, %v", state, err)
	}
	if err := c.FlushInstance("alpha beta"); err != nil {
		t.Fatalf("FlushInstance() error = %v", err)
	}
	if history, err := c.ConsoleHistory("alpha beta"); err != nil || history != "serial ok\n" {
		t.Fatalf("ConsoleHistory() = %q, %v", history, err)
	}
	if err := c.DeleteImage("saved tag"); err != nil {
		t.Fatalf("DeleteImage() error = %v", err)
	}
	if !sawStatus || !sawList || !sawCreate || !sawShutdown || !sawForward || !sawFlush || !sawConsole || !sawSave || !sawDelete {
		t.Fatalf("missing requests: status=%v list=%v create=%v shutdown=%v forward=%v flush=%v console=%v save=%v delete=%v", sawStatus, sawList, sawCreate, sawShutdown, sawForward, sawFlush, sawConsole, sawSave, sawDelete)
	}
}

func TestClientExecStreamInSendsID(t *testing.T) {
	ts := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(ws *websocket.Conn) {
			var req ExecRequest
			if err := websocket.JSON.Receive(ws, &req); err != nil {
				t.Fatalf("JSON.Receive(req) error = %v", err)
			}
			if req.ID != "alpha" || len(req.Command) != 1 || req.Command[0] != "true" {
				t.Fatalf("req = %#v, want id alpha command true", req)
			}
			if err := websocket.JSON.Send(ws, ExecEvent{Kind: "exit", ExitCode: 0}); err != nil {
				t.Fatalf("JSON.Send(exit) error = %v", err)
			}
		},
	})
	defer ts.Close()

	c := NewClient(ts.URL, func() (net.Conn, error) {
		return nil, nil
	})
	c.client = *ts.Client()

	if err := c.ExecStreamIn("alpha", ExecRequest{Command: []string{"true"}}, nil, nil); err != nil {
		t.Fatalf("ExecStreamIn() error = %v", err)
	}
}
