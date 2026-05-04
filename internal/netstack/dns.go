package netstack

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
)

const (
	dnsTypeA   = 1
	dnsClassIN = 1

	dnsRCodeFormatError = 1
	dnsRCodeNameError   = 3
)

type dnsServer struct {
	pc     net.PacketConn
	lookup func(name string) (string, error)

	closeOnce sync.Once
	done      chan struct{}
}

func newDNSServer(pc net.PacketConn, lookup func(name string) (string, error)) *dnsServer {
	s := &dnsServer{
		pc:     pc,
		lookup: lookup,
		done:   make(chan struct{}),
	}
	go s.serve()
	return s
}

func (s *dnsServer) close() {
	s.closeOnce.Do(func() {
		_ = s.pc.Close()
		<-s.done
	})
}

func (s *dnsServer) serve() {
	defer close(s.done)

	var buf [1500]byte
	for {
		n, addr, err := s.pc.ReadFrom(buf[:])
		if err != nil {
			return
		}
		resp := buildDNSResponse(buf[:n], s.lookup)
		if len(resp) == 0 {
			continue
		}
		_, _ = s.pc.WriteTo(resp, addr)
	}
}

func (ns *NetStack) StopDNSServer() {
	if ns.dnsServer == nil {
		return
	}
	srv := ns.dnsServer
	ns.dnsServer = nil
	srv.close()
}

type dnsQuestion struct {
	nameStart int
	nameEnd   int
	name      string
	qtype     uint16
	qclass    uint16
}

func buildDNSResponse(query []byte, lookup func(name string) (string, error)) []byte {
	if len(query) < 12 {
		return nil
	}

	qdCount := int(binary.BigEndian.Uint16(query[4:6]))
	if qdCount == 0 {
		return buildDNSError(query, dnsRCodeFormatError)
	}

	questions := make([]dnsQuestion, 0, qdCount)
	off := 12
	for i := 0; i < qdCount; i++ {
		q, next, err := parseDNSQuestion(query, off)
		if err != nil {
			return buildDNSError(query, dnsRCodeFormatError)
		}
		questions = append(questions, q)
		off = next
	}

	answers := make([]struct {
		question int
		ip       [4]byte
	}, 0, len(questions))
	rcode := uint16(0)
	for i, q := range questions {
		if q.qtype != dnsTypeA || q.qclass != dnsClassIN {
			continue
		}
		ipText, err := lookup(q.name)
		if err != nil || ipText == "" {
			rcode = dnsRCodeNameError
			continue
		}
		ip4 := net.ParseIP(ipText).To4()
		if ip4 == nil {
			rcode = dnsRCodeNameError
			continue
		}
		var addr [4]byte
		copy(addr[:], ip4)
		answers = append(answers, struct {
			question int
			ip       [4]byte
		}{question: i, ip: addr})
	}
	if len(answers) > 0 {
		rcode = 0
	}

	size := off + len(answers)*16
	resp := make([]byte, size)
	copy(resp[:off], query[:off])
	flags := uint16(0x8000 | 0x0080 | rcode) // response + recursion available.
	if binary.BigEndian.Uint16(query[2:4])&0x0100 != 0 {
		flags |= 0x0100 // preserve recursion desired.
	}
	binary.BigEndian.PutUint16(resp[2:4], flags)
	binary.BigEndian.PutUint16(resp[6:8], uint16(len(answers)))
	binary.BigEndian.PutUint16(resp[8:10], 0)
	binary.BigEndian.PutUint16(resp[10:12], 0)

	out := off
	for _, ans := range answers {
		q := questions[ans.question]
		// Compression pointer to the question name.
		binary.BigEndian.PutUint16(resp[out:out+2], 0xc000|uint16(q.nameStart))
		binary.BigEndian.PutUint16(resp[out+2:out+4], dnsTypeA)
		binary.BigEndian.PutUint16(resp[out+4:out+6], dnsClassIN)
		binary.BigEndian.PutUint32(resp[out+6:out+10], 30)
		binary.BigEndian.PutUint16(resp[out+10:out+12], 4)
		copy(resp[out+12:out+16], ans.ip[:])
		out += 16
	}

	return resp
}

func buildDNSError(query []byte, rcode uint16) []byte {
	resp := make([]byte, 12)
	copy(resp, query[:12])
	flags := uint16(0x8000 | 0x0080 | (rcode & 0x000f))
	if binary.BigEndian.Uint16(query[2:4])&0x0100 != 0 {
		flags |= 0x0100
	}
	binary.BigEndian.PutUint16(resp[2:4], flags)
	binary.BigEndian.PutUint16(resp[4:6], 0)
	binary.BigEndian.PutUint16(resp[6:8], 0)
	binary.BigEndian.PutUint16(resp[8:10], 0)
	binary.BigEndian.PutUint16(resp[10:12], 0)
	return resp
}

func parseDNSQuestion(msg []byte, off int) (dnsQuestion, int, error) {
	start := off
	var b strings.Builder
	for {
		if off >= len(msg) {
			return dnsQuestion{}, 0, fmt.Errorf("dns: name exceeds packet")
		}
		labelLen := int(msg[off])
		off++
		if labelLen == 0 {
			break
		}
		if labelLen&0xc0 != 0 {
			return dnsQuestion{}, 0, fmt.Errorf("dns: compressed question name unsupported")
		}
		if labelLen > 63 || off+labelLen > len(msg) {
			return dnsQuestion{}, 0, fmt.Errorf("dns: invalid label length")
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		for _, c := range msg[off : off+labelLen] {
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			b.WriteByte(c)
		}
		off += labelLen
	}
	if off+4 > len(msg) {
		return dnsQuestion{}, 0, fmt.Errorf("dns: question truncated")
	}
	q := dnsQuestion{
		nameStart: start,
		nameEnd:   off,
		name:      b.String(),
		qtype:     binary.BigEndian.Uint16(msg[off : off+2]),
		qclass:    binary.BigEndian.Uint16(msg[off+2 : off+4]),
	}
	return q, off + 4, nil
}
