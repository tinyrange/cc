package vm

import (
	"encoding/json"
	"net/http"
	"testing"

	"j5.nz/cc/client"
)

func TestNetworkRuntimeAppliesBlockHostAccess(t *testing.T) {
	runtime, err := newNetworkRuntime(networkDeviceConfig{
		Config: &client.NetworkConfig{
			Enabled:                  true,
			AllowInternet:            true,
			BlockHostAccess:          true,
			AllowedServiceProxyPorts: []int{43210},
		},
	})
	if err != nil {
		t.Fatalf("newNetworkRuntime: %v", err)
	}
	t.Cleanup(func() {
		_ = runtime.Close()
	})

	if err := runtime.stack.EnableDebugHTTP("127.0.0.1:0"); err != nil {
		t.Fatalf("enable debug http: %v", err)
	}
	resp, err := http.Get("http://" + runtime.stack.DebugHTTPAddr() + "/status")
	if err != nil {
		t.Fatalf("get debug status: %v", err)
	}
	defer resp.Body.Close()
	var status struct {
		HostAccess               bool     `json:"hostAccess"`
		ServiceProxy             bool     `json:"serviceProxy"`
		AllowedServiceProxyPorts []uint16 `json:"allowedServiceProxyPorts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode debug status: %v", err)
	}
	if status.HostAccess || status.ServiceProxy {
		t.Fatalf("debug status = %+v, want host access and service proxy disabled", status)
	}
	if len(status.AllowedServiceProxyPorts) != 1 || status.AllowedServiceProxyPorts[0] != 43210 {
		t.Fatalf("debug status allowed ports = %+v, want [43210]", status.AllowedServiceProxyPorts)
	}
}
