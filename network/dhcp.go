package network

import (
	"bytes"
	"encoding/binary"
)

// dhcp is a fixed single-client RFC 2131 lease, not a configurable host DHCP
// service. It is reached only after Ethernet/IP source and length validation.
func (n *Isolated) dhcp(ip []byte, header int) {
	udp := ip[header:]
	if len(udp) < 8 || binary.BigEndian.Uint16(udp) != 68 || binary.BigEndian.Uint16(udp[2:]) != 67 {
		return
	}
	size := int(binary.BigEndian.Uint16(udp[4:]))
	if size < 8+240 || size > len(udp) {
		return
	}
	udp = udp[:size]
	if binary.BigEndian.Uint16(udp[6:]) != 0 {
		pseudo := append([]byte(nil), ip[12:20]...)
		pseudo = append(pseudo, 0, 17, byte(size>>8), byte(size))
		if checksum(append(pseudo, udp...)) != 0 {
			return
		}
	}
	b := udp[8:]
	if b[0] != 1 || b[1] != 1 || b[2] != 6 || b[3] != 0 || !bytes.Equal(b[28:34], n.guest[:]) || !bytes.Equal(b[24:28], []byte{0, 0, 0, 0}) || !bytes.Equal(b[236:240], []byte{99, 130, 83, 99}) {
		return
	}
	var options [256][]byte
	ended := false
	for at := 240; at < len(b); {
		code := b[at]
		at++
		if code == 255 {
			ended = true
			break
		}
		if code == 0 {
			continue
		}
		if at == len(b) {
			return
		}
		size := int(b[at])
		at++
		if size > len(b)-at || options[code] != nil {
			return
		}
		options[code] = b[at : at+size]
		at += size
	}
	if !ended || len(options[53]) != 1 || options[52] != nil {
		return
	} // No option overload.
	if options[54] != nil && !bytes.Equal(options[54], hostIP[:]) {
		return
	}
	kind := options[53][0]
	replyKind := byte(0)
	switch kind {
	case 1:
		replyKind = 2 // DISCOVER -> OFFER
	case 3: // REQUEST in SELECTING, INIT-REBOOT, or RENEWING.
		requested := options[50]
		if requested == nil {
			requested = b[12:16]
		}
		if !bytes.Equal(requested, guestIP[:]) {
			replyKind = 6
		} else {
			replyKind = 5
		}
	case 8: // INFORM: address already configured; return configuration only.
		if !bytes.Equal(b[12:16], guestIP[:]) {
			return
		}
		replyKind = 5
	default:
		return
	}
	out := make([]byte, 240)
	out[0], out[1], out[2] = 2, 1, 6
	copy(out[4:8], b[4:8])
	copy(out[10:12], b[10:12])
	if replyKind != 6 {
		copy(out[12:16], b[12:16])
		if kind != 8 {
			copy(out[16:20], guestIP[:])
		}
	}
	copy(out[28:34], n.guest[:])
	copy(out[236:240], []byte{99, 130, 83, 99})
	option := func(code byte, value []byte) { out = append(out, code, byte(len(value))); out = append(out, value...) }
	option(53, []byte{replyKind})
	option(54, hostIP[:])
	if replyKind != 6 {
		option(1, []byte{255, 255, 255, 0})
		if kind != 8 {
			option(51, []byte{0, 1, 81, 128})
		} // 86400 seconds.
	}
	if options[61] != nil {
		option(61, options[61])
	}
	out = append(out, 255)
	if len(out) < 300 {
		out = append(out, make([]byte, 300-len(out))...)
	}
	// Broadcast is usable before the client has configured its offered address.
	frame := make([]byte, 14+20+8+len(out))
	copy(frame[:6], broadcastMAC[:])
	copy(frame[6:12], n.host[:])
	binary.BigEndian.PutUint16(frame[12:], 0x0800)
	h := frame[14:34]
	h[0], h[8], h[9] = 0x45, 64, 17
	binary.BigEndian.PutUint16(h[2:], uint16(20+8+len(out)))
	copy(h[12:16], hostIP[:])
	copy(h[16:20], broadcastIP[:])
	binary.BigEndian.PutUint16(h[10:], checksum(h))
	u := frame[34:]
	binary.BigEndian.PutUint16(u, 67)
	binary.BigEndian.PutUint16(u[2:], 68)
	binary.BigEndian.PutUint16(u[4:], uint16(8+len(out)))
	copy(u[8:], out)
	_ = n.enqueue(frame)
}
