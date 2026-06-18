package nfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

const (
	rpcVersion = 2

	rpcMsgCall  = 0
	rpcMsgReply = 1

	rpcReplyAccepted = 0
	rpcAcceptSuccess = 0
	rpcProgUnavail   = 1
	rpcProcUnavail   = 3
	rpcGarbageArgs   = 4

	authNull = 0
	authUnix = 1

	progPortmap = 100000
	progMount   = 100005
	progNFS     = 100003

	portmapVersion  = 2
	rpcbindVersion3 = 3
	rpcbindVersion4 = 4
	mountVersion3   = 3
	nfsVersion3     = 3

	portmapProcNull = 0
	portmapProcGet  = 3

	rpcbindProcNull    = 0
	rpcbindProcGetAddr = 3

	mountProcNull    = 0
	mountProcMnt     = 1
	mountProcDump    = 2
	mountProcUmnt    = 3
	mountProcUmntAll = 4
	mountProcExport  = 5

	nfsProcNull        = 0
	nfsProcGetAttr     = 1
	nfsProcSetAttr     = 2
	nfsProcLookup      = 3
	nfsProcAccess      = 4
	nfsProcReadlink    = 5
	nfsProcRead        = 6
	nfsProcWrite       = 7
	nfsProcCreate      = 8
	nfsProcMkdir       = 9
	nfsProcSymlink     = 10
	nfsProcMknod       = 11
	nfsProcRemove      = 12
	nfsProcRmdir       = 13
	nfsProcRename      = 14
	nfsProcLink        = 15
	nfsProcReaddir     = 16
	nfsProcReaddirPlus = 17
	nfsProcFsstat      = 18
	nfsProcFsinfo      = 19
	nfsProcPathconf    = 20
	nfsProcCommit      = 21
)

type rpcCall struct {
	xid     uint32
	prog    uint32
	version uint32
	proc    uint32
	body    []byte
}

type rpcHandler func(call rpcCall) ([]byte, uint32)

func serveRPCListener(ln net.Listener, handler rpcHandler, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				serveRPCConn(conn, handler)
			}()
		}
	}()
}

func serveRPCPacketConn(pc net.PacketConn, handler rpcHandler, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 64<<10)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			call, err := parseRPCCall(buf[:n])
			if err != nil {
				_, _ = pc.WriteTo(rpcReplyPayload(0, rpcGarbageArgs, nil), addr)
				continue
			}
			body, accept := handler(call)
			_, _ = pc.WriteTo(rpcReplyPayload(call.xid, accept, body), addr)
		}
	}()
}

func serveRPCConn(conn net.Conn, handler rpcHandler) {
	defer conn.Close()
	for {
		record, err := readRPCRecord(conn)
		if err != nil {
			return
		}
		call, err := parseRPCCall(record)
		if err != nil {
			_, _ = conn.Write(rpcReply(0, rpcGarbageArgs, nil))
			continue
		}
		body, accept := handler(call)
		if _, err := conn.Write(rpcReply(call.xid, accept, body)); err != nil {
			return
		}
	}
}

func readRPCRecord(r io.Reader) ([]byte, error) {
	var out []byte
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, err
		}
		marker := binary.BigEndian.Uint32(hdr[:])
		last := marker&0x80000000 != 0
		n := int(marker & 0x7fffffff)
		if n < 0 || n > 32<<20 {
			return nil, fmt.Errorf("rpc record too large: %d", n)
		}
		chunk := make([]byte, n)
		if _, err := io.ReadFull(r, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if last {
			return out, nil
		}
	}
}

func parseRPCCall(data []byte) (rpcCall, error) {
	r := newXDRReader(data)
	xid, err := r.Uint32()
	if err != nil {
		return rpcCall{}, err
	}
	msgType, err := r.Uint32()
	if err != nil {
		return rpcCall{}, err
	}
	if msgType != rpcMsgCall {
		return rpcCall{}, fmt.Errorf("rpc msg type %d is not call", msgType)
	}
	version, err := r.Uint32()
	if err != nil {
		return rpcCall{}, err
	}
	if version != rpcVersion {
		return rpcCall{}, fmt.Errorf("rpc version %d", version)
	}
	prog, err := r.Uint32()
	if err != nil {
		return rpcCall{}, err
	}
	progVersion, err := r.Uint32()
	if err != nil {
		return rpcCall{}, err
	}
	proc, err := r.Uint32()
	if err != nil {
		return rpcCall{}, err
	}
	if err := r.SkipAuth(); err != nil {
		return rpcCall{}, err
	}
	if err := r.SkipAuth(); err != nil {
		return rpcCall{}, err
	}
	return rpcCall{xid: xid, prog: prog, version: progVersion, proc: proc, body: data[r.off:]}, nil
}

func rpcReply(xid uint32, accept uint32, body []byte) []byte {
	payload := rpcReplyPayload(xid, accept, body)
	out := make([]byte, 4, 4+len(payload))
	binary.BigEndian.PutUint32(out[:4], 0x80000000|uint32(len(payload)))
	out = append(out, payload...)
	return out
}

func rpcReplyPayload(xid uint32, accept uint32, body []byte) []byte {
	var w xdrWriter
	w.Uint32(xid)
	w.Uint32(rpcMsgReply)
	w.Uint32(rpcReplyAccepted)
	w.Uint32(authNull)
	w.Uint32(0)
	w.Uint32(accept)
	if accept == rpcAcceptSuccess {
		w.data = append(w.data, body...)
	}
	return w.Bytes()
}

func isClosedNetErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, net.ErrClosed)
}
