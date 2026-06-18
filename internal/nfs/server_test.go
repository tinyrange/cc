package nfs

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"j5.nz/cc/client"
)

func TestServerAddShareBuildsExportAndRejectsConflict(t *testing.T) {
	dir := t.TempDir()
	server := New(nil)
	exp, err := server.AddShare(client.ShareMount{Source: dir, Mount: "/host", Writable: true})
	if err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	if exp.Name == "" || exp.Mount != "/host" || !exp.Writable {
		t.Fatalf("export = %+v", exp)
	}
	if _, err := server.AddShare(client.ShareMount{Source: dir, Mount: "/host", Writable: true}); err != nil {
		t.Fatalf("idempotent AddShare: %v", err)
	}
	if _, err := server.AddShare(client.ShareMount{Source: dir, Mount: "/host", Writable: false}); err == nil {
		t.Fatalf("conflicting AddShare succeeded")
	}
}

func TestServerNFSLookupReadWrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil)
	exp, err := server.AddShare(client.ShareMount{Source: dir, Mount: "/host", Writable: true})
	if err != nil {
		t.Fatalf("AddShare: %v", err)
	}

	lookup := xdrWriter{}
	lookup.Opaque(fileHandle(exp.ID, 1))
	lookup.String("hello.txt")
	body, err := server.handleNFSProc(nfsProcLookup, lookup.Bytes())
	if err != nil {
		t.Fatalf("LOOKUP: %v", err)
	}
	r := newXDRReader(body)
	status, _ := r.Uint32()
	if status != nfsOK {
		t.Fatalf("LOOKUP status = %d", status)
	}
	fh, err := r.Opaque(64)
	if err != nil {
		t.Fatalf("LOOKUP file handle: %v", err)
	}

	read := xdrWriter{}
	read.Opaque(fh)
	read.Uint64(0)
	read.Uint32(16)
	read.Uint32(16)
	body, err = server.handleNFSProc(nfsProcRead, read.Bytes())
	if err != nil {
		t.Fatalf("READ: %v", err)
	}
	r = newXDRReader(body)
	status, _ = r.Uint32()
	if status != nfsOK {
		t.Fatalf("READ status = %d", status)
	}
	if _, err := r.Uint32(); err != nil { // post-op attr present
		t.Fatal(err)
	}
	if err := skipFAttr(r); err != nil {
		t.Fatal(err)
	}
	count, _ := r.Uint32()
	if count != 5 {
		t.Fatalf("READ count = %d", count)
	}
	_, _ = r.Uint32()
	data, err := r.Opaque(16)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("READ data = %q", data)
	}
}

func TestGetAttrReturnsBareFAttr(t *testing.T) {
	dir := t.TempDir()
	server := New(nil)
	exp, err := server.AddShare(client.ShareMount{Source: dir, Mount: "/host"})
	if err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	req := xdrWriter{}
	req.Opaque(fileHandle(exp.ID, 1))
	body, err := server.handleNFSProc(nfsProcGetAttr, req.Bytes())
	if err != nil {
		t.Fatalf("GETATTR: %v", err)
	}
	r := newXDRReader(body)
	status, _ := r.Uint32()
	if status != nfsOK {
		t.Fatalf("GETATTR status = %d", status)
	}
	fileType, err := r.Uint32()
	if err != nil {
		t.Fatal(err)
	}
	if fileType != nfsFileDir {
		t.Fatalf("GETATTR first fattr field = %d, want directory type", fileType)
	}
}

func TestFSInfoResponseIncludesProperties(t *testing.T) {
	dir := t.TempDir()
	server := New(nil)
	exp, err := server.AddShare(client.ShareMount{Source: dir, Mount: "/host"})
	if err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	req := xdrWriter{}
	req.Opaque(fileHandle(exp.ID, 1))
	body, err := server.handleNFSProc(nfsProcFsinfo, req.Bytes())
	if err != nil {
		t.Fatalf("FSINFO: %v", err)
	}
	r := newXDRReader(body)
	status, _ := r.Uint32()
	if status != nfsOK {
		t.Fatalf("FSINFO status = %d", status)
	}
	present, _ := r.Uint32()
	if present != 1 {
		t.Fatalf("FSINFO attr present = %d", present)
	}
	if err := skipFAttr(r); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		if _, err := r.Uint32(); err != nil {
			t.Fatalf("FSINFO uint32 field %d: %v", i, err)
		}
	}
	if _, err := r.Uint64(); err != nil {
		t.Fatalf("FSINFO maxfilesize: %v", err)
	}
	if _, err := r.Uint32(); err != nil {
		t.Fatalf("FSINFO time_delta seconds: %v", err)
	}
	if _, err := r.Uint32(); err != nil {
		t.Fatalf("FSINFO time_delta nseconds: %v", err)
	}
	properties, err := r.Uint32()
	if err != nil {
		t.Fatalf("FSINFO properties: %v", err)
	}
	if properties == 0 {
		t.Fatalf("FSINFO properties = 0")
	}
	if r.remaining() != 0 {
		t.Fatalf("FSINFO trailing bytes = %d", r.remaining())
	}
}

