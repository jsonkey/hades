// Copyright (c) 2014 The HADES Authors. All rights reserved.
// Use of this source code is governed by The MIT License (MIT) that can be
// found in the LICENSE file.

package server

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/miekg/dns"
)

// ServeDNSForward forwards a request to a nameservers and returns the response.
func (s *server) ServeDNSForward(w dns.ResponseWriter, req *dns.Msg) *dns.Msg {
	promExternalRequestCount.WithLabelValues("recursive").Inc()

	if len(s.config.Nameservers) == 0 || dns.CountLabel(req.Question[0].Name) < s.config.Ndots {

		if len(s.config.Nameservers) == 0 {
			glog.Infof("can not forward, no nameservers defined")
		} else {
			glog.Infof("can not forward, name too short (less than %d labels): `%s'", s.config.Ndots, req.Question[0].Name)
		}

		m := new(dns.Msg)
		m.SetReply(req)
		m.SetRcode(req, dns.RcodeServerFailure)
		m.Authoritative = false     // no matter what set to false
		m.RecursionAvailable = true // and this is still true
		w.WriteMsg(m)
		return m
	}

	tcp := isTCP(w)

	var (
		r   *dns.Msg
		err error
		try int
	)

	nsid := 0
Redo:
	switch tcp {
	case false:
		r, _, err = s.dnsUDPclient.Exchange(req, s.config.Nameservers[nsid])
	case true:
		r, _, err = s.dnsTCPclient.Exchange(req, s.config.Nameservers[nsid])
	}
	if err == nil {
		r.Compress = true
		r.Id = req.Id
		w.WriteMsg(r)
		return r
	}
	// Seen an error, this can only mean, "server not reached", try again
	// but only if we have not exausted our nameservers.
	if try < len(s.config.Nameservers) {
		try++
		nsid = (nsid + 1) % len(s.config.Nameservers)
		goto Redo
	}

	glog.Infof("failure to forward request %q, req name:%s", err,req.Question[0].Name)
	m := new(dns.Msg)
	m.SetReply(req)
	m.SetRcode(req, dns.RcodeServerFailure)
	w.WriteMsg(m)
	return m
}

// Lookup looks up name,type using the recursive nameserver defines
// in the server's config. If none defined it returns an error.
func (s *server) Lookup(n string, t, bufsize uint16) (*dns.Msg, error) {
	promExternalRequestCount.WithLabelValues("lookup").Inc()

	if len(s.config.Nameservers) == 0 {
		return nil, fmt.Errorf("no nameservers configured can not lookup name")
	}
	if dns.CountLabel(n) < s.config.Ndots {
		return nil, fmt.Errorf("name has fewer than %d labels", s.config.Ndots)
	}
	m := new(dns.Msg)
	m.SetQuestion(n, t)
	m.SetEdns0(bufsize, false)

	nsid := int(m.Id) % len(s.config.Nameservers)
	try := 0
Redo:
	r, _, err := s.dnsUDPclient.Exchange(m, s.config.Nameservers[nsid])
	if err == nil {
		if r.Rcode != dns.RcodeSuccess {
			return nil, fmt.Errorf("rcode is not equal to success")
		}
		// Reset TTLs to rcache TTL to make some of the other code
		// and the tests not care about TTLs
		for _, rr := range r.Answer {
			rr.Header().Ttl = uint32(s.config.RCacheTtl)
		}
		for _, rr := range r.Extra {
			rr.Header().Ttl = uint32(s.config.RCacheTtl)
		}
		return r, nil
	}
	// Seen an error, this can only mean, "server not reached", try again
	// but only if we have not exausted our nameservers.
	if try < len(s.config.Nameservers) {
		try++
		nsid = (nsid + 1) % len(s.config.Nameservers)
		goto Redo
	}
	return nil, fmt.Errorf("failure to lookup name")
}
