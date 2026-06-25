package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/websocket"
)

type Client struct {
	url       string
	dialer    func() (net.Conn, error)
	authToken string
	client    http.Client
}

func NewClient(url string, dialer func() (net.Conn, error)) *Client {
	c := &Client{
		url:    url,
		dialer: dialer,
	}
	c.client = http.Client{
		Transport: &authTransport{
			base: &http.Transport{
				Dial: func(_, _ string) (net.Conn, error) {
					return c.dialer()
				},
			},
			token: func() string {
				return c.authToken
			},
		},
	}
	return c
}

type authTransport struct {
	base  http.RoundTripper
	token func() string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.token != nil {
		if token := strings.TrimSpace(t.token()); token != "" {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return t.base.RoundTrip(req)
}

func (c *Client) SetBearerToken(token string) {
	c.authToken = strings.TrimSpace(token)
}

func (c *Client) HealthCheck() error {
	resp, err := c.client.Get(c.url + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decodeErrorResponse(resp)
	}
	return nil
}

func (c *Client) Shutdown() error {
	req, err := http.NewRequest(http.MethodPost, c.url+"/shutdown", nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return decodeErrorResponse(resp)
	}
	return nil
}

func (c *Client) RouteExists(path string) bool {
	resp, err := c.client.Get(c.url + path)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode != http.StatusNotFound
}

func (c *Client) KernelStatus() (KernelState, error) {
	var ret KernelState
	resp, err := c.client.Get(c.url + "/kernel")
	if err != nil {
		return ret, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ret, decodeErrorResponse(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&ret)
	return ret, err
}

func (c *Client) DownloadKernel(req DownloadRequest) error {
	return c.postJSONExpectOK("/kernel/download", req, nil)
}

func (c *Client) DownloadKernelStream(req DownloadRequest, onEvent func(ProgressEvent) error) error {
	return c.postJSONProgressStream("/kernel/download", req, onEvent)
}

func (c *Client) PrepareImageMetadata(name string) (ImageMetadataState, error) {
	var ret ImageMetadataState
	err := c.postJSONExpectOK("/image/"+imagePathName(name)+"/metadata", map[string]any{}, &ret)
	return ret, err
}

func (c *Client) PrepareImageEmulator(name string) (EmulatorState, error) {
	var ret EmulatorState
	err := c.postJSONExpectOK("/image/"+imagePathName(name)+"/qemu/download", map[string]any{}, &ret)
	return ret, err
}

func (c *Client) ListImages() ([]ImageState, error) {
	resp, err := c.client.Get(c.url + "/image")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeErrorResponse(resp)
	}
	var ret []ImageState
	if err := json.NewDecoder(resp.Body).Decode(&ret); err != nil {
		return nil, err
	}
	return ret, nil
}

func (c *Client) GetImage(name string) (ImageState, error) {
	var ret ImageState
	resp, err := c.client.Get(c.url + "/image/" + imagePathName(name))
	if err != nil {
		return ret, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ret, decodeErrorResponse(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&ret)
	return ret, err
}

func (c *Client) PullImage(name string, req PullImageRequest) error {
	return c.postJSONExpectOK("/image/"+imagePathName(name), req, nil)
}

func (c *Client) PullImageStream(name string, req PullImageRequest, onEvent func(ProgressEvent) error) error {
	return c.PullImageStreamContext(context.Background(), name, req, onEvent)
}

func (c *Client) PullImageStreamContext(ctx context.Context, name string, req PullImageRequest, onEvent func(ProgressEvent) error) error {
	return c.postJSONProgressStreamContext(ctx, "/image/"+imagePathName(name), req, onEvent)
}

func (c *Client) DeleteImage(name string) error {
	req, err := http.NewRequest(http.MethodDelete, c.url+"/image/"+imagePathName(name), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decodeErrorResponse(resp)
	}
	return nil
}

func (c *Client) SaveInstanceImage(id string, req SaveImageRequest) (ImageState, error) {
	var ret ImageState
	err := c.postJSONExpectOK("/vm/"+imagePathName(id)+"/save", req, &ret)
	return ret, err
}

func (c *Client) FlushInstance(id string) error {
	return c.postJSONExpectOK("/vm/"+imagePathName(id)+"/flush", map[string]any{}, nil)
}

func imagePathName(name string) string {
	return url.PathEscape(name)
}

func (c *Client) VMSupported() (VMSupportedResponse, error) {
	var ret VMSupportedResponse
	resp, err := c.client.Get(c.url + "/vm/supported")
	if err != nil {
		return ret, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ret, decodeErrorResponse(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&ret)
	return ret, err
}

func (c *Client) Capabilities() (CapabilitiesResponse, error) {
	var ret CapabilitiesResponse
	resp, err := c.client.Get(c.url + "/capabilities")
	if err != nil {
		return ret, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ret, decodeErrorResponse(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&ret)
	return ret, err
}

func (c *Client) CreateWatchdogLease(req WatchdogLeaseRequest) (WatchdogLeaseResponse, error) {
	var ret WatchdogLeaseResponse
	err := c.postJSONExpectOK("/watchdog/lease", req, &ret)
	return ret, err
}

func (c *Client) FeedWatchdogLease(id string) error {
	return c.postJSONExpectOK("/watchdog/lease/feed", WatchdogLeaseRequest{LeaseID: id}, nil)
}

func (c *Client) ReleaseWatchdogLease(id string) error {
	return c.postJSONExpectOK("/watchdog/lease/release", WatchdogLeaseRequest{LeaseID: id}, nil)
}

func (c *Client) CreateInstance(req CreateInstanceRequest) (InstanceState, error) {
	var ret InstanceState
	err := c.postJSONExpectOK("/vm", req, &ret)
	return ret, err
}

func (c *Client) CreateInstanceWithID(id string, req CreateInstanceRequest) (InstanceState, error) {
	req.ID = id
	return c.CreateInstance(req)
}

func (c *Client) CreateInstanceStream(req CreateInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	return c.postJSONBootStream("/vm", req, onEvent)
}

func (c *Client) CreateInstanceStreamWithID(id string, req CreateInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	req.ID = id
	return c.CreateInstanceStream(req, onEvent)
}

func (c *Client) StartInstance(req StartInstanceRequest) (InstanceState, error) {
	var ret InstanceState
	err := c.postJSONExpectOK("/vm/start", req, &ret)
	return ret, err
}

func (c *Client) StartInstanceWithID(id string, req StartInstanceRequest) (InstanceState, error) {
	req.ID = id
	return c.StartInstance(req)
}

func (c *Client) StartInstanceStream(req StartInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	return c.StartInstanceStreamContext(context.Background(), req, onEvent)
}

func (c *Client) StartInstanceStreamContext(ctx context.Context, req StartInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	return c.postJSONBootStreamContext(ctx, "/vm/start", req, onEvent)
}

func (c *Client) StartInstanceStreamWithID(id string, req StartInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	return c.StartInstanceStreamWithIDContext(context.Background(), id, req, onEvent)
}

func (c *Client) StartInstanceStreamWithIDContext(ctx context.Context, id string, req StartInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	req.ID = id
	return c.StartInstanceStreamContext(ctx, req, onEvent)
}

func (c *Client) InstanceStatus() (InstanceState, error) {
	var ret InstanceState
	resp, err := c.client.Get(c.url + "/vm/status")
	if err != nil {
		return ret, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ret, decodeErrorResponse(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&ret)
	return ret, err
}

func (c *Client) InstanceStatuses() ([]InstanceState, error) {
	resp, err := c.client.Get(c.url + "/vm")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeErrorResponse(resp)
	}
	var ret []InstanceState
	if err := json.NewDecoder(resp.Body).Decode(&ret); err != nil {
		return nil, err
	}
	return ret, nil
}

func (c *Client) InstanceStatusOf(id string) (InstanceState, error) {
	var ret InstanceState
	resp, err := c.client.Get(c.url + "/vm/status" + idQuery(id))
	if err != nil {
		return ret, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ret, decodeErrorResponse(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&ret)
	return ret, err
}

func (c *Client) ConsoleHistory(id string) (string, error) {
	var ret ConsoleHistoryResponse
	resp, err := c.client.Get(c.url + "/vm/console" + idQuery(id))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", decodeErrorResponse(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&ret); err != nil {
		return "", err
	}
	return ret.History, nil
}

func (c *Client) ShutdownInstance() error {
	return c.postJSONExpectOK("/vm/shutdown", nil, nil)
}

func (c *Client) ShutdownInstanceWithID(id string) error {
	return c.postJSONExpectOK("/vm/shutdown"+idQuery(id), nil, nil)
}

func (c *Client) AddPortForwardTo(id string, forward PortForward) error {
	return c.postJSONExpectOK("/vm/forward"+idQuery(id), forward, nil)
}

func (c *Client) AllowServiceProxyPortTo(id string, port int) error {
	return c.postJSONExpectOK("/vm/service-proxy-port"+idQuery(id), ServiceProxyPortRequest{Port: port}, nil)
}

func (c *Client) Run(req RunRequest) (ExecResponse, error) {
	var ret ExecResponse
	err := c.postJSONExpectOK("/vm/run", req, &ret)
	return ret, err
}

func (c *Client) RunIn(id string, req RunRequest) (ExecResponse, error) {
	req.ID = id
	return c.Run(req)
}

func (c *Client) RunStream(req RunRequest, onEvent func(ExecEvent) error) error {
	return c.RunStreamContext(context.Background(), req, onEvent)
}

func (c *Client) RunStreamContext(ctx context.Context, req RunRequest, onEvent func(ExecEvent) error) error {
	return c.postJSONExecStream(ctx, "/vm/run", req, onEvent)
}

func (c *Client) RunStreamIn(id string, req RunRequest, onEvent func(ExecEvent) error) error {
	return c.RunStreamInContext(context.Background(), id, req, onEvent)
}

func (c *Client) RunStreamInContext(ctx context.Context, id string, req RunRequest, onEvent func(ExecEvent) error) error {
	req.ID = id
	return c.RunStreamContext(ctx, req, onEvent)
}

func (c *Client) RunInteractiveStreamIn(id string, req RunRequest, inputs <-chan ExecInput, onEvent func(ExecEvent) error) error {
	return c.RunInteractiveStreamInContext(context.Background(), id, req, inputs, onEvent)
}

func (c *Client) RunInteractiveStreamInContext(ctx context.Context, id string, req RunRequest, inputs <-chan ExecInput, onEvent func(ExecEvent) error) error {
	req.ID = id
	return c.RunInteractiveStreamContext(ctx, req, inputs, onEvent)
}

func (c *Client) RunInteractiveStream(req RunRequest, inputs <-chan ExecInput, onEvent func(ExecEvent) error) error {
	return c.RunInteractiveStreamContext(context.Background(), req, inputs, onEvent)
}

func (c *Client) RunInteractiveStreamContext(ctx context.Context, req RunRequest, inputs <-chan ExecInput, onEvent func(ExecEvent) error) error {
	wsURL, err := websocketURL(c.url, "/vm/run/stream")
	if err != nil {
		return err
	}
	cfg, err := websocket.NewConfig(wsURL, c.url)
	if err != nil {
		return err
	}
	c.applyWebSocketAuth(cfg)
	if c.dialer != nil {
		cfg.Dialer = &net.Dialer{}
	}
	ws, err := websocket.DialConfig(cfg)
	if err != nil {
		return err
	}
	defer ws.Close()

	if err := websocket.JSON.Send(ws, req); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = ws.Close()
		case <-ctxDone:
		}
	}()
	defer close(ctxDone)
	sendErr := streamExecInputsToWebSocket(ws, inputs)
	err = receiveExecEventsFromWebSocket(ws, onEvent)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if sendErrValue := currentWebSocketSendError(sendErr); sendErrValue != nil {
		return sendErrValue
	}
	return err
}

func streamExecInputsToWebSocket(ws *websocket.Conn, inputs <-chan ExecInput) <-chan error {
	done := make(chan error, 1)
	if inputs == nil {
		go func() {
			done <- websocket.JSON.Send(ws, ExecInput{Kind: "stdin_close"})
		}()
		return done
	}
	go func() {
		stdinClosed := false
		for input := range inputs {
			if input.Kind == "stdin_close" {
				if stdinClosed {
					continue
				}
				stdinClosed = true
			} else if input.Kind == "stdin" && stdinClosed {
				continue
			}
			if err := websocket.JSON.Send(ws, input); err != nil {
				done <- err
				_ = ws.Close()
				return
			}
		}
		if !stdinClosed {
			if err := websocket.JSON.Send(ws, ExecInput{Kind: "stdin_close"}); err != nil {
				done <- err
				_ = ws.Close()
				return
			}
		}
		done <- nil
	}()
	return done
}

func receiveExecEventsFromWebSocket(ws *websocket.Conn, onEvent func(ExecEvent) error) error {
	for {
		var event ExecEvent
		if err := websocket.JSON.Receive(ws, &event); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if onEvent != nil {
			if err := onEvent(event); err != nil {
				return err
			}
		}
		if event.Kind == "exit" || event.Kind == "error" {
			break
		}
	}
	return nil
}

func currentWebSocketSendError(sendErr <-chan error) error {
	if sendErr == nil {
		return nil
	}
	select {
	case err := <-sendErr:
		return err
	default:
		return nil
	}
}

func (c *Client) RunEvents(req RunRequest) ([]ExecEvent, error) {
	return c.ExecEvents(ExecRequest{
		ID:         req.ID,
		Command:    append([]string(nil), req.Command...),
		Env:        append([]string(nil), req.Env...),
		RootDir:    req.RootDir,
		ReplaceEnv: req.ReplaceEnv,
		WorkDir:    req.WorkDir,
		User:       req.User,
		Stdin:      append([]byte(nil), req.Stdin...),
		TTY:        req.TTY,
		Cols:       req.Cols,
		Rows:       req.Rows,
	})
}

func (c *Client) RunEventsIn(id string, req RunRequest) ([]ExecEvent, error) {
	req.ID = id
	return c.RunEvents(req)
}

func (c *Client) ExecEvents(req ExecRequest) ([]ExecEvent, error) {
	var events []ExecEvent
	err := c.ExecStream(req, nil, func(event ExecEvent) error {
		events = append(events, event)
		return nil
	})
	return events, err
}

func (c *Client) ExecEventsIn(id string, req ExecRequest) ([]ExecEvent, error) {
	req.ID = id
	return c.ExecEvents(req)
}

func (c *Client) ExecStream(req ExecRequest, inputs <-chan ExecInput, onEvent func(ExecEvent) error) error {
	return c.ExecStreamContext(context.Background(), req, inputs, onEvent)
}

func (c *Client) ExecStreamContext(ctx context.Context, req ExecRequest, inputs <-chan ExecInput, onEvent func(ExecEvent) error) error {
	wsURL, err := websocketURL(c.url, "/vm/run")
	if err != nil {
		return err
	}
	cfg, err := websocket.NewConfig(wsURL, c.url)
	if err != nil {
		return err
	}
	c.applyWebSocketAuth(cfg)
	if c.dialer != nil {
		cfg.Dialer = &net.Dialer{}
	}
	ws, err := websocket.DialConfig(cfg)
	if err != nil {
		return err
	}
	defer ws.Close()

	if err := websocket.JSON.Send(ws, req); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = ws.Close()
		case <-ctxDone:
		}
	}()
	defer close(ctxDone)
	sendErr := streamExecInputsToWebSocket(ws, inputs)
	err = receiveExecEventsFromWebSocket(ws, onEvent)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if sendErrValue := currentWebSocketSendError(sendErr); sendErrValue != nil {
		return sendErrValue
	}
	return err
}

func (c *Client) applyWebSocketAuth(cfg *websocket.Config) {
	if cfg == nil {
		return
	}
	if token := strings.TrimSpace(c.authToken); token != "" {
		if cfg.Header == nil {
			cfg.Header = http.Header{}
		}
		cfg.Header.Set("Authorization", "Bearer "+token)
	}
}

func (c *Client) ExecStreamIn(id string, req ExecRequest, inputs <-chan ExecInput, onEvent func(ExecEvent) error) error {
	return c.ExecStreamInContext(context.Background(), id, req, inputs, onEvent)
}

func (c *Client) ExecStreamInContext(ctx context.Context, id string, req ExecRequest, inputs <-chan ExecInput, onEvent func(ExecEvent) error) error {
	req.ID = id
	return c.ExecStreamContext(ctx, req, inputs, onEvent)
}

func websocketURL(baseURL, path string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported base URL scheme %q", u.Scheme)
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func idQuery(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return "?id=" + url.QueryEscape(id)
}

func (c *Client) StartVM(req StartVMRequest) (VMState, error) { return c.CreateInstance(req) }
func (c *Client) VMStatus() (VMState, error)                  { return c.InstanceStatus() }
func (c *Client) ShutdownVM() error                           { return c.ShutdownInstance() }
func (c *Client) RunVM(req StartVMRequest) (RunVMResponse, error) {
	return c.Run(RunRequest{
		Image:    req.Image,
		MemoryMB: req.MemoryMB,
		CPUs:     req.CPUs,
		Dmesg:    req.Dmesg,
	})
}

func (c *Client) postJSONExpectOK(path string, reqBody any, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
			return err
		}
		body = buf
	}

	req, err := http.NewRequest(http.MethodPost, c.url+path, body)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return decodeErrorResponse(resp)
	}

	if respBody == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}

