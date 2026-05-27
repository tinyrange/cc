package arm64vm

import (
	"bytes"
	"testing"
)

func TestBuildPersistentInitramfsIncludesUnixTime(t *testing.T) {
	initrd, err := BuildPersistentInitramfs(RunRequest{
		Init:     []byte("#!/init\n"),
		UnixTime: 1_700_000_123,
	}, nil, "/")
	if err != nil {
		t.Fatalf("BuildPersistentInitramfs() error = %v", err)
	}
	if !bytes.Contains(initrd, []byte(`"unix_time":1700000123`)) {
		t.Fatalf("initramfs does not contain unix_time config")
	}
}

func TestBuildExecInitramfsIncludesUnixTime(t *testing.T) {
	initrd, err := BuildExecInitramfs(RunRequest{
		Init:     []byte("#!/init\n"),
		UnixTime: 1_700_000_456,
	}, []string{"true"}, nil, "/")
	if err != nil {
		t.Fatalf("BuildExecInitramfs() error = %v", err)
	}
	if !bytes.Contains(initrd, []byte(`"unix_time":1700000456`)) {
		t.Fatalf("initramfs does not contain unix_time config")
	}
}

func TestBuildExecInitramfsIncludesUser(t *testing.T) {
	initrd, err := BuildExecInitramfs(RunRequest{
		Init: []byte("#!/init\n"),
		User: "1000:100",
	}, []string{"id"}, nil, "/")
	if err != nil {
		t.Fatalf("BuildExecInitramfs() error = %v", err)
	}
	if !bytes.Contains(initrd, []byte(`"user":"1000:100"`)) {
		t.Fatalf("initramfs does not contain user config")
	}
}
