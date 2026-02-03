package main

/*
#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include <string.h>

// API version
#define CC_API_VERSION_MAJOR 0
#define CC_API_VERSION_MINOR 1
#define CC_API_VERSION_PATCH 0

// Handle types
#define CC_DEFINE_HANDLE(name) typedef struct { uint64_t _h; } name

CC_DEFINE_HANDLE(cc_oci_client);
CC_DEFINE_HANDLE(cc_instance_source);
CC_DEFINE_HANDLE(cc_instance);
CC_DEFINE_HANDLE(cc_file);
CC_DEFINE_HANDLE(cc_cmd);
CC_DEFINE_HANDLE(cc_listener);
CC_DEFINE_HANDLE(cc_conn);
CC_DEFINE_HANDLE(cc_snapshot);
CC_DEFINE_HANDLE(cc_cancel_token);

// Error codes
typedef enum {
    CC_OK = 0,
    CC_ERR_INVALID_HANDLE = 1,
    CC_ERR_INVALID_ARGUMENT = 2,
    CC_ERR_NOT_RUNNING = 3,
    CC_ERR_ALREADY_CLOSED = 4,
    CC_ERR_TIMEOUT = 5,
    CC_ERR_HYPERVISOR_UNAVAILABLE = 6,
    CC_ERR_IO = 7,
    CC_ERR_NETWORK = 8,
    CC_ERR_CANCELLED = 9,
    CC_ERR_UNKNOWN = 99
} cc_error_code;

typedef struct {
    cc_error_code code;
    char* message;
    char* op;
    char* path;
} cc_error;

// Pull policy
typedef enum {
    CC_PULL_IF_NOT_PRESENT = 0,
    CC_PULL_ALWAYS = 1,
    CC_PULL_NEVER = 2
} cc_pull_policy;

// Pull options
typedef struct {
    const char* platform_os;
    const char* platform_arch;
    const char* username;
    const char* password;
    cc_pull_policy policy;
} cc_pull_options;

// Download progress
typedef struct {
    int64_t current;
    int64_t total;
    const char* filename;
    int blob_index;
    int blob_count;
    double bytes_per_second;
    double eta_seconds;
} cc_download_progress;

typedef void (*cc_progress_callback)(const cc_download_progress* progress, void* user_data);

// Helper to invoke the progress callback
static inline void invoke_progress_callback(cc_progress_callback cb, const cc_download_progress* progress, void* user_data) {
    if (cb != NULL) {
        cb(progress, user_data);
    }
}

// Instance options
typedef struct {
    const char* tag;
    const char* host_path;
    bool writable;
} cc_mount_config;

typedef struct {
    uint64_t memory_mb;
    int cpus;
    double timeout_seconds;
    const char* user;
    bool enable_dmesg;
    const cc_mount_config* mounts;
    size_t mount_count;
} cc_instance_options;

// File info
typedef uint32_t cc_file_mode;

typedef struct {
    char* name;
    int64_t size;
    cc_file_mode mode;
    int64_t mod_time_unix;
    bool is_dir;
    bool is_symlink;
} cc_file_info;

// Directory entry
typedef struct {
    char* name;
    bool is_dir;
    cc_file_mode mode;
} cc_dir_entry;

// Seek whence
typedef enum {
    CC_SEEK_SET = 0,
    CC_SEEK_CUR = 1,
    CC_SEEK_END = 2
} cc_seek_whence;

// Image config
typedef struct {
    char* architecture;
    char** env;
    size_t env_count;
    char* working_dir;
    char** entrypoint;
    size_t entrypoint_count;
    char** cmd;
    size_t cmd_count;
    char* user;
} cc_image_config;

// Snapshot options
typedef struct {
    const char* const* excludes;
    size_t exclude_count;
    const char* cache_dir;
} cc_snapshot_options;

// System capabilities
typedef struct {
    bool hypervisor_available;
    uint64_t max_memory_mb;
    int max_cpus;
    const char* architecture;
} cc_capabilities;

// Helpers
static inline void set_error(cc_error* err, cc_error_code code, const char* message, const char* op, const char* path) {
    if (err == NULL) return;
    err->code = code;
    err->message = message ? strdup(message) : NULL;
    err->op = op ? strdup(op) : NULL;
    err->path = path ? strdup(path) : NULL;
}

static inline void clear_error(cc_error* err) {
    if (err == NULL) return;
    err->code = CC_OK;
    err->message = NULL;
    err->op = NULL;
    err->path = NULL;
}

// voidptr cast helper for free
static inline void* voidptr(char* p) { return (void*)p; }
*/
import "C"

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/internal/api"
)

// ==========================================================================
// Library state
// ==========================================================================

var (
	libInitCount  atomic.Int32
	libInitMu     sync.Mutex
	libShutdown   atomic.Bool
	apiVersionStr = "0.1.0"
)

// ==========================================================================
// Phase 1: Core Infrastructure
// ==========================================================================

//export cc_api_version
func cc_api_version() *C.char {
	return C.CString(apiVersionStr)
}

//export cc_api_version_compatible
func cc_api_version_compatible(major, minor C.int) C.bool {
	// Compatible if major matches and requested minor <= our minor
	if int(major) != 0 {
		return C.bool(false)
	}
	return C.bool(int(minor) <= 1)
}

//export cc_init
func cc_init() C.cc_error_code {
	libInitMu.Lock()
	defer libInitMu.Unlock()

	libShutdown.Store(false)
	libInitCount.Add(1)
	return C.CC_OK
}

//export cc_shutdown
func cc_shutdown() {
	libInitMu.Lock()
	defer libInitMu.Unlock()

	if libInitCount.Add(-1) <= 0 {
		libShutdown.Store(true)
		libInitCount.Store(0)
	}
}

//export cc_supports_hypervisor
func cc_supports_hypervisor(cErr *C.cc_error) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}

	err := cc.SupportsHypervisor()
	if err != nil {
		return setError(err, cErr)
	}
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_guest_protocol_version
func cc_guest_protocol_version() C.int {
	return C.int(1)
}

