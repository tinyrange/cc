package vm

import "j5.nz/cc/internal/virtio"

func closeVirtioFSDevices(devices []*virtio.FS) {
	for _, device := range devices {
		if device != nil {
			_ = device.Close()
		}
	}
}
