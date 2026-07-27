package rfb

import (
	"crypto/des"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
)

const securityVNCAuthentication = 2

// PasswordSecurity implements the standard VNC challenge-response handshake.
// The protocol only uses the first eight bytes of a password.
type PasswordSecurity string

func (PasswordSecurity) Type() uint8 {
	return securityVNCAuthentication
}

func (s PasswordSecurity) Handshake(conn io.ReadWriter) error {
	password := []byte(s)
	if len(password) == 0 {
		return fmt.Errorf("VNC password is empty")
	}
	if len(password) > 8 {
		return fmt.Errorf("VNC passwords are limited to 8 bytes")
	}
	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return fmt.Errorf("generate VNC challenge: %w", err)
	}
	if _, err := conn.Write(challenge); err != nil {
		return err
	}
	response := make([]byte, len(challenge))
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	key := make([]byte, 8)
	for index, value := range password {
		key[index] = reverseByte(value)
	}
	cipher, err := des.NewCipher(key)
	if err != nil {
		return err
	}
	expected := make([]byte, len(challenge))
	cipher.Encrypt(expected[:8], challenge[:8])
	cipher.Encrypt(expected[8:], challenge[8:])
	if subtle.ConstantTimeCompare(response, expected) != 1 {
		if err := binary.Write(conn, binary.BigEndian, uint32(1)); err != nil {
			return err
		}
		reason := "authentication failed"
		if err := binary.Write(conn, binary.BigEndian, uint32(len(reason))); err != nil {
			return err
		}
		_, _ = io.WriteString(conn, reason)
		return fmt.Errorf("VNC authentication failed")
	}
	return binary.Write(conn, binary.BigEndian, uint32(0))
}

func reverseByte(value byte) byte {
	value = value>>4 | value<<4
	value = (value&0xcc)>>2 | (value&0x33)<<2
	return (value&0xaa)>>1 | (value&0x55)<<1
}