//export cc_query_capabilities
func cc_query_capabilities(out *C.cc_capabilities, cErr *C.cc_error) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	err := cc.SupportsHypervisor()
	out.hypervisor_available = C.bool(err == nil)
	out.max_memory_mb = 0 // Unknown
	out.max_cpus = 0      // Unknown

	// Set architecture based on runtime
	switch runtime.GOARCH {
	case "amd64":
		out.architecture = C.CString("x86_64")
	case "arm64":
		out.architecture = C.CString("arm64")
	default:
		out.architecture = C.CString(runtime.GOARCH)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_error_free
func cc_error_free(err *C.cc_error) {
	if err == nil {
		return
	}
	if err.message != nil {
		C.free(unsafe.Pointer(err.message))
		err.message = nil
	}
	if err.op != nil {
		C.free(unsafe.Pointer(err.op))
		err.op = nil
	}
	if err.path != nil {
		C.free(unsafe.Pointer(err.path))
		err.path = nil
	}
	err.code = C.CC_OK
}

//export cc_free_string
func cc_free_string(str *C.char) {
	if str != nil {
		C.free(unsafe.Pointer(str))
	}
}

//export cc_free_bytes
func cc_free_bytes(buf *C.uint8_t) {
	if buf != nil {
		C.free(unsafe.Pointer(buf))
	}
}

// ==========================================================================
// Cancellation
// ==========================================================================

// cancelToken wraps a context cancel func for the C API.
type cancelToken struct {
	ctx    context.Context
	cancel context.CancelFunc
}

//export cc_cancel_token_new
func cc_cancel_token_new() C.cc_cancel_token {
	ctx, cancel := context.WithCancel(context.Background())
	h := newHandle(&cancelToken{ctx: ctx, cancel: cancel})
	return C.cc_cancel_token{_h: C.uint64_t(h)}
}

//export cc_cancel_token_cancel
func cc_cancel_token_cancel(token C.cc_cancel_token) {
	ct, ok := getHandleTyped[*cancelToken](uint64(token._h))
	if !ok {
		return
	}
	ct.cancel()
}

//export cc_cancel_token_is_cancelled
func cc_cancel_token_is_cancelled(token C.cc_cancel_token) C.bool {
	ct, ok := getHandleTyped[*cancelToken](uint64(token._h))
	if !ok {
		return C.bool(false)
	}
	select {
	case <-ct.ctx.Done():
		return C.bool(true)
	default:
		return C.bool(false)
	}
}

//export cc_cancel_token_free
func cc_cancel_token_free(token C.cc_cancel_token) {
	ct, ok := freeHandleTyped[*cancelToken](uint64(token._h))
	if ok {
		ct.cancel() // Cancel to release resources
	}
}

// getCancelContext returns the context for a cancel token, or background if invalid.
func getCancelContext(token C.cc_cancel_token) context.Context {
	if token._h == 0 {
		return context.Background()
	}
	ct, ok := getHandleTyped[*cancelToken](uint64(token._h))
	if !ok {
		return context.Background()
	}
	return ct.ctx
}

// ==========================================================================
// Phase 2: OCI Client + Instance
// ==========================================================================

//export cc_oci_client_new
func cc_oci_client_new(out *C.cc_oci_client, cErr *C.cc_error) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	client, err := cc.NewOCIClient()
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(client)
	out._h = C.uint64_t(h)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_oci_client_new_with_cache
func cc_oci_client_new_with_cache(cacheDir *C.char, out *C.cc_oci_client, cErr *C.cc_error) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	dir := ""
	if cacheDir != nil {
		dir = C.GoString(cacheDir)
	}

	cache, err := cc.NewCacheDir(dir)
	if err != nil {
		return setError(err, cErr)
	}

	client, err := cc.NewOCIClientWithCache(cache)
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(client)
	out._h = C.uint64_t(h)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_oci_client_free
func cc_oci_client_free(client C.cc_oci_client) {
	freeHandle(uint64(client._h))
}

//export cc_oci_client_pull
func cc_oci_client_pull(
	client C.cc_oci_client,
	imageRef *C.char,
	opts *C.cc_pull_options,
	progressCb C.cc_progress_callback,
	progressUserData unsafe.Pointer,
	cancel C.cc_cancel_token,
	out *C.cc_instance_source,
	cErr *C.cc_error,
) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}

	ociClient, ok := getHandleTyped[cc.OCIClient](uint64(client._h))
	if !ok {
		return setInvalidHandle(cErr, "oci_client")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}
	if imageRef == nil {
		return setInvalidArgument(cErr, "image_ref is NULL")
	}

	ctx := getCancelContext(cancel)
	ref := C.GoString(imageRef)

	// Build pull options
	var pullOpts []cc.OCIPullOption

	if opts != nil {
		// Platform options
		if opts.platform_os != nil && opts.platform_arch != nil {
			pullOpts = append(pullOpts, cc.WithPlatform(
				C.GoString(opts.platform_os),
				C.GoString(opts.platform_arch),
			))
		}

		// Auth options
		if opts.username != nil && opts.password != nil {
			pullOpts = append(pullOpts, cc.WithAuth(
				C.GoString(opts.username),
				C.GoString(opts.password),
			))
		}

		// Pull policy
		var policy cc.PullPolicy
		switch opts.policy {
		case C.CC_PULL_IF_NOT_PRESENT:
			policy = cc.PullIfNotPresent
		case C.CC_PULL_ALWAYS:
			policy = cc.PullAlways
		case C.CC_PULL_NEVER:
			policy = cc.PullNever
		}
		pullOpts = append(pullOpts, cc.WithPullPolicy(policy))
	}

	// Progress callback
	if progressCb != nil {
		pullOpts = append(pullOpts, cc.WithProgressCallback(func(p cc.DownloadProgress) {
			var cProgress C.cc_download_progress
			cProgress.current = C.int64_t(p.Current)
			cProgress.total = C.int64_t(p.Total)
			if p.Filename != "" {
				cProgress.filename = C.CString(p.Filename)
			}
			cProgress.blob_index = C.int(p.BlobIndex)
			cProgress.blob_count = C.int(p.BlobCount)
			cProgress.bytes_per_second = C.double(p.BytesPerSecond)
			cProgress.eta_seconds = C.double(p.ETA.Seconds())

			C.invoke_progress_callback(progressCb, &cProgress, progressUserData)

			if cProgress.filename != nil {
				C.free(unsafe.Pointer(cProgress.filename))
			}
		}))
	}

	source, err := ociClient.Pull(ctx, ref, pullOpts...)
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(source)
	out._h = C.uint64_t(h)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_oci_client_load_tar
