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
	"time"

	"golang.org/x/net/websocket"
)

const (
	clientConnectTimeout        = 30 * time.Second
	clientTLSHandshakeTimeout   = 10 * time.Second
	clientResponseHeaderTimeout = 30 * time.Second
	clientIdleConnTimeout       = 90 * time.Second
)

type Client struct {
	url         string
	dialContext func(context.Context) (net.Conn, error)
	headers     http.Header
	client      http.Client
}

func NewClient(url string, dialer func() (net.Conn, error)) *Client {
	if dialer == nil {
		return NewClientContext(url, nil)
	}
	return NewClientContext(url, func(context.Context) (net.Conn, error) {
		return dialer()
	})
}

func NewClientContext(url string, dialer func(context.Context) (net.Conn, error)) *Client {
	c := &Client{
		url:         url,
		dialContext: dialer,
	}
	transport := &http.Transport{
		TLSHandshakeTimeout:   clientTLSHandshakeTimeout,
		ResponseHeaderTimeout: clientResponseHeaderTimeout,
		IdleConnTimeout:       clientIdleConnTimeout,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCtx, cancel := context.WithTimeout(ctx, clientConnectTimeout)
		defer cancel()
		if dialer != nil {
			return dialer(dialCtx)
		}
		return (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext(dialCtx, network, address)
	}
	c.client = http.Client{
		Transport: &authTransport{
			base: transport,
			token: func() string {
				return c.headers.Get("Authorization")
			},
			headers: func() http.Header {
				return c.headers.Clone()
			},
		},
	}
	return c
}

type authTransport struct {
	base    http.RoundTripper
	token   func() string
	headers func() http.Header
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.headers != nil {
		for key, values := range t.headers() {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}
	if t.token != nil {
		if token := strings.TrimSpace(t.token()); token != "" {
			req.Header.Set("Authorization", token)
		}
	}
	return t.base.RoundTrip(req)
}

func (c *Client) SetBearerToken(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		c.SetHeader("Authorization", "")
		return
	}
	c.SetHeader("Authorization", "Bearer "+token)
}

func (c *Client) SetHeader(key, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return
	}
	if c.headers == nil {
		c.headers = http.Header{}
	}
	if value == "" {
		c.headers.Del(key)
		return
	}
	c.headers.Set(key, value)
}

func (c *Client) HealthCheck() error {
	return c.HealthCheckContext(context.Background())
}

