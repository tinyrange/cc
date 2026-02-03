package main

/*
#include <stdlib.h>
#include <string.h>

// Error codes (must match libcc.h)
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

// Helper to set error fields
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
*/
import "C"

import (
	"context"
	"errors"
	"unsafe"

	"github.com/tinyrange/cc/bindings/c/ipc"
	"github.com/tinyrange/cc/internal/api"
)

// errorCode maps a Go error to a C error code.
func errorCode(err error) C.cc_error_code {
	if err == nil {
		return C.CC_OK
	}

	// Check for IPC errors first (they carry their own error code)
	var ipcErr *ipc.IPCError
	if errors.As(err, &ipcErr) {
		switch ipcErr.Code {
		case ipc.ErrCodeOK:
			return C.CC_OK
		case ipc.ErrCodeInvalidHandle:
			return C.CC_ERR_INVALID_HANDLE
		case ipc.ErrCodeInvalidArgument:
			return C.CC_ERR_INVALID_ARGUMENT
		case ipc.ErrCodeNotRunning:
			return C.CC_ERR_NOT_RUNNING
		case ipc.ErrCodeAlreadyClosed:
			return C.CC_ERR_ALREADY_CLOSED
		case ipc.ErrCodeTimeout:
			return C.CC_ERR_TIMEOUT
		case ipc.ErrCodeHypervisorUnavailable:
			return C.CC_ERR_HYPERVISOR_UNAVAILABLE
		case ipc.ErrCodeIO:
			return C.CC_ERR_IO
		case ipc.ErrCodeNetwork:
			return C.CC_ERR_NETWORK
		case ipc.ErrCodeCancelled:
			return C.CC_ERR_CANCELLED
		default:
			return C.CC_ERR_UNKNOWN
		}
	}

	// Check for sentinel errors
	if errors.Is(err, api.ErrHypervisorUnavailable) {
		return C.CC_ERR_HYPERVISOR_UNAVAILABLE
	}
	if errors.Is(err, api.ErrNotRunning) {
		return C.CC_ERR_NOT_RUNNING
	}
	if errors.Is(err, api.ErrAlreadyClosed) {
		return C.CC_ERR_ALREADY_CLOSED
	}
	if errors.Is(err, api.ErrTimeout) {
		return C.CC_ERR_TIMEOUT
	}
	if errors.Is(err, context.Canceled) {
		return C.CC_ERR_CANCELLED
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return C.CC_ERR_TIMEOUT
	}

	// Check for api.Error which has Op and Path
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		// Filesystem/IO errors use CC_ERR_IO
		return C.CC_ERR_IO
	}

	return C.CC_ERR_UNKNOWN
}

// setError populates a cc_error struct from a Go error.
func setError(err error, cErr *C.cc_error) C.cc_error_code {
	if err == nil {
		C.clear_error(cErr)
		return C.CC_OK
	}

	code := errorCode(err)

	// Extract op and path if available
	var op, path string
	var ipcErr *ipc.IPCError
	if errors.As(err, &ipcErr) {
		op = ipcErr.Op
		path = ipcErr.Path
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		op = apiErr.Op
		path = apiErr.Path
	}

	var cOp, cPath *C.char
	if op != "" {
		cOp = C.CString(op)
	}
	if path != "" {
		cPath = C.CString(path)
	}

	C.set_error(cErr, code, C.CString(err.Error()), cOp, cPath)

	// Free the temporary C strings we just created (set_error duplicates them)
	if cOp != nil {
		C.free(unsafe.Pointer(cOp))
	}
	if cPath != nil {
		C.free(unsafe.Pointer(cPath))
	}

	return code
}

// setInvalidHandle sets an invalid handle error.
func setInvalidHandle(cErr *C.cc_error, handleType string) C.cc_error_code {
	msg := "invalid " + handleType + " handle"
	C.set_error(cErr, C.CC_ERR_INVALID_HANDLE, C.CString(msg), nil, nil)
	return C.CC_ERR_INVALID_HANDLE
}

// setInvalidArgument sets an invalid argument error.
func setInvalidArgument(cErr *C.cc_error, msg string) C.cc_error_code {
	C.set_error(cErr, C.CC_ERR_INVALID_ARGUMENT, C.CString(msg), nil, nil)
	return C.CC_ERR_INVALID_ARGUMENT
}
