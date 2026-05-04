package netstack

import "sync"

const maxRetainedPayloadPoolSize = 256 * 1024

var retainedPayloadPool = sync.Pool{
	New: func() any {
		return make([]byte, 0, 4096)
	},
}

var proxyCopyBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, 64*1024)
	},
}

// retainPayload copies packet data that must outlive the caller-owned packet.
// The copy is intentional: guest TX buffers may be released as soon as
// DeliverGuestPacket returns, while TCP/UDP readers and retransmission queues
// can hold payloads for much longer.
func retainPayload(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	if len(src) > maxRetainedPayloadPoolSize {
		dst := make([]byte, len(src))
		copy(dst, src)
		return dst
	}
	dst := retainedPayloadPool.Get().([]byte)
	if cap(dst) < len(src) {
		retainedPayloadPool.Put(dst[:0])
		dst = make([]byte, len(src))
	}
	dst = dst[:len(src)]
	copy(dst, src)
	return dst
}

func releasePayload(buf []byte) {
	if buf == nil || cap(buf) > maxRetainedPayloadPoolSize {
		return
	}
	retainedPayloadPool.Put(buf[:0])
}

func getProxyCopyBuffer(size int) []byte {
	buf := proxyCopyBufferPool.Get().([]byte)
	if cap(buf) < size {
		proxyCopyBufferPool.Put(buf[:cap(buf)])
		return make([]byte, size)
	}
	return buf[:size]
}

func releaseProxyCopyBuffer(buf []byte) {
	if cap(buf) != 64*1024 {
		return
	}
	proxyCopyBufferPool.Put(buf[:cap(buf)])
}