func (c *Client) postJSONProgressStream(path string, reqBody any, onEvent func(ProgressEvent) error) error {
	return c.postJSONProgressStreamContext(context.Background(), path, reqBody, onEvent)
}

func (c *Client) postJSONProgressStreamContext(ctx context.Context, path string, reqBody any, onEvent func(ProgressEvent) error) error {
	resp, err := c.postJSONStreamContext(ctx, path, reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	for {
		var event ProgressEvent
		if err := dec.Decode(&event); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if onEvent != nil {
			if err := onEvent(event); err != nil {
				return err
			}
		}
		if event.Status == "error" {
			if event.Error != "" {
				return fmt.Errorf("%s", event.Error)
			}
			return fmt.Errorf("streamed operation failed")
		}
	}
}

func (c *Client) postJSONBootStream(path string, reqBody any, onEvent func(BootEvent) error) (InstanceState, error) {
	return c.postJSONBootStreamContext(context.Background(), path, reqBody, onEvent)
}

func (c *Client) postJSONBootStreamContext(ctx context.Context, path string, reqBody any, onEvent func(BootEvent) error) (InstanceState, error) {
	resp, err := c.postJSONStreamContext(ctx, path, reqBody)
	if err != nil {
		return InstanceState{}, err
	}
	defer resp.Body.Close()

	var state InstanceState
	dec := json.NewDecoder(resp.Body)
	for {
		var event BootEvent
		if err := dec.Decode(&event); err != nil {
			if err == io.EOF {
				if state.Status == "" {
					return state, fmt.Errorf("boot stream ended before ready")
				}
				return state, nil
			}
			return state, err
		}
		if onEvent != nil {
			if err := onEvent(event); err != nil {
				return state, err
			}
		}
		if event.Kind == "ready" {
			state = event.State
		}
		if event.Kind == "error" {
			if event.Error != "" {
				return state, fmt.Errorf("%s", event.Error)
			}
			return state, fmt.Errorf("boot failed")
		}
	}
}

func (c *Client) postJSONExecStream(ctx context.Context, path string, reqBody any, onEvent func(ExecEvent) error) error {
	resp, err := c.postJSONStreamContext(ctx, path, reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	for {
		var event ExecEvent
		if err := dec.Decode(&event); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if onEvent != nil {
			if err := onEvent(event); err != nil {
				return err
			}
		}
		if event.Kind == "error" {
			if event.Error != "" {
				return fmt.Errorf("%s", event.Error)
			}
			return fmt.Errorf("streamed exec failed")
		}
		if event.Kind == "exit" {
			return nil
		}
	}
}

func (c *Client) postJSONStream(path string, reqBody any) (*http.Response, error) {
	return c.postJSONStreamContext(context.Background(), path, reqBody)
}

func (c *Client) postJSONStreamContext(ctx context.Context, path string, reqBody any) (*http.Response, error) {
	var body io.Reader
	if reqBody != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
			return nil, err
		}
		body = buf
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+path+"?stream=1", body)
	if err != nil {
		return nil, err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, decodeErrorResponse(resp)
	}
	return resp, nil
}

func decodeErrorResponse(resp *http.Response) error {
	var apiErr ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.Error != "" {
		return fmt.Errorf("%s", apiErr.Error)
	}
	return fmt.Errorf("request failed: %s", resp.Status)
}
