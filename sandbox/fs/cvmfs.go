package fs

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"j5.nz/cc/internal/cvmfs"
	"j5.nz/cc/internal/linuxabi"
	"j5.nz/cc/internal/virtio"
)

// CVMFSConfig is trusted configuration. Fetching uses the host network, limited
// to these HTTPS mirror/repository prefixes, independently of guest networking.
// CacheBytes bounds retained and in-flight compressed response payloads in RAM;
// zero disables caching. No host mount, FUSE process or disk cache is required.
type CVMFSConfig struct {
	Repository     string
	Mirrors        []string
	CacheBytes     int64
	MaxObjectBytes int64
	HTTPClient     *http.Client
}

type CVMFSStats struct {
	CacheBytes, CacheLimitBytes, FetchedBytes int64
	Requests, ActiveRequests                  int64
	LastError                                 string
}

// CVMFS exposes only the read-only backend methods, plus lifecycle and stats.
// Optional mutation interfaces of the underlying image backend are not exposed.
type CVMFS struct {
	Backend
	operations sync.Mutex // The client's lazy catalog maps are not concurrent.
	repository string
	cancel     context.CancelFunc
	transport  *cvmfsTransport
}

var cvmfsRepository = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}[a-zA-Z0-9]$`)

func ValidateCVMFS(c CVMFSConfig) error {
	if !cvmfsRepository.MatchString(c.Repository) || strings.Contains(c.Repository, "..") {
		return errors.New("invalid CVMFS repository")
	}
	if len(c.Mirrors) < 1 || len(c.Mirrors) > 8 || c.CacheBytes < 0 || c.CacheBytes > 1<<30 || c.MaxObjectBytes < 1 || c.MaxObjectBytes > 4<<30 {
		return errors.New("invalid CVMFS mirrors, cache or object limits")
	}
	for _, mirror := range c.Mirrors {
		if len(mirror) > 4096 {
			return errors.New("CVMFS mirror URL exceeds 4096 bytes")
		}
		u, err := url.Parse(mirror)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || path.Clean(u.Path) != u.Path || path.Base(u.Path) != "cvmfs" {
			return errors.New("CVMFS mirrors must be clean HTTPS URLs ending in /cvmfs, without credentials, query or fragment")
		}
	}
	return nil
}

// NewCVMFS performs no network IO. The first guest lookup loads the repository.
// The caller owns the returned backend and must close it after guest teardown.
func NewCVMFS(ctx context.Context, c CVMFSConfig) (*CVMFS, error) {
	if err := ValidateCVMFS(c); err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(ctx)
	client := http.Client{Timeout: 2 * time.Minute}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	client.Transport = base
	if c.HTTPClient != nil {
		client = *c.HTTPClient
		if client.Transport == nil {
			client.Transport = base
		}
		if client.Timeout == 0 || client.Timeout > 2*time.Minute {
			client.Timeout = 2 * time.Minute
		}
	}
	transport := &cvmfsTransport{base: client.Transport, limit: c.CacheBytes, maxObject: c.MaxObjectBytes, cache: map[string]*list.Element{}, lru: list.New()}
	for _, mirror := range c.Mirrors {
		transport.prefixes = append(transport.prefixes, mirror+"/"+c.Repository+"/")
	}
	client.Transport = transport
	// CheckRedirect alone is insufficient: every request, including redirects,
	// passes through the prefix check in RoundTrip.
	client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("CVMFS redirect limit exceeded")
		}
		return nil
	}
	cc := cvmfs.NewClient()
	cc.Context, cc.HTTPClient = life, &client
	cc.MaxExpandedObjectBytes = c.MaxObjectBytes
	cc.Mirrors = append([]string(nil), c.Mirrors...)
	target, err := cvmfs.FormatTarget(cvmfs.Target{Remote: true, Mirror: c.Mirrors[0], Repo: c.Repository, Path: "/"})
	if err != nil {
		cancel()
		return nil, err
	}
	root, err := cvmfs.NewImageFS(cc, target)
	if err != nil {
		cancel()
		return nil, err
	}
	return &CVMFS{Backend: virtio.NewImageFS(root, ""), repository: c.Repository, cancel: cancel, transport: transport}, nil
}

func (c *CVMFS) Mount() Mount {
	return Mount{GuestPath: "/cvmfs/" + c.repository, Backend: c, Writable: false, CacheMode: "auto"}
}

func (c *CVMFS) Lookup(parent uint64, name string) (uint64, Attr, int32) {
	c.operations.Lock()
	defer c.operations.Unlock()
	return c.Backend.Lookup(parent, name)
}
func (c *CVMFS) Read(node, fh, offset uint64, size uint32) ([]byte, int32) {
	c.operations.Lock()
	defer c.operations.Unlock()
	return c.Backend.Read(node, fh, offset, size)
}
func (c *CVMFS) ReadDir(node, fh, offset uint64, maxBytes uint32) ([]byte, int32) {
	c.operations.Lock()
	defer c.operations.Unlock()
	return c.Backend.ReadDir(node, fh, offset, maxBytes)
}
func (c *CVMFS) Open(node uint64, flags uint32) (uint64, int32) {
	// Guest Linux O_CREAT, O_TRUNC and O_APPEND, independent of the host ABI.
	if flags&3 != 0 || flags&(0x40|0x200|0x400) != 0 {
		return 0, -linuxabi.EROFS
	}
	return c.Backend.Open(node, flags)
}
func (c *CVMFS) BeginClose() { c.cancel() }
func (c *CVMFS) Close() error {
	c.BeginClose()
	c.transport.mu.Lock()
	c.transport.closed = true
	c.transport.cache = map[string]*list.Element{}
	c.transport.lru.Init()
	c.transport.bytes = 0
	c.transport.mu.Unlock()
	return nil
}
func (c *CVMFS) Stats() CVMFSStats {
	t := c.transport
	t.mu.Lock()
	defer t.mu.Unlock()
	return CVMFSStats{CacheBytes: t.bytes + t.buffered, CacheLimitBytes: t.limit, FetchedBytes: t.fetched, Requests: t.requests, ActiveRequests: t.active, LastError: t.lastError}
}

type cvmfsCacheEntry struct {
	key  string
	data []byte
}
type cvmfsTransport struct {
	base              http.RoundTripper
	prefixes          []string
	limit, maxObject  int64
	mu                sync.Mutex
	cache             map[string]*list.Element
	lru               *list.List
	bytes, buffered   int64
	fetched, requests int64
	active            int64
	lastError         string
	closed            bool
}

func (t *cvmfsTransport) fail(err error) {
	t.mu.Lock()
	t.lastError = err.Error()
	if len(t.lastError) > 1024 {
		t.lastError = t.lastError[:1024]
	}
	t.mu.Unlock()
}

func (t *cvmfsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	allowed := false
	if r.Method == http.MethodGet && r.URL.User == nil && r.URL.RawQuery == "" && r.URL.Fragment == "" && r.URL.RawPath == "" && path.Clean(r.URL.Path) == r.URL.Path {
		for _, prefix := range t.prefixes {
			allowed = allowed || strings.HasPrefix(r.URL.String(), prefix)
		}
	}
	if !allowed {
		err := errors.New("CVMFS request outside granted mirror/repository")
		t.fail(err)
		return nil, err
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, errors.New("CVMFS backend closed")
	}
	key := r.URL.String()
	if e := t.cache[key]; e != nil {
		t.lru.MoveToFront(e)
		data := e.Value.(cvmfsCacheEntry).data
		t.mu.Unlock()
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Request: r}, nil
	}
	t.requests++
	t.active++
	t.mu.Unlock()
	response, err := t.base.RoundTrip(r)
	if err != nil {
		t.mu.Lock()
		t.active--
		t.mu.Unlock()
		t.fail(err)
		return nil, err
	}
	response.Body = &cvmfsBody{ReadCloser: response.Body, t: t, key: key, cache: response.StatusCode == 200 && t.limit > 0, remaining: t.maxObject}
	if response.StatusCode != 200 {
		t.fail(errors.New("CVMFS HTTP " + response.Status))
	}
	return response, nil
}

type cvmfsBody struct {
	io.ReadCloser
	t         *cvmfsTransport
	key       string
	data      []byte
	cache     bool
	remaining int64
	eof       bool
	closed    bool
	once      sync.Once
}

func (b *cvmfsBody) Read(p []byte) (int, error) {
	if b.remaining < 0 {
		return 0, errors.New("CVMFS compressed response exceeds object limit")
	}
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		err = errors.New("CVMFS compressed response exceeds object limit")
	}
	b.t.mu.Lock()
	b.t.fetched += int64(n)
	if b.cache && !b.closed {
		for b.t.bytes+b.t.buffered+int64(n) > b.t.limit && b.t.lru.Len() > 0 {
			e := b.t.lru.Back()
			entry := e.Value.(cvmfsCacheEntry)
			delete(b.t.cache, entry.key)
			b.t.bytes -= int64(len(entry.data))
			b.t.lru.Remove(e)
		}
		if b.t.closed || b.t.bytes+b.t.buffered+int64(n) > b.t.limit || (err != nil && err != io.EOF) {
			b.t.buffered -= int64(len(b.data))
			b.data, b.cache = nil, false
		} else {
			b.data = append(b.data, p[:n]...)
			b.t.buffered += int64(n)
		}
	}
	b.eof = err == io.EOF
	b.t.mu.Unlock()
	if err != nil && err != io.EOF {
		b.t.fail(err)
	}
	return n, err
}
func (b *cvmfsBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() {
		b.t.mu.Lock()
		defer b.t.mu.Unlock()
		b.t.active--
		b.closed = true
		b.t.buffered -= int64(len(b.data))
		if b.cache && b.eof && !b.t.closed {
			if e := b.t.cache[b.key]; e != nil {
				b.t.bytes -= int64(len(e.Value.(cvmfsCacheEntry).data))
				b.t.lru.Remove(e)
			}
			for b.t.lru.Len() >= 4096 {
				e := b.t.lru.Back()
				entry := e.Value.(cvmfsCacheEntry)
				delete(b.t.cache, entry.key)
				b.t.bytes -= int64(len(entry.data))
				b.t.lru.Remove(e)
			}
			b.t.cache[b.key] = b.t.lru.PushFront(cvmfsCacheEntry{key: b.key, data: b.data})
			b.t.bytes += int64(len(b.data))
		}
		b.data = nil
	})
	return err
}
