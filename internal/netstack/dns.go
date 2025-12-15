package netstack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/miekg/dns"
)

type dnsServer struct {
	log    *slog.Logger
	server *dns.Server
	lookup func(name string) (string, error)
}

func newDNSServer(logger *slog.Logger, lookup func(name string) (string, error), packetConn net.PacketConn) *dnsServer {
	srv := &dnsServer{
		log:    logger,
		lookup: lookup,
	}

	mux := dns.NewServeMux()
	mux.HandleFunc(".", srv.handleDNSRequest)

	srv.server = &dns.Server{
		Addr:       ":53",
		Net:        "udp",
		Handler:    mux,
		PacketConn: packetConn,
	}
	return srv
}

func (s *dnsServer) start() {
	go func() {
		if err := s.server.ActivateAndServe(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.log.Error("dns: server exited", "err", err)
		}
	}()
}

func (ns *NetStack) StopDNSServer() {
	if ns.dnsServer == nil {
		return
	}
	srv := ns.dnsServer
	ns.dnsServer = nil
	if srv.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_ = srv.server.ShutdownContext(ctx)
		if srv.server.PacketConn != nil {
			_ = srv.server.PacketConn.Close()
		}
	}
}

func (s *dnsServer) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false
	m.RecursionAvailable = true

	for _, q := range r.Question {
		if q.Qtype != dns.TypeA {
			continue
		}
		ip, err := s.lookup(q.Name)
		if err != nil {
			s.log.Debug("dns: lookup failed", "name", q.Name, "err", err)
			m.SetRcode(r, dns.RcodeNameError)
			continue
		}
		if ip == "" {
			s.log.Debug("dns: unknown name", "name", q.Name)
			m.SetRcode(r, dns.RcodeNameError)
			continue
		}
		rr, err := dns.NewRR(fmt.Sprintf("%s A %s", q.Name, ip))
		if err != nil {
			s.log.Debug("dns: create rr", "err", err)
			continue
		}
		m.Answer = append(m.Answer, rr)
	}

	_ = w.WriteMsg(m)
}
