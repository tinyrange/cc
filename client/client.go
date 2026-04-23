package client

import (
	"bytes"
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
	url    string
	dialer func() (net.Conn, error)
	client http.Client
}

func NewClient(url string, dialer func() (net.Conn, error)) *Client {
	return &Client{
		url:    url,
		dialer: dialer,
		client: http.Client{
			Transport: &http.Transport{
				Dial: func(_, _ string) (net.Conn, error) {
					return dialer()
				},
			},
		},
	}
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

func (c *Client) PrepareImageMetadata(name string) (ImageMetadataState, error) {
	var ret ImageMetadataState
	err := c.postJSONExpectOK("/image/"+name+"/metadata", map[string]any{}, &ret)
	return ret, err
}

func (c *Client) PrepareImageEmulator(name string) (EmulatorState, error) {
	var ret EmulatorState
	err := c.postJSONExpectOK("/image/"+name+"/qemu/download", map[string]any{}, &ret)
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
	resp, err := c.client.Get(c.url + "/image/" + name)
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
	return c.postJSONExpectOK("/image/"+name, req, nil)
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

func (c *Client) CreateInstance(req CreateInstanceRequest) (InstanceState, error) {
	var ret InstanceState
	err := c.postJSONExpectOK("/vm", req, &ret)
	return ret, err
}

func (c *Client) StartInstance(req StartInstanceRequest) (InstanceState, error) {
	var ret InstanceState
	err := c.postJSONExpectOK("/vm/start", req, &ret)
	return ret, err
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

func (c *Client) ShutdownInstance() error {
	return c.postJSONExpectOK("/vm/shutdown", nil, nil)
}

func (c *Client) Run(req RunRequest) (ExecResponse, error) {
	var ret ExecResponse
	err := c.postJSONExpectOK("/vm/run", req, &ret)
	return ret, err
}

func (c *Client) RunEvents(req RunRequest) ([]ExecEvent, error) {
	return c.ExecEvents(ExecRequest{
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

func (c *Client) ExecEvents(req ExecRequest) ([]ExecEvent, error) {
	var events []ExecEvent
	err := c.ExecStream(req, nil, func(event ExecEvent) error {
		events = append(events, event)
		return nil
	})
	return events, err
}

func (c *Client) ExecStream(req ExecRequest, inputs <-chan ExecInput, onEvent func(ExecEvent) error) error {
	wsURL, err := websocketURL(c.url, "/vm/run")
	if err != nil {
		return err
	}
	cfg, err := websocket.NewConfig(wsURL, c.url)
	if err != nil {
		return err
	}
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
	if inputs != nil {
		go func() {
			for input := range inputs {
				_ = websocket.JSON.Send(ws, input)
			}
		}()
	}

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

func decodeErrorResponse(resp *http.Response) error {
	var apiErr ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.Error != "" {
		return fmt.Errorf("%s", apiErr.Error)
	}
	return fmt.Errorf("request failed: %s", resp.Status)
}