func cc_oci_client_load_tar(
	client C.cc_oci_client,
	tarPath *C.char,
	opts *C.cc_pull_options,
	out *C.cc_instance_source,
	cErr *C.cc_error,
) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}

	ociClient, ok := getHandleTyped[cc.OCIClient](uint64(client._h))
	if !ok {
		return setInvalidHandle(cErr, "oci_client")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}
	if tarPath == nil {
		return setInvalidArgument(cErr, "tar_path is NULL")
	}

	path := C.GoString(tarPath)
	source, err := ociClient.LoadFromTar(path)
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(source)
	out._h = C.uint64_t(h)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_oci_client_load_dir
func cc_oci_client_load_dir(
	client C.cc_oci_client,
	dirPath *C.char,
	opts *C.cc_pull_options,
	out *C.cc_instance_source,
	cErr *C.cc_error,
) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}

	ociClient, ok := getHandleTyped[cc.OCIClient](uint64(client._h))
	if !ok {
		return setInvalidHandle(cErr, "oci_client")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}
	if dirPath == nil {
		return setInvalidArgument(cErr, "dir_path is NULL")
	}

	path := C.GoString(dirPath)
	source, err := ociClient.LoadFromDir(path)
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(source)
	out._h = C.uint64_t(h)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_oci_client_export_dir
func cc_oci_client_export_dir(
	client C.cc_oci_client,
	source C.cc_instance_source,
	dirPath *C.char,
	cErr *C.cc_error,
) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}

	ociClient, ok := getHandleTyped[cc.OCIClient](uint64(client._h))
	if !ok {
		return setInvalidHandle(cErr, "oci_client")
	}

	src, ok := getHandleTyped[cc.InstanceSource](uint64(source._h))
	if !ok {
		return setInvalidHandle(cErr, "instance_source")
	}

	if dirPath == nil {
		return setInvalidArgument(cErr, "dir_path is NULL")
	}

	path := C.GoString(dirPath)
	err := ociClient.ExportToDir(src, path)
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_oci_client_cache_dir
func cc_oci_client_cache_dir(client C.cc_oci_client) *C.char {
	ociClient, ok := getHandleTyped[cc.OCIClient](uint64(client._h))
	if !ok {
		return nil
	}
	return C.CString(ociClient.CacheDir())
}

//export cc_instance_source_free
func cc_instance_source_free(source C.cc_instance_source) {
	freeHandle(uint64(source._h))
}

//export cc_source_get_config
func cc_source_get_config(
	source C.cc_instance_source,
	out **C.cc_image_config,
	cErr *C.cc_error,
) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}

	src, ok := getHandleTyped[cc.InstanceSource](uint64(source._h))
	if !ok {
		return setInvalidHandle(cErr, "instance_source")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	cfg := cc.SourceConfig(src)
	if cfg == nil {
		return setInvalidArgument(cErr, "source has no image config")
	}

	// Allocate C struct
	cCfg := (*C.cc_image_config)(C.malloc(C.size_t(unsafe.Sizeof(C.cc_image_config{}))))
	if cCfg == nil {
		return setInvalidArgument(cErr, "out of memory")
	}

	// Architecture
	if cfg.Architecture != "" {
		cCfg.architecture = C.CString(cfg.Architecture)
	}

	// Environment
	if len(cfg.Env) > 0 {
		cCfg.env_count = C.size_t(len(cfg.Env))
		cCfg.env = (**C.char)(C.malloc(C.size_t(len(cfg.Env)+1) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))
		for i, e := range cfg.Env {
			*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(cCfg.env)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil)))) = C.CString(e)
		}
		// NULL terminate
		*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(cCfg.env)) + uintptr(len(cfg.Env))*unsafe.Sizeof((*C.char)(nil)))) = nil
	}

	// Working dir
	if cfg.WorkingDir != "" {
		cCfg.working_dir = C.CString(cfg.WorkingDir)
	}

	// Entrypoint
	if len(cfg.Entrypoint) > 0 {
		cCfg.entrypoint_count = C.size_t(len(cfg.Entrypoint))
		cCfg.entrypoint = (**C.char)(C.malloc(C.size_t(len(cfg.Entrypoint)+1) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))
		for i, e := range cfg.Entrypoint {
			*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(cCfg.entrypoint)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil)))) = C.CString(e)
		}
		*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(cCfg.entrypoint)) + uintptr(len(cfg.Entrypoint))*unsafe.Sizeof((*C.char)(nil)))) = nil
	}

	// Cmd
	if len(cfg.Cmd) > 0 {
		cCfg.cmd_count = C.size_t(len(cfg.Cmd))
		cCfg.cmd = (**C.char)(C.malloc(C.size_t(len(cfg.Cmd)+1) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))
		for i, c := range cfg.Cmd {
			*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(cCfg.cmd)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil)))) = C.CString(c)
		}
		*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(cCfg.cmd)) + uintptr(len(cfg.Cmd))*unsafe.Sizeof((*C.char)(nil)))) = nil
	}

	// User
	if cfg.User != "" {
		cCfg.user = C.CString(cfg.User)
	}

	*out = cCfg
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_image_config_free
func cc_image_config_free(config *C.cc_image_config) {
	if config == nil {
		return
	}

	if config.architecture != nil {
		C.free(unsafe.Pointer(config.architecture))
	}

	// Free env array
	if config.env != nil {
		for i := C.size_t(0); i < config.env_count; i++ {
			ptr := *(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(config.env)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil))))
			if ptr != nil {
				C.free(unsafe.Pointer(ptr))
			}
		}
		C.free(unsafe.Pointer(config.env))
	}

	if config.working_dir != nil {
		C.free(unsafe.Pointer(config.working_dir))
	}

	// Free entrypoint array
	if config.entrypoint != nil {
		for i := C.size_t(0); i < config.entrypoint_count; i++ {
			ptr := *(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(config.entrypoint)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil))))
			if ptr != nil {
				C.free(unsafe.Pointer(ptr))
			}
		}
		C.free(unsafe.Pointer(config.entrypoint))
	}

	// Free cmd array
	if config.cmd != nil {
		for i := C.size_t(0); i < config.cmd_count; i++ {
			ptr := *(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(config.cmd)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil))))
			if ptr != nil {
				C.free(unsafe.Pointer(ptr))
			}
		}
		C.free(unsafe.Pointer(config.cmd))
	}

	if config.user != nil {
		C.free(unsafe.Pointer(config.user))
	}

	C.free(unsafe.Pointer(config))
}

