package client

import (
	"fmt"
	"net"
	"net/http"
)

type ServerHello struct {
	Addr string `json:"addr"`
}

type Client struct {
	url    string
	client http.Client
}

func (c *Client) HealthCheck() error {
	resp, err := c.client.Get(c.url + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: %s", resp.Status)
	}
	return nil
}

func (c *Client) Shutdown() error {
	resp, err := c.client.Post(c.url+"/shutdown", "text/plain", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shutdown failed: %s", resp.Status)
	}

	return nil
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
