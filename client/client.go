package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
)

type Client struct {
	url    string
	client http.Client
}

func NewClient(url string, dialer func() (net.Conn, error)) *Client {
	return &Client{
		url: url,
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

func (c *Client) StartVM(req StartVMRequest) (VMState, error) {
	var ret VMState
	err := c.postJSONExpectOK("/vm", req, &ret)
	return ret, err
}

func (c *Client) VMStatus() (VMState, error) {
	var ret VMState
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

func (c *Client) ShutdownVM() error {
	return c.postJSONExpectOK("/vm/shutdown", nil, nil)
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