func (c *Client) HealthCheckContext(ctx context.Context) error {
	req, err := http.NewRequestWithContext(contextOrBackground(ctx), http.MethodGet, c.url+"/healthz", nil)
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

func (c *Client) Shutdown() error {
	return c.ShutdownContext(context.Background())
}

func (c *Client) ShutdownContext(ctx context.Context) error {
	req, err := http.NewRequestWithContext(contextOrBackground(ctx), http.MethodPost, c.url+"/shutdown", nil)
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
	exists, _ := c.RouteExistsContext(context.Background(), path)
	return exists
}

func (c *Client) RouteExistsContext(ctx context.Context, path string) (bool, error) {
	req, err := http.NewRequestWithContext(contextOrBackground(ctx), http.MethodGet, c.url+path, nil)
	if err != nil {
		return false, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode != http.StatusNotFound, nil
}

func (c *Client) KernelStatus() (KernelState, error) {
	return c.KernelStatusContext(context.Background())
}

func (c *Client) KernelStatusContext(ctx context.Context) (KernelState, error) {
	var ret KernelState
	err := c.getJSONContext(ctx, "/kernel", &ret)
	return ret, err
}

func (c *Client) DownloadKernel(req DownloadRequest) error {
	return c.DownloadKernelContext(context.Background(), req)
}

func (c *Client) DownloadKernelContext(ctx context.Context, req DownloadRequest) error {
	return c.postJSONExpectOKContext(ctx, "/kernel/download", req, nil)
}

func (c *Client) DownloadKernelStream(req DownloadRequest, onEvent func(ProgressEvent) error) error {
	return c.DownloadKernelStreamContext(context.Background(), req, onEvent)
}

func (c *Client) DownloadKernelStreamContext(ctx context.Context, req DownloadRequest, onEvent func(ProgressEvent) error) error {
	return c.postJSONProgressStreamContext(ctx, "/kernel/download", req, onEvent)
}

func (c *Client) PrepareImageMetadata(name string) (ImageMetadataState, error) {
	return c.PrepareImageMetadataContext(context.Background(), name)
}

func (c *Client) PrepareImageMetadataContext(ctx context.Context, name string) (ImageMetadataState, error) {
	var ret ImageMetadataState
	err := c.postJSONExpectOKContext(ctx, "/image/"+imagePathName(name)+"/metadata", map[string]any{}, &ret)
	return ret, err
}

func (c *Client) PrepareImageEmulator(name string) (EmulatorState, error) {
	return c.PrepareImageEmulatorContext(context.Background(), name)
}

func (c *Client) PrepareImageEmulatorContext(ctx context.Context, name string) (EmulatorState, error) {
	var ret EmulatorState
	err := c.postJSONExpectOKContext(ctx, "/image/"+imagePathName(name)+"/qemu/download", map[string]any{}, &ret)
	return ret, err
}

func (c *Client) ListImages() ([]ImageState, error) {
	return c.ListImagesContext(context.Background())
}

func (c *Client) ListImagesContext(ctx context.Context) ([]ImageState, error) {
	var ret []ImageState
	if err := c.getJSONContext(ctx, "/image", &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

func (c *Client) GetImage(name string) (ImageState, error) {
	return c.GetImageContext(context.Background(), name)
}

func (c *Client) GetImageContext(ctx context.Context, name string) (ImageState, error) {
	var ret ImageState
	err := c.getJSONContext(ctx, "/image/"+imagePathName(name), &ret)
	return ret, err
}

func (c *Client) PullImage(name string, req PullImageRequest) error {
	return c.PullImageContext(context.Background(), name, req)
}

func (c *Client) PullImageContext(ctx context.Context, name string, req PullImageRequest) error {
	return c.postJSONExpectOKContext(ctx, "/image/"+imagePathName(name), req, nil)
}

func (c *Client) PullImageStream(name string, req PullImageRequest, onEvent func(ProgressEvent) error) error {
	return c.PullImageStreamContext(context.Background(), name, req, onEvent)
}

func (c *Client) PullImageStreamContext(ctx context.Context, name string, req PullImageRequest, onEvent func(ProgressEvent) error) error {
	return c.postJSONProgressStreamContext(ctx, "/image/"+imagePathName(name), req, onEvent)
}

func (c *Client) DeleteImage(name string) error {
	return c.DeleteImageContext(context.Background(), name)
}

func (c *Client) DeleteImageContext(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(contextOrBackground(ctx), http.MethodDelete, c.url+"/image/"+imagePathName(name), nil)
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
	return c.SaveInstanceImageContext(context.Background(), id, req)
}

func (c *Client) SaveInstanceImageContext(ctx context.Context, id string, req SaveImageRequest) (ImageState, error) {
	var ret ImageState
	err := c.postJSONExpectOKContext(ctx, "/vm/"+imagePathName(id)+"/save", req, &ret)
	return ret, err
}

func (c *Client) FlushInstance(id string) error {
	return c.FlushInstanceContext(context.Background(), id)
}

func (c *Client) FlushInstanceContext(ctx context.Context, id string) error {
	return c.postJSONExpectOKContext(ctx, "/vm/"+imagePathName(id)+"/flush", map[string]any{}, nil)
}

func imagePathName(name string) string {
	return url.PathEscape(name)
}

func (c *Client) VMSupported() (VMSupportedResponse, error) {
	return c.VMSupportedContext(context.Background())
}

func (c *Client) VMSupportedContext(ctx context.Context) (VMSupportedResponse, error) {
	var ret VMSupportedResponse
	err := c.getJSONContext(ctx, "/vm/supported", &ret)
	return ret, err
}

func (c *Client) Capabilities() (CapabilitiesResponse, error) {
	return c.CapabilitiesContext(context.Background())
}

func (c *Client) CapabilitiesContext(ctx context.Context) (CapabilitiesResponse, error) {
	var ret CapabilitiesResponse
	err := c.getJSONContext(ctx, "/capabilities", &ret)
	return ret, err
}

func (c *Client) CreateWatchdogLease(req WatchdogLeaseRequest) (WatchdogLeaseResponse, error) {
	return c.CreateWatchdogLeaseContext(context.Background(), req)
}

func (c *Client) CreateWatchdogLeaseContext(ctx context.Context, req WatchdogLeaseRequest) (WatchdogLeaseResponse, error) {
	var ret WatchdogLeaseResponse
	err := c.postJSONExpectOKContext(ctx, "/watchdog/lease", req, &ret)
	return ret, err
}

func (c *Client) FeedWatchdogLease(id string) error {
	return c.FeedWatchdogLeaseContext(context.Background(), id)
}

func (c *Client) FeedWatchdogLeaseContext(ctx context.Context, id string) error {
	return c.postJSONExpectOKContext(ctx, "/watchdog/lease/feed", WatchdogLeaseRequest{LeaseID: id}, nil)
}

func (c *Client) ReleaseWatchdogLease(id string) error {
	return c.ReleaseWatchdogLeaseContext(context.Background(), id)
}

func (c *Client) ReleaseWatchdogLeaseContext(ctx context.Context, id string) error {
	return c.postJSONExpectOKContext(ctx, "/watchdog/lease/release", WatchdogLeaseRequest{LeaseID: id}, nil)
}

func (c *Client) CreateInstance(req CreateInstanceRequest) (InstanceState, error) {
	return c.CreateInstanceContext(context.Background(), req)
}

func (c *Client) CreateInstanceContext(ctx context.Context, req CreateInstanceRequest) (InstanceState, error) {
	var ret InstanceState
	err := c.postJSONExpectOKContext(ctx, "/vm", req, &ret)
	return ret, err
}

func (c *Client) CreateInstanceWithID(id string, req CreateInstanceRequest) (InstanceState, error) {
	return c.CreateInstanceWithIDContext(context.Background(), id, req)
}

func (c *Client) CreateInstanceWithIDContext(ctx context.Context, id string, req CreateInstanceRequest) (InstanceState, error) {
	req.ID = id
	return c.CreateInstanceContext(ctx, req)
}

func (c *Client) CreateInstanceStream(req CreateInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	return c.CreateInstanceStreamContext(context.Background(), req, onEvent)
}

func (c *Client) CreateInstanceStreamContext(ctx context.Context, req CreateInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	return c.postJSONBootStreamContext(ctx, "/vm", req, onEvent)
}

func (c *Client) CreateInstanceStreamWithID(id string, req CreateInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	return c.CreateInstanceStreamWithIDContext(context.Background(), id, req, onEvent)
}

func (c *Client) CreateInstanceStreamWithIDContext(ctx context.Context, id string, req CreateInstanceRequest, onEvent func(BootEvent) error) (InstanceState, error) {
	req.ID = id
	return c.CreateInstanceStreamContext(ctx, req, onEvent)
}

func (c *Client) StartInstance(req StartInstanceRequest) (InstanceState, error) {
	return c.StartInstanceContext(context.Background(), req)
}

func (c *Client) StartInstanceContext(ctx context.Context, req StartInstanceRequest) (InstanceState, error) {
	var ret InstanceState
	err := c.postJSONExpectOKContext(ctx, "/vm/start", req, &ret)
	return ret, err
}

func (c *Client) StartInstanceWithID(id string, req StartInstanceRequest) (InstanceState, error) {
	return c.StartInstanceWithIDContext(context.Background(), id, req)
}

func (c *Client) StartInstanceWithIDContext(ctx context.Context, id string, req StartInstanceRequest) (InstanceState, error) {
	req.ID = id
	return c.StartInstanceContext(ctx, req)
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
	return c.InstanceStatusContext(context.Background())
}

func (c *Client) InstanceStatusContext(ctx context.Context) (InstanceState, error) {
	var ret InstanceState
	err := c.getJSONContext(ctx, "/vm/status", &ret)
	return ret, err
}

func (c *Client) InstanceStatuses() ([]InstanceState, error) {
	return c.InstanceStatusesContext(context.Background())
}

func (c *Client) InstanceStatusesContext(ctx context.Context) ([]InstanceState, error) {
	var ret []InstanceState
	if err := c.getJSONContext(ctx, "/vm", &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

func (c *Client) InstanceStatusOf(id string) (InstanceState, error) {
	return c.InstanceStatusOfContext(context.Background(), id)
}

func (c *Client) InstanceStatusOfContext(ctx context.Context, id string) (InstanceState, error) {
	var ret InstanceState
	err := c.getJSONContext(ctx, "/vm/status"+idQuery(id), &ret)
	return ret, err
}

func (c *Client) ConsoleHistory(id string) (string, error) {
	return c.ConsoleHistoryContext(context.Background(), id)
}

func (c *Client) ConsoleHistoryContext(ctx context.Context, id string) (string, error) {
	var ret ConsoleHistoryResponse
	if err := c.getJSONContext(ctx, "/vm/console"+idQuery(id), &ret); err != nil {
		return "", err
	}
	return ret.History, nil
}

func (c *Client) ShutdownInstance() error {
	return c.ShutdownInstanceContext(context.Background())
}

func (c *Client) ShutdownInstanceContext(ctx context.Context) error {
	return c.postJSONExpectOKContext(ctx, "/vm/shutdown", nil, nil)
}

func (c *Client) ShutdownInstanceWithID(id string) error {
	return c.ShutdownInstanceWithIDContext(context.Background(), id)
}

func (c *Client) ShutdownInstanceWithIDContext(ctx context.Context, id string) error {
	return c.postJSONExpectOKContext(ctx, "/vm/shutdown"+idQuery(id), nil, nil)
}

func (c *Client) AddPortForwardTo(id string, forward PortForward) error {
	return c.AddPortForwardToContext(context.Background(), id, forward)
}

func (c *Client) AddPortForwardToContext(ctx context.Context, id string, forward PortForward) error {
	return c.postJSONExpectOKContext(ctx, "/vm/forward"+idQuery(id), forward, nil)
}

func (c *Client) AllowServiceProxyPortTo(id string, port int) error {
	return c.AllowServiceProxyPortToContext(context.Background(), id, port)
}

func (c *Client) AllowServiceProxyPortToContext(ctx context.Context, id string, port int) error {
	return c.postJSONExpectOKContext(ctx, "/vm/service-proxy-port"+idQuery(id), ServiceProxyPortRequest{Port: port}, nil)
}

func (c *Client) Run(req RunRequest) (ExecResponse, error) {
	return c.RunContext(context.Background(), req)
}

func (c *Client) RunContext(ctx context.Context, req RunRequest) (ExecResponse, error) {
	var ret ExecResponse
	err := c.postJSONExpectOKContext(ctx, "/vm/run", req, &ret)
	return ret, err
}

func (c *Client) RunIn(id string, req RunRequest) (ExecResponse, error) {
	return c.RunInContext(context.Background(), id, req)
}

func (c *Client) RunInContext(ctx context.Context, id string, req RunRequest) (ExecResponse, error) {
	req.ID = id
	return c.RunContext(ctx, req)
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
	if c.dialContext != nil {
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
	return c.RunEventsContext(context.Background(), req)
}

func (c *Client) RunEventsContext(ctx context.Context, req RunRequest) ([]ExecEvent, error) {
	return c.ExecEventsContext(ctx, ExecRequest{
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
	return c.RunEventsInContext(context.Background(), id, req)
}

func (c *Client) RunEventsInContext(ctx context.Context, id string, req RunRequest) ([]ExecEvent, error) {
	req.ID = id
	return c.RunEventsContext(ctx, req)
}

func (c *Client) ExecEvents(req ExecRequest) ([]ExecEvent, error) {
	return c.ExecEventsContext(context.Background(), req)
}

func (c *Client) ExecEventsContext(ctx context.Context, req ExecRequest) ([]ExecEvent, error) {
	var events []ExecEvent
	err := c.ExecStreamContext(ctx, req, nil, func(event ExecEvent) error {
		events = append(events, event)
		return nil
	})
	return events, err
}

func (c *Client) ExecEventsIn(id string, req ExecRequest) ([]ExecEvent, error) {
	return c.ExecEventsInContext(context.Background(), id, req)
}

func (c *Client) ExecEventsInContext(ctx context.Context, id string, req ExecRequest) ([]ExecEvent, error) {
	req.ID = id
	return c.ExecEventsContext(ctx, req)
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
	if c.dialContext != nil {
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
	if c.headers != nil {
		if cfg.Header == nil {
			cfg.Header = http.Header{}
		}
		for key, values := range c.headers {
			for _, value := range values {
				cfg.Header.Add(key, value)
			}
		}
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

func (c *Client) StartVM(req StartVMRequest) (VMState, error) {
	return c.StartVMContext(context.Background(), req)
}
func (c *Client) StartVMContext(ctx context.Context, req StartVMRequest) (VMState, error) {
	return c.CreateInstanceContext(ctx, req)
}
func (c *Client) VMStatus() (VMState, error) { return c.VMStatusContext(context.Background()) }
func (c *Client) VMStatusContext(ctx context.Context) (VMState, error) {
	return c.InstanceStatusContext(ctx)
}
func (c *Client) ShutdownVM() error { return c.ShutdownVMContext(context.Background()) }
func (c *Client) ShutdownVMContext(ctx context.Context) error {
	return c.ShutdownInstanceContext(ctx)
}
func (c *Client) RunVM(req StartVMRequest) (RunVMResponse, error) {
	return c.RunVMContext(context.Background(), req)
}
func (c *Client) RunVMContext(ctx context.Context, req StartVMRequest) (RunVMResponse, error) {
	return c.RunContext(ctx, RunRequest{
		Image:     req.Image,
		MemoryMB:  req.MemoryMB,
		BalloonMB: req.BalloonMB,
		CPUs:      req.CPUs,
		Dmesg:     req.Dmesg,
	})
}

func (c *Client) postJSONExpectOK(path string, reqBody any, respBody any) error {
	return c.postJSONExpectOKContext(context.Background(), path, reqBody, respBody)
}

func (c *Client) postJSONExpectOKContext(ctx context.Context, path string, reqBody any, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
			return err
		}
		body = buf
	}

	req, err := http.NewRequestWithContext(contextOrBackground(ctx), http.MethodPost, c.url+path, body)
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

func (c *Client) getJSONContext(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(contextOrBackground(ctx), http.MethodGet, c.url+path, nil)
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
	return json.NewDecoder(resp.Body).Decode(target)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
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

	req, err := http.NewRequestWithContext(contextOrBackground(ctx), http.MethodPost, c.url+path+"?stream=1", body)
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
