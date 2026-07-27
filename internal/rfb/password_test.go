package rfb

import (
	"crypto/des"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

func TestPasswordSecurityAuthenticatesVNCResponse(t *testing.T) {
	server, viewer := net.Pipe()
	defer viewer.Close()
	done := make(chan error, 1)
	go func() {
		done <- PasswordSecurity("password").Handshake(server)
		_ = server.Close()
	}()

	challenge := make([]byte, 16)
	if _, err := io.ReadFull(viewer, challenge); err != nil {
		t.Fatal(err)
	}
	// VNC DES keys reverse the bit order of each password byte.
	cipher, err := des.NewCipher([]byte{0x0e, 0x86, 0xce, 0xce, 0xee, 0xf6, 0x4e, 0x26})
	if err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 16)
	cipher.Encrypt(response[:8], challenge[:8])
	cipher.Encrypt(response[8:], challenge[8:])
	if _, err := viewer.Write(response); err != nil {
		t.Fatal(err)
	}
	var status uint32
	if err := binary.Read(viewer, binary.BigEndian, &status); err != nil {
		t.Fatal(err)
	}
	if status != 0 {
		t.Fatalf("VNC authentication status = %d", status)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