//export cc_instance_new
func cc_instance_new(
	source C.cc_instance_source,
	opts *C.cc_instance_options,
	out *C.cc_instance,
	cErr *C.cc_error,
) C.cc_error_code {
	if libShutdown.Load() {
		return setInvalidHandle(cErr, "library")
	}

	src, ok := getHandleTyped[cc.InstanceSource](uint64(source._h))
	if !ok {
		return setInvalidHandle(cErr, "instance_source")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	var ccOpts []cc.Option

	if opts != nil {
		if opts.memory_mb > 0 {
			ccOpts = append(ccOpts, cc.WithMemoryMB(uint64(opts.memory_mb)))
		}
		if opts.cpus > 0 {
			ccOpts = append(ccOpts, cc.WithCPUs(int(opts.cpus)))
		}
		if opts.timeout_seconds > 0 {
			ccOpts = append(ccOpts, cc.WithTimeout(time.Duration(float64(opts.timeout_seconds)*float64(time.Second))))
		}
		if opts.user != nil {
			ccOpts = append(ccOpts, cc.WithUser(C.GoString(opts.user)))
		}
		if opts.enable_dmesg {
			ccOpts = append(ccOpts, cc.WithDmesg())
		}

		// Handle mounts
		if opts.mounts != nil && opts.mount_count > 0 {
			for i := C.size_t(0); i < opts.mount_count; i++ {
				mountPtr := (*C.cc_mount_config)(unsafe.Pointer(uintptr(unsafe.Pointer(opts.mounts)) + uintptr(i)*unsafe.Sizeof(C.cc_mount_config{})))
				var tag, hostPath string
				if mountPtr.tag != nil {
					tag = C.GoString(mountPtr.tag)
				}
				if mountPtr.host_path != nil {
					hostPath = C.GoString(mountPtr.host_path)
				}
				ccOpts = append(ccOpts, cc.WithMount(cc.MountConfig{
					Tag:      tag,
					HostPath: hostPath,
					Writable: bool(mountPtr.writable),
				}))
			}
		}
	}

	inst, err := cc.New(src, ccOpts...)
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(inst)
	out._h = C.uint64_t(h)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_instance_close
func cc_instance_close(inst C.cc_instance, cErr *C.cc_error) C.cc_error_code {
	instance, ok := freeHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}

	err := instance.Close()
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_instance_wait
func cc_instance_wait(inst C.cc_instance, cancel C.cc_cancel_token, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}

	// TODO: Implement cancellation support by wrapping Wait with a context
	_ = cancel

	err := instance.Wait()
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_instance_id
func cc_instance_id(inst C.cc_instance) *C.char {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return nil
	}
	return C.CString(instance.ID())
}

//export cc_instance_is_running
func cc_instance_is_running(inst C.cc_instance) C.bool {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return C.bool(false)
	}

	// Check if instance is still running by checking the Done channel
	select {
	case <-instance.Done():
		return C.bool(false)
	default:
		return C.bool(true)
	}
}

//export cc_instance_set_console_size
func cc_instance_set_console_size(inst C.cc_instance, cols, rows C.int, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}

	instance.SetConsoleSize(int(cols), int(rows))
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_instance_set_network_enabled
func cc_instance_set_network_enabled(inst C.cc_instance, enabled C.bool, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}

	instance.SetNetworkEnabled(bool(enabled))
	C.clear_error(cErr)
	return C.CC_OK
}

// ==========================================================================
// Phase 3: Filesystem Operations
// ==========================================================================

// instanceFile wraps an api.File for the C API.
type instanceFile struct {
	file cc.File
	path string
}