func TestRPCBindGetAddrReturnsTCPUniversalAddress(t *testing.T) {
	server := New(nil)
	req := xdrWriter{}
	req.Uint32(progNFS)
	req.Uint32(nfsVersion3)
	req.String("tcp")
	req.String("")
	req.String("")
	body, accept := server.handlePortmap(rpcCall{prog: progPortmap, version: rpcbindVersion4, proc: rpcbindProcGetAddr, body: req.Bytes()})
	if accept != rpcAcceptSuccess {
		t.Fatalf("GETADDR accept = %d", accept)
	}
	r := newXDRReader(body)
	addr, err := r.String(256)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "10.42.0.1.8.1" {
		t.Fatalf("NFS universal address = %q", addr)
	}

	req = xdrWriter{}
	req.Uint32(progMount)
	req.Uint32(mountVersion3)
	req.String("tcp")
	req.String("")
	req.String("")
	body, accept = server.handlePortmap(rpcCall{prog: progPortmap, version: rpcbindVersion3, proc: rpcbindProcGetAddr, body: req.Bytes()})
	if accept != rpcAcceptSuccess {
		t.Fatalf("GETADDR mount accept = %d", accept)
	}
	r = newXDRReader(body)
	addr, err = r.String(256)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "10.42.0.1.78.80" {
		t.Fatalf("mountd universal address = %q", addr)
	}
}

func TestMountReplyAdvertisesAuthUnix(t *testing.T) {
	dir := t.TempDir()
	server := New(nil)
	exp, err := server.AddShare(client.ShareMount{Source: dir, Mount: "/host"})
	if err != nil {
		t.Fatalf("AddShare: %v", err)
	}
	req := xdrWriter{}
	req.String(exp.Name)
	body, accept := server.handleMount(rpcCall{prog: progMount, version: mountVersion3, proc: mountProcMnt, body: req.Bytes()})
	if accept != rpcAcceptSuccess {
		t.Fatalf("MNT accept = %d", accept)
	}
	r := newXDRReader(body)
	status, _ := r.Uint32()
	if status != nfsOK {
		t.Fatalf("MNT status = %d", status)
	}
	if _, err := r.Opaque(64); err != nil {
		t.Fatalf("MNT file handle: %v", err)
	}
	count, err := r.Uint32()
	if err != nil {
		t.Fatalf("MNT auth count: %v", err)
	}
	if count != 1 {
		t.Fatalf("MNT auth count = %d", count)
	}
	flavor, err := r.Uint32()
	if err != nil {
		t.Fatalf("MNT auth flavor: %v", err)
	}
	if flavor != authUnix {
		t.Fatalf("MNT auth flavor = %d, want AUTH_UNIX", flavor)
	}
}

func TestMountCommandUsesOSCompatibleOptions(t *testing.T) {
	tests := []struct {
		osName string
		want   []string
		avoid  []string
	}{
		{osName: "openbsd", want: []string{"tcp", "port=2049"}, avoid: []string{"mountport", "nolock"}},
		{osName: "freebsd", want: []string{"nfsv3", "tcp", "port=2049", "mountport=20048", "nolockd"}},
		{osName: "netbsd", want: []string{"/sbin/mount_nfs", "-3", "-T", "-p"}, avoid: []string{"mountport", "nolock"}},
	}
	for _, tt := range tests {
		cmd := MountCommand(tt.osName, "10.42.0.1", "/ccx3/1", "/host")
		joined := ""
		for _, part := range cmd {
			joined += part + " "
		}
		for _, want := range tt.want {
			if !contains(joined, want) {
				t.Fatalf("%s mount command = %q, missing %q", tt.osName, joined, want)
			}
		}
		for _, avoid := range tt.avoid {
			if contains(joined, avoid) {
				t.Fatalf("%s mount command = %q, contains unsupported %q", tt.osName, joined, avoid)
			}
		}
	}
}

func skipFAttr(r *xdrReader) error {
	for i := 0; i < 21; i++ {
		if _, err := r.Uint32(); err != nil {
			return err
		}
	}
	return nil
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && index(s, sub) >= 0)
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

type fakeNetwork struct{}

func (fakeNetwork) ListenInternal(network, address string) (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(context.Background(), network, "127.0.0.1:0")
}
