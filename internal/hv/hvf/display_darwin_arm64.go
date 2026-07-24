//go:build darwin && arm64

package hvf

import (
	"context"

	"j5.nz/cc/internal/virtio"
	"j5.nz/cc/internal/vmruntime"
)

func serveDisplayConnections(ctx context.Context, listener virtio.VsockListener, desktop *virtio.Desktop) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = serveDisplayConnection(ctx, conn, desktop)
		_ = conn.Close()
		if ctx.Err() != nil {
			return
		}
	}
}

func serveDisplayConnection(ctx context.Context, conn virtio.VsockConn, desktop *virtio.Desktop) error {
	width, height := desktop.Framebuffer.Size()
	if err := vmruntime.WriteDisplaySize(conn, uint32(width), uint32(height)); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case size := <-desktop.ResizeRequests():
			if err := vmruntime.WriteDisplaySize(conn, size.Width, size.Height); err != nil {
				return err
			}
		}
	}
}