//export cc_fs_open
func cc_fs_open(inst C.cc_instance, path *C.char, out *C.cc_file, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	goPath := C.GoString(path)
	f, err := instance.Open(goPath)
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(&instanceFile{file: f, path: goPath})
	out._h = C.uint64_t(h)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_create
func cc_fs_create(inst C.cc_instance, path *C.char, out *C.cc_file, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	goPath := C.GoString(path)
	f, err := instance.Create(goPath)
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(&instanceFile{file: f, path: goPath})
	out._h = C.uint64_t(h)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_open_file
func cc_fs_open_file(
	inst C.cc_instance,
	path *C.char,
	flags C.int,
	perm C.cc_file_mode,
	out *C.cc_file,
	cErr *C.cc_error,
) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	goPath := C.GoString(path)
	f, err := instance.OpenFile(goPath, int(flags), fs.FileMode(perm))
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(&instanceFile{file: f, path: goPath})
	out._h = C.uint64_t(h)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_file_close
func cc_file_close(f C.cc_file, cErr *C.cc_error) C.cc_error_code {
	file, ok := freeHandleTyped[*instanceFile](uint64(f._h))
	if !ok {
		return setInvalidHandle(cErr, "file")
	}

	err := file.file.Close()
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_file_read
func cc_file_read(f C.cc_file, buf *C.uint8_t, length C.size_t, n *C.size_t, cErr *C.cc_error) C.cc_error_code {
	file, ok := getHandleTyped[*instanceFile](uint64(f._h))
	if !ok {
		return setInvalidHandle(cErr, "file")
	}
	if buf == nil {
		return setInvalidArgument(cErr, "buf is NULL")
	}
	if n == nil {
		return setInvalidArgument(cErr, "n is NULL")
	}

	goBuf := make([]byte, int(length))
	bytesRead, err := file.file.Read(goBuf)
	if err != nil && !errors.Is(err, api.ErrAlreadyClosed) {
		*n = C.size_t(bytesRead)
		return setError(err, cErr)
	}

	// Copy to C buffer
	if bytesRead > 0 {
		C.memcpy(unsafe.Pointer(buf), unsafe.Pointer(&goBuf[0]), C.size_t(bytesRead))
	}
	*n = C.size_t(bytesRead)

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_file_write
func cc_file_write(f C.cc_file, buf *C.uint8_t, length C.size_t, n *C.size_t, cErr *C.cc_error) C.cc_error_code {
	file, ok := getHandleTyped[*instanceFile](uint64(f._h))
	if !ok {
		return setInvalidHandle(cErr, "file")
	}
	if buf == nil && length > 0 {
		return setInvalidArgument(cErr, "buf is NULL")
	}
	if n == nil {
		return setInvalidArgument(cErr, "n is NULL")
	}

	// Convert C buffer to Go slice
	goBuf := C.GoBytes(unsafe.Pointer(buf), C.int(length))

	bytesWritten, err := file.file.Write(goBuf)
	*n = C.size_t(bytesWritten)

	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_file_seek
func cc_file_seek(f C.cc_file, offset C.int64_t, whence C.cc_seek_whence, newOffset *C.int64_t, cErr *C.cc_error) C.cc_error_code {
	file, ok := getHandleTyped[*instanceFile](uint64(f._h))
	if !ok {
		return setInvalidHandle(cErr, "file")
	}

	newPos, err := file.file.Seek(int64(offset), int(whence))
	if newOffset != nil {
		*newOffset = C.int64_t(newPos)
	}

	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_file_sync
func cc_file_sync(f C.cc_file, cErr *C.cc_error) C.cc_error_code {
	file, ok := getHandleTyped[*instanceFile](uint64(f._h))
	if !ok {
		return setInvalidHandle(cErr, "file")
	}

	err := file.file.Sync()
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_file_truncate
func cc_file_truncate(f C.cc_file, size C.int64_t, cErr *C.cc_error) C.cc_error_code {
	file, ok := getHandleTyped[*instanceFile](uint64(f._h))
	if !ok {
		return setInvalidHandle(cErr, "file")
	}

	err := file.file.Truncate(int64(size))
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_file_stat
func cc_file_stat(f C.cc_file, out *C.cc_file_info, cErr *C.cc_error) C.cc_error_code {
	file, ok := getHandleTyped[*instanceFile](uint64(f._h))
	if !ok {
		return setInvalidHandle(cErr, "file")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	info, err := file.file.Stat()
	if err != nil {
		return setError(err, cErr)
	}

	out.name = C.CString(info.Name())
	out.size = C.int64_t(info.Size())
	out.mode = C.cc_file_mode(info.Mode())
	out.mod_time_unix = C.int64_t(info.ModTime().Unix())
	out.is_dir = C.bool(info.IsDir())
	out.is_symlink = C.bool(info.Mode()&fs.ModeSymlink != 0)

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_file_name
func cc_file_name(f C.cc_file) *C.char {
	file, ok := getHandleTyped[*instanceFile](uint64(f._h))
	if !ok {
		return nil
	}
	return C.CString(file.file.Name())
}

//export cc_fs_read_file
func cc_fs_read_file(inst C.cc_instance, path *C.char, out **C.uint8_t, length *C.size_t, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}
	if length == nil {
		return setInvalidArgument(cErr, "length is NULL")
	}

	goPath := C.GoString(path)
	data, err := instance.ReadFile(goPath)
	if err != nil {
		return setError(err, cErr)
	}

	if len(data) > 0 {
		cData := C.malloc(C.size_t(len(data)))
		C.memcpy(cData, unsafe.Pointer(&data[0]), C.size_t(len(data)))
		*out = (*C.uint8_t)(cData)
	} else {
		*out = nil
	}
	*length = C.size_t(len(data))

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_write_file
func cc_fs_write_file(inst C.cc_instance, path *C.char, data *C.uint8_t, length C.size_t, perm C.cc_file_mode, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	goPath := C.GoString(path)
	goData := C.GoBytes(unsafe.Pointer(data), C.int(length))

	err := instance.WriteFile(goPath, goData, fs.FileMode(perm))
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_stat
func cc_fs_stat(inst C.cc_instance, path *C.char, out *C.cc_file_info, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	goPath := C.GoString(path)
	info, err := instance.Stat(goPath)
	if err != nil {
		return setError(err, cErr)
	}

	out.name = C.CString(info.Name())
	out.size = C.int64_t(info.Size())
	out.mode = C.cc_file_mode(info.Mode())
	out.mod_time_unix = C.int64_t(info.ModTime().Unix())
	out.is_dir = C.bool(info.IsDir())
	out.is_symlink = C.bool(info.Mode()&fs.ModeSymlink != 0)

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_lstat
func cc_fs_lstat(inst C.cc_instance, path *C.char, out *C.cc_file_info, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	goPath := C.GoString(path)
	info, err := instance.Lstat(goPath)
	if err != nil {
		return setError(err, cErr)
	}

	out.name = C.CString(info.Name())
	out.size = C.int64_t(info.Size())
	out.mode = C.cc_file_mode(info.Mode())
	out.mod_time_unix = C.int64_t(info.ModTime().Unix())
	out.is_dir = C.bool(info.IsDir())
	out.is_symlink = C.bool(info.Mode()&fs.ModeSymlink != 0)

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_file_info_free
func cc_file_info_free(info *C.cc_file_info) {
	if info == nil {
		return
	}
	if info.name != nil {
		C.free(unsafe.Pointer(info.name))
		info.name = nil
	}
}

//export cc_fs_remove
func cc_fs_remove(inst C.cc_instance, path *C.char, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	goPath := C.GoString(path)
	err := instance.Remove(goPath)
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_remove_all
func cc_fs_remove_all(inst C.cc_instance, path *C.char, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	goPath := C.GoString(path)
	err := instance.RemoveAll(goPath)
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_mkdir
func cc_fs_mkdir(inst C.cc_instance, path *C.char, perm C.cc_file_mode, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	goPath := C.GoString(path)
	err := instance.Mkdir(goPath, fs.FileMode(perm))
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_mkdir_all
func cc_fs_mkdir_all(inst C.cc_instance, path *C.char, perm C.cc_file_mode, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	goPath := C.GoString(path)
	err := instance.MkdirAll(goPath, fs.FileMode(perm))
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_rename
func cc_fs_rename(inst C.cc_instance, oldpath, newpath *C.char, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if oldpath == nil || newpath == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	err := instance.Rename(C.GoString(oldpath), C.GoString(newpath))
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_symlink
func cc_fs_symlink(inst C.cc_instance, oldname, newname *C.char, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if oldname == nil || newname == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	err := instance.Symlink(C.GoString(oldname), C.GoString(newname))
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_readlink
func cc_fs_readlink(inst C.cc_instance, path *C.char, out **C.char, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	goPath := C.GoString(path)
	target, err := instance.Readlink(goPath)
	if err != nil {
		return setError(err, cErr)
	}

	*out = C.CString(target)
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_read_dir
func cc_fs_read_dir(inst C.cc_instance, path *C.char, out **C.cc_dir_entry, count *C.size_t, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}
	if out == nil || count == nil {
		return setInvalidArgument(cErr, "out or count is NULL")
	}

	goPath := C.GoString(path)
	entries, err := instance.ReadDir(goPath)
	if err != nil {
		return setError(err, cErr)
	}

	if len(entries) == 0 {
		*out = nil
		*count = 0
		C.clear_error(cErr)
		return C.CC_OK
	}

	// Allocate C array
	cEntries := (*C.cc_dir_entry)(C.malloc(C.size_t(len(entries)) * C.size_t(unsafe.Sizeof(C.cc_dir_entry{}))))

	for i, e := range entries {
		entryPtr := (*C.cc_dir_entry)(unsafe.Pointer(uintptr(unsafe.Pointer(cEntries)) + uintptr(i)*unsafe.Sizeof(C.cc_dir_entry{})))
		entryPtr.name = C.CString(e.Name())
		entryPtr.is_dir = C.bool(e.IsDir())
		info, _ := e.Info()
		if info != nil {
			entryPtr.mode = C.cc_file_mode(info.Mode())
		}
	}

	*out = cEntries
	*count = C.size_t(len(entries))
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_dir_entries_free
func cc_dir_entries_free(entries *C.cc_dir_entry, count C.size_t) {
	if entries == nil {
		return
	}

	for i := C.size_t(0); i < count; i++ {
		entryPtr := (*C.cc_dir_entry)(unsafe.Pointer(uintptr(unsafe.Pointer(entries)) + uintptr(i)*unsafe.Sizeof(C.cc_dir_entry{})))
		if entryPtr.name != nil {
			C.free(unsafe.Pointer(entryPtr.name))
		}
	}

	C.free(unsafe.Pointer(entries))
}

//export cc_fs_chmod
func cc_fs_chmod(inst C.cc_instance, path *C.char, mode C.cc_file_mode, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	goPath := C.GoString(path)
	err := instance.Chmod(goPath, fs.FileMode(mode))
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_chown
func cc_fs_chown(inst C.cc_instance, path *C.char, uid, gid C.int, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	goPath := C.GoString(path)
	err := instance.Chown(goPath, int(uid), int(gid))
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_fs_chtimes
func cc_fs_chtimes(inst C.cc_instance, path *C.char, atimeUnix, mtimeUnix C.int64_t, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if path == nil {
		return setInvalidArgument(cErr, "path is NULL")
	}

	goPath := C.GoString(path)
	atime := time.Unix(int64(atimeUnix), 0)
	mtime := time.Unix(int64(mtimeUnix), 0)

	err := instance.Chtimes(goPath, atime, mtime)
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

// ==========================================================================
// Phase 4: Command Execution
// ==========================================================================

//export cc_cmd_new
func cc_cmd_new(inst C.cc_instance, name *C.char, args **C.char, out *C.cc_cmd, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if name == nil {
		return setInvalidArgument(cErr, "name is NULL")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	cmdName := C.GoString(name)

	// Convert args array (NULL-terminated)
	var goArgs []string
	if args != nil {
		for i := 0; ; i++ {
			argPtr := *(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(args)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil))))
			if argPtr == nil {
				break
			}
			goArgs = append(goArgs, C.GoString(argPtr))
		}
	}

	cmd := instance.Command(cmdName, goArgs...)
	h := newHandle(cmd)
	out._h = C.uint64_t(h)

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_entrypoint
func cc_cmd_entrypoint(inst C.cc_instance, args **C.char, out *C.cc_cmd, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	// Convert args array (NULL-terminated)
	var goArgs []string
	if args != nil {
		for i := 0; ; i++ {
			argPtr := *(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(args)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil))))
			if argPtr == nil {
				break
			}
			goArgs = append(goArgs, C.GoString(argPtr))
		}
	}

	cmd := instance.EntrypointCommand(goArgs...)
	h := newHandle(cmd)
	out._h = C.uint64_t(h)

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_free
func cc_cmd_free(cmd C.cc_cmd) {
	freeHandle(uint64(cmd._h))
}

//export cc_cmd_set_dir
func cc_cmd_set_dir(cmd C.cc_cmd, dir *C.char, cErr *C.cc_error) C.cc_error_code {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return setInvalidHandle(cErr, "cmd")
	}
	if dir == nil {
		return setInvalidArgument(cErr, "dir is NULL")
	}

	c.SetDir(C.GoString(dir))
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_set_env
func cc_cmd_set_env(cmd C.cc_cmd, key, value *C.char, cErr *C.cc_error) C.cc_error_code {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return setInvalidHandle(cErr, "cmd")
	}
	if key == nil || value == nil {
		return setInvalidArgument(cErr, "key or value is NULL")
	}

	c.SetEnv(C.GoString(key), C.GoString(value))
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_get_env
func cc_cmd_get_env(cmd C.cc_cmd, key *C.char) *C.char {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return nil
	}
	if key == nil {
		return nil
	}

	val := c.GetEnv(C.GoString(key))
	if val == "" {
		return nil
	}
	return C.CString(val)
}

//export cc_cmd_environ
func cc_cmd_environ(cmd C.cc_cmd, out ***C.char, count *C.size_t, cErr *C.cc_error) C.cc_error_code {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return setInvalidHandle(cErr, "cmd")
	}
	if out == nil || count == nil {
		return setInvalidArgument(cErr, "out or count is NULL")
	}

	environ := c.Environ()

	if len(environ) == 0 {
		*out = nil
		*count = 0
		C.clear_error(cErr)
		return C.CC_OK
	}

	// Allocate array
	cEnv := (***C.char)(C.malloc(C.size_t(len(environ)+1) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))

	for i, e := range environ {
		*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(cEnv)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil)))) = C.CString(e)
	}
	// NULL terminate
	*(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(cEnv)) + uintptr(len(environ))*unsafe.Sizeof((*C.char)(nil)))) = nil

	*out = (**C.char)(unsafe.Pointer(cEnv))
	*count = C.size_t(len(environ))

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_start
func cc_cmd_start(cmd C.cc_cmd, cErr *C.cc_error) C.cc_error_code {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return setInvalidHandle(cErr, "cmd")
	}

	err := c.Start()
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_wait
func cc_cmd_wait(cmd C.cc_cmd, exitCode *C.int, cErr *C.cc_error) C.cc_error_code {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return setInvalidHandle(cErr, "cmd")
	}

	err := c.Wait()
	if exitCode != nil {
		*exitCode = C.int(c.ExitCode())
	}

	// Non-zero exit code is not an error for cc_cmd_wait
	if err != nil {
		// Check if it's just a non-zero exit code error
		var apiErr *api.Error
		if errors.As(err, &apiErr) && c.ExitCode() != 0 {
			// This is expected - return success
			C.clear_error(cErr)
			return C.CC_OK
		}
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_run
func cc_cmd_run(cmd C.cc_cmd, exitCode *C.int, cErr *C.cc_error) C.cc_error_code {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return setInvalidHandle(cErr, "cmd")
	}

	err := c.Run()
	if exitCode != nil {
		*exitCode = C.int(c.ExitCode())
	}

	// Non-zero exit code is not an error for cc_cmd_run
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && c.ExitCode() != 0 {
			C.clear_error(cErr)
			return C.CC_OK
		}
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_output
func cc_cmd_output(cmd C.cc_cmd, out **C.uint8_t, length *C.size_t, exitCode *C.int, cErr *C.cc_error) C.cc_error_code {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return setInvalidHandle(cErr, "cmd")
	}
	if out == nil || length == nil {
		return setInvalidArgument(cErr, "out or length is NULL")
	}

	output, err := c.Output()
	if exitCode != nil {
		*exitCode = C.int(c.ExitCode())
	}

	// Copy output to C buffer
	if len(output) > 0 {
		cData := C.malloc(C.size_t(len(output)))
		C.memcpy(cData, unsafe.Pointer(&output[0]), C.size_t(len(output)))
		*out = (*C.uint8_t)(cData)
	} else {
		*out = nil
	}
	*length = C.size_t(len(output))

	// Non-zero exit code is not an error
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && c.ExitCode() != 0 {
			C.clear_error(cErr)
			return C.CC_OK
		}
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_combined_output
func cc_cmd_combined_output(cmd C.cc_cmd, out **C.uint8_t, length *C.size_t, exitCode *C.int, cErr *C.cc_error) C.cc_error_code {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return setInvalidHandle(cErr, "cmd")
	}
	if out == nil || length == nil {
		return setInvalidArgument(cErr, "out or length is NULL")
	}

	output, err := c.CombinedOutput()
	if exitCode != nil {
		*exitCode = C.int(c.ExitCode())
	}

	// Copy output to C buffer
	if len(output) > 0 {
		cData := C.malloc(C.size_t(len(output)))
		C.memcpy(cData, unsafe.Pointer(&output[0]), C.size_t(len(output)))
		*out = (*C.uint8_t)(cData)
	} else {
		*out = nil
	}
	*length = C.size_t(len(output))

	// Non-zero exit code is not an error
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && c.ExitCode() != 0 {
			C.clear_error(cErr)
			return C.CC_OK
		}
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_cmd_exit_code
func cc_cmd_exit_code(cmd C.cc_cmd) C.int {
	c, ok := getHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return C.int(-1)
	}
	return C.int(c.ExitCode())
}

//export cc_cmd_kill
func cc_cmd_kill(cmd C.cc_cmd, cErr *C.cc_error) C.cc_error_code {
	_, ok := freeHandleTyped[cc.Cmd](uint64(cmd._h))
	if !ok {
		return setInvalidHandle(cErr, "cmd")
	}
	// TODO: Actually kill the running command if possible
	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_instance_exec
func cc_instance_exec(inst C.cc_instance, name *C.char, args **C.char, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if name == nil {
		return setInvalidArgument(cErr, "name is NULL")
	}

	cmdName := C.GoString(name)

	// Convert args array (NULL-terminated)
	var goArgs []string
	if args != nil {
		for i := 0; ; i++ {
			argPtr := *(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(args)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil))))
			if argPtr == nil {
				break
			}
			goArgs = append(goArgs, C.GoString(argPtr))
		}
	}

	err := instance.Exec(cmdName, goArgs...)
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

// ==========================================================================
// Phase 5: Networking
// ==========================================================================

//export cc_net_listen
func cc_net_listen(inst C.cc_instance, network, address *C.char, out *C.cc_listener, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if network == nil || address == nil {
		return setInvalidArgument(cErr, "network or address is NULL")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	ln, err := instance.Listen(C.GoString(network), C.GoString(address))
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(ln)
	out._h = C.uint64_t(h)

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_listener_accept
func cc_listener_accept(ln C.cc_listener, out *C.cc_conn, cErr *C.cc_error) C.cc_error_code {
	listener, ok := getHandleTyped[net.Listener](uint64(ln._h))
	if !ok {
		return setInvalidHandle(cErr, "listener")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	conn, err := listener.Accept()
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(conn)
	out._h = C.uint64_t(h)

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_listener_close
func cc_listener_close(ln C.cc_listener, cErr *C.cc_error) C.cc_error_code {
	listener, ok := freeHandleTyped[net.Listener](uint64(ln._h))
	if !ok {
		return setInvalidHandle(cErr, "listener")
	}

	err := listener.Close()
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_listener_addr
func cc_listener_addr(ln C.cc_listener) *C.char {
	listener, ok := getHandleTyped[net.Listener](uint64(ln._h))
	if !ok {
		return nil
	}
	return C.CString(listener.Addr().String())
}

//export cc_conn_read
func cc_conn_read(c C.cc_conn, buf *C.uint8_t, length C.size_t, n *C.size_t, cErr *C.cc_error) C.cc_error_code {
	conn, ok := getHandleTyped[net.Conn](uint64(c._h))
	if !ok {
		return setInvalidHandle(cErr, "conn")
	}
	if buf == nil {
		return setInvalidArgument(cErr, "buf is NULL")
	}
	if n == nil {
		return setInvalidArgument(cErr, "n is NULL")
	}

	goBuf := make([]byte, int(length))
	bytesRead, err := conn.Read(goBuf)
	if bytesRead > 0 {
		C.memcpy(unsafe.Pointer(buf), unsafe.Pointer(&goBuf[0]), C.size_t(bytesRead))
	}
	*n = C.size_t(bytesRead)

	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_conn_write
func cc_conn_write(c C.cc_conn, buf *C.uint8_t, length C.size_t, n *C.size_t, cErr *C.cc_error) C.cc_error_code {
	conn, ok := getHandleTyped[net.Conn](uint64(c._h))
	if !ok {
		return setInvalidHandle(cErr, "conn")
	}
	if buf == nil && length > 0 {
		return setInvalidArgument(cErr, "buf is NULL")
	}
	if n == nil {
		return setInvalidArgument(cErr, "n is NULL")
	}

	goBuf := C.GoBytes(unsafe.Pointer(buf), C.int(length))
	bytesWritten, err := conn.Write(goBuf)
	*n = C.size_t(bytesWritten)

	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_conn_close
func cc_conn_close(c C.cc_conn, cErr *C.cc_error) C.cc_error_code {
	conn, ok := freeHandleTyped[net.Conn](uint64(c._h))
	if !ok {
		return setInvalidHandle(cErr, "conn")
	}

	err := conn.Close()
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_conn_local_addr
func cc_conn_local_addr(c C.cc_conn) *C.char {
	conn, ok := getHandleTyped[net.Conn](uint64(c._h))
	if !ok {
		return nil
	}
	return C.CString(conn.LocalAddr().String())
}

//export cc_conn_remote_addr
func cc_conn_remote_addr(c C.cc_conn) *C.char {
	conn, ok := getHandleTyped[net.Conn](uint64(c._h))
	if !ok {
		return nil
	}
	return C.CString(conn.RemoteAddr().String())
}

// ==========================================================================
// Phase 6: Snapshots
// ==========================================================================

//export cc_fs_snapshot
func cc_fs_snapshot(inst C.cc_instance, opts *C.cc_snapshot_options, out *C.cc_snapshot, cErr *C.cc_error) C.cc_error_code {
	instance, ok := getHandleTyped[cc.Instance](uint64(inst._h))
	if !ok {
		return setInvalidHandle(cErr, "instance")
	}
	if out == nil {
		return setInvalidArgument(cErr, "out is NULL")
	}

	var snapshotOpts []cc.FilesystemSnapshotOption

	if opts != nil {
		// Handle excludes
		if opts.excludes != nil && opts.exclude_count > 0 {
			var excludes []string
			for i := C.size_t(0); i < opts.exclude_count; i++ {
				ptr := *(**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(opts.excludes)) + uintptr(i)*unsafe.Sizeof((*C.char)(nil))))
				if ptr != nil {
					excludes = append(excludes, C.GoString(ptr))
				}
			}
			if len(excludes) > 0 {
				snapshotOpts = append(snapshotOpts, cc.WithSnapshotExcludes(excludes...))
			}
		}

		// Handle cache dir
		if opts.cache_dir != nil {
			snapshotOpts = append(snapshotOpts, cc.WithSnapshotCacheDir(C.GoString(opts.cache_dir)))
		}
	}

	snap, err := instance.SnapshotFilesystem(snapshotOpts...)
	if err != nil {
		return setError(err, cErr)
	}

	h := newHandle(snap)
	out._h = C.uint64_t(h)

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_snapshot_cache_key
func cc_snapshot_cache_key(snap C.cc_snapshot) *C.char {
	snapshot, ok := getHandleTyped[cc.FilesystemSnapshot](uint64(snap._h))
	if !ok {
		return nil
	}
	return C.CString(snapshot.CacheKey())
}

//export cc_snapshot_parent
func cc_snapshot_parent(snap C.cc_snapshot) C.cc_snapshot {
	snapshot, ok := getHandleTyped[cc.FilesystemSnapshot](uint64(snap._h))
	if !ok {
		return C.cc_snapshot{_h: 0}
	}

	parent := snapshot.Parent()
	if parent == nil {
		return C.cc_snapshot{_h: 0}
	}

	h := newHandle(parent)
	return C.cc_snapshot{_h: C.uint64_t(h)}
}

//export cc_snapshot_close
func cc_snapshot_close(snap C.cc_snapshot, cErr *C.cc_error) C.cc_error_code {
	snapshot, ok := freeHandleTyped[cc.FilesystemSnapshot](uint64(snap._h))
	if !ok {
		return setInvalidHandle(cErr, "snapshot")
	}

	err := snapshot.Close()
	if err != nil {
		return setError(err, cErr)
	}

	C.clear_error(cErr)
	return C.CC_OK
}

//export cc_snapshot_as_source
func cc_snapshot_as_source(snap C.cc_snapshot) C.cc_instance_source {
	snapshot, ok := getHandleTyped[cc.FilesystemSnapshot](uint64(snap._h))
	if !ok {
		return C.cc_instance_source{_h: 0}
	}

	// FilesystemSnapshot implements InstanceSource
	h := newHandle(cc.InstanceSource(snapshot))
	return C.cc_instance_source{_h: C.uint64_t(h)}
}

// Required for CGO shared library
func main() {}

