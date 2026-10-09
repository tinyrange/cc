// Package fs exposes cc's virtio-fs backend contract without linking a VM
// runtime or requiring embedded guest-init payloads.
package fs

import "j5.nz/cc/internal/virtio"

type Backend = virtio.FSBackend
type Attr = virtio.FuseAttr
type CachePolicy = virtio.FSCachePolicy
type Mount = virtio.ShareMount
