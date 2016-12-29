// Copyright (c) 2014 The HADES Authors. All rights reserved.
// Use of this source code is governed by The MIT License (MIT) that can be
// found in the LICENSE file.

package server

import (
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/miekg/dns"
	"github.com/ipdcode/hades/cache"
	"github.com/ipdcode/hades/msg"
	"github.com/golang/glog"
)

type server struct {
	backend Backend
	config  *Config

	group        *sync.WaitGroup
	dnsUDPclient *dns.Client // used for forwarding queries
	dnsTCPclient *dns.Client // used for forwarding queries
	rcache       *cache.Cache
	ipMonitorPath  string
}

type Backend interface {
	Records(name string, exact bool) ([]msg.Service, error)
	ReverseRecord(name string) (*msg.Service, error)
	ParseRecords(node *etcd.Node) ([]msg.Service, error)
	Get(path string) (*etcd.Response, error)
}

// FirstBackend exposes the Backend interface over multiple Backends, returning
// the first Backend that answers the provided record request. If no Backend answers
// a record request, the last error seen will be returned.
type FirstBackend []Backend

// FirstBackend implements Backend
var _ Backend = FirstBackend{}

func (g FirstBackend) Records(name string, exact bool) (records []msg.Service, err error) {
	var lastError error
	for _, backend := range g {
		if records, err = backend.Records(name, exact); err == nil && len(records) > 0 {
			return records, nil
		}
		if err != nil {
			lastError = err
		}
	}
	return nil, lastError
}

func (g FirstBackend) ParseRecords(node *etcd.Node) (records []msg.Service, err error) {
	var lastError error
	for _, backend := range g {
		if records, err = backend.ParseRecords(node); err == nil && len(records) > 0 {
			return records, nil
		}
		if err != nil {
			lastError = err
		}
	}
	return nil, lastError
}
func (g FirstBackend)Get(path string) (records *etcd.Response, err error) {
	var lastError error
	for _, backend := range g {
		if records, err = backend.Get(path); err == nil {
			return records, nil
		}
		if err != nil {
			lastError = err
		}
	}
	return nil, lastError
}

func (g FirstBackend) ReverseRecord(name string) (record *msg.Service, err error) {
	var lastError error
	for _, backend := range g {
		if record, err = backend.ReverseRecord(name); err == nil && record != nil {
			return record, nil
		}
		if err != nil {
			lastError = err
		}
	}
	return nil, lastError
}

// New returns a new HADES server.
func New(backend Backend, config *Config) *server {
	return &server{
		backend: backend,
		config:  config,

		group:        new(sync.WaitGroup),
		rcache:       cache.New(config.RCache, config.RCacheTtl),
		dnsUDPclient: &dns.Client{Net: "udp", ReadTimeout: config.ReadTimeout, WriteTimeout: config.ReadTimeout, SingleInflight: true},
		dnsTCPclient: &dns.Client{Net: "tcp", ReadTimeout: config.ReadTimeout, WriteTimeout: config.ReadTimeout, SingleInflight: true},
		ipMonitorPath : config.IpMonitorPath,
	}
}


func (s *server)getSvcDomainName(key string) string{
	keys := strings.Split(key,"/")
	domLen := len(keys)-1
	for i, j := 0,domLen; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	domainKey := strings.Join(keys[1:], ".") // ingoore the first
	//glog.Infof("domainKey =%s\n",domainKey )
	return domainKey
}
func (s *server)getSvcCnameName(key string) string{
	keys := strings.Split(key,"/")
	domLen := len(keys)-1
	for i, j := 0,domLen; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	domainKey := strings.Join(keys, ".")
	return domainKey
}

func (s *server) updateRcacheParseRecord(node *etcd.Node) interface{} {
	records, err := s.backend.ParseRecords(node)
	if err != nil {
		glog.Infof("ParseRecords err %s \n",err.Error() )
		return nil
	}
	if len(records)==0 { // no result it is a dir
		return nil
	}
	ip := net.ParseIP(records[0].Host)
	switch {
	case ip == nil:
		name := s.getSvcCnameName(records[0].Key)
		return records[0].NewCNAME(name, dns.Fqdn(records[0].Host))
	case ip.To4() != nil:
		name := s.getSvcDomainName(records[0].Key)
		return records[0].NewA(name, ip.To4())
	default:
		glog.Infof("updateRcacheParseRecord err \n" )
	}

  	return nil
}

func (s *server) UpdateRcache(resp *etcd.Response) {
        glog.V(2).Infof("UpdateRcache: Action =%s Key=%s", resp.Action, resp.Node.Key)

	switch strings.ToLower(resp.Action){
		case "create":
			fallthrough
		case "set":
			valNew := s.updateRcacheParseRecord(resp.Node)
			var valOld interface{} = nil
			if resp.PrevNode != nil {
				valOld = s.updateRcacheParseRecord(resp.PrevNode)
			}
			if valNew != nil && valOld != nil{
				s.rcache.UpdateRcacheUpdate(valOld, valNew)
			}else if valNew != nil{
				s.rcache.UpdateRcacheSet(valNew)
                        }else{
				glog.Infof("UpdateRcache  set err \n" )
			}
		case "compareanddelete":
			fallthrough
 		case "delete":
			valA := s.updateRcacheParseRecord(resp.PrevNode)
			if valA != nil {
				s.rcache.UpdateRcacheDelete(valA)
                        }else{
				glog.Infof("UpdateRcache  del err \n" )
			}
		case "compareandswap":
			fallthrough
		case "update":
			valA := s.updateRcacheParseRecord(resp.Node)
			var valAOld interface{}  = nil
			if resp.PrevNode != nil{
				valAOld = s.updateRcacheParseRecord(resp.PrevNode)
			}
			if valA != nil && valAOld != nil{
				s.rcache.UpdateRcacheUpdate(valAOld, valA)
                        }else{
				glog.Infof("UpdateRcache  update err \n" )
			}
		default:
		    	glog.Infof("the action not monitored: Action =%s Key=%s", resp.Action, resp.Node.Key)

	}
}
// Run is a blocking operation that starts the server listening on the DNS ports.
func (s *server) Run() error {
	mux := dns.NewServeMux()
	mux.Handle(".", s)
	s.group.Add(1)
	go func() {
		defer s.group.Done()
		if err := dns.ListenAndServe(s.config.DnsAddr, "tcp", mux); err != nil {
			glog.Fatalf("%s", err)
		}
	}()
	glog.Infof("ready for queries on %s for %s://%s [rcache %d]", s.config.Domain, "tcp", s.config.DnsAddr, s.config.RCache)
	s.group.Add(1)
	go func() {
		defer s.group.Done()
		if err := dns.ListenAndServe(s.config.DnsAddr, "udp", mux); err != nil {
			glog.Fatalf("%s", err)
		}
	}()
	glog.Infof("ready for queries on %s for %s://%s [rcache %d]", s.config.Domain, "udp", s.config.DnsAddr, s.config.RCache)

	s.group.Wait()
	return nil
}

// Stop stops a server.
func (s *server) Stop() {
	glog.Infof("exit from hades\n")
}

func Fit(m *dns.Msg, size int, tcp bool) (*dns.Msg, bool) {
	if m.Len() > size {
		m.Extra = nil
	}
	if m.Len() < size {
		return m, false
	}

	// With TCP setting TC does not mean anything.
	if !tcp {
		m.Truncated = true
	}

	// Additional section is gone, binary search until we have length that fits.
	min, max := 0, len(m.Answer)
	original := make([]dns.RR, len(m.Answer))
	copy(original, m.Answer)
	for {
		if min == max {
			break
		}

		mid := (min + max) / 2
		m.Answer = original[:mid]

		if m.Len() < size {
			min++
			continue
		}
		max = mid

	}
	if max > 1 {
		max--
	}
	m.Answer = m.Answer[:max]
	return m, true
}
// ServeDNS is the handler for DNS requests, responsible for parsing DNS request, possibly forwarding
// it to a real dns server and returning a response.
func (s *server) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.Authoritative = true
	m.RecursionAvailable = true
	m.Compress = true
	bufsize := uint16(512)
	tcp := false
	start := time.Now()

	q := req.Question[0]
	name := strings.ToLower(q.Name)

	if q.Qtype == dns.TypeANY {
		m.Authoritative = false
		m.Rcode = dns.RcodeRefused
		m.RecursionAvailable = false
		m.RecursionDesired = false
		m.Compress = false
		// if write fails don't care
		w.WriteMsg(m)

		promErrorCount.WithLabelValues("refused").Inc()
		return
	}

	if bufsize < 512 {
		bufsize = 512
	}
	// with TCP we can send 64K
	if tcp = isTCP(w); tcp {
		bufsize = dns.MaxMsgSize - 1
		promRequestCount.WithLabelValues("tcp").Inc()
	} else {
		promRequestCount.WithLabelValues("udp").Inc()
	}

	glog.V(2).Infof("received DNS Request for %q from %q with type %d", q.Name, w.RemoteAddr(), q.Qtype)

	// Check cache first.
	remoteAddr := w.RemoteAddr().String() //10.8.65.158:42158
	remoteIp := strings.Split(remoteAddr, ":")
	m1 := s.rcache.Search(q, tcp, m.Id,remoteIp[0])
	if m1 != nil {
		glog.V(4).Infof("cache hit %q: %v\n ", q.Name,m1.Answer)
		if tcp {
			if _, overflow := Fit(m1, dns.MaxMsgSize, tcp); overflow {
				promErrorCount.WithLabelValues("overflow").Inc()
				msgFail := new(dns.Msg)
				s.ServerFailure(msgFail, req)
				w.WriteMsg(msgFail)
				return
			}
		} else {
			// Overflow with udp always results in TC.
			Fit(m1, int(bufsize), tcp)
			if m1.Truncated {
				promErrorCount.WithLabelValues("truncated").Inc()
			}
		}

		if err := w.WriteMsg(m1); err != nil {
			glog.Infof("failure to return reply %q", err)
		}
		metricSizeAndDuration(m1, start, tcp)
		return
	}

	if q.Qclass != dns.ClassCHAOS && !strings.HasSuffix(name, s.config.Domain) {
		resp := s.ServeDNSForward(w, req)
		glog.V(4).Infof("ServeDNSForward %q: %v \n ", q.Name, resp.Answer)
		if resp != nil {
			s.rcache.InsertMessage(cache.Key(q, tcp), resp,remoteIp[0])
			metricSizeAndDuration(resp, start, tcp)
		}
		return
	}

	promCacheMiss.WithLabelValues("response").Inc()

	defer func() {
		glog.V(4).Infof("get  %q: %v \n ", q.Name, m.Answer)
		if m.Rcode == dns.RcodeServerFailure {
			if err := w.WriteMsg(m); err != nil {
				glog.Infof("failure to return reply %q", err)
			}
			return
		}
		// Set TTL to the minimum of the RRset and dedup the message, i.e. remove identical RRs.
		m = s.dedup(m)

		minttl := s.config.Ttl
		if len(m.Answer) > 1 {
			for _, r := range m.Answer {
				if r.Header().Ttl < minttl {
					minttl = r.Header().Ttl
				}
			}
			for _, r := range m.Answer {
				r.Header().Ttl = minttl
			}
		}

		if tcp {
			if _, overflow := Fit(m, dns.MaxMsgSize, tcp); overflow {
				msgFail := new(dns.Msg)
				s.ServerFailure(msgFail, req)
				w.WriteMsg(msgFail)
				return
			}
		} else {
			Fit(m, int(bufsize), tcp)
			if m.Truncated {
				promErrorCount.WithLabelValues("truncated").Inc()
			}
		}
		s.rcache.InsertMessage(cache.Key(q, tcp), m,remoteIp[0])

		if err := w.WriteMsg(m); err != nil {
			glog.Infof("failure to return reply %q", err)
		}
		metricSizeAndDuration(m, start, tcp)
	}()

	if q.Qclass == dns.ClassCHAOS {
	       // not support
		m.SetReply(req)
		m.SetRcode(req, dns.RcodeServerFailure)
		return
	}

	switch q.Qtype {
	case dns.TypeA, dns.TypeAAAA:
		records, err := s.AddressRecords(q, name, nil, bufsize, false)
		if isEtcdNameError(err, s) {
			s.NameError(m, req)
			return
		}
		m.Answer = append(m.Answer, records...)
	case dns.TypeCNAME:
		records, err := s.CNAMERecords(q, name)
		if isEtcdNameError(err, s) {
			s.NameError(m, req)
			return
		}
		m.Answer = append(m.Answer, records...)

	default:
		 glog.Infof("err, the type %s, we ignore, q name=%s\n", q.Qtype,q.Name)
	}

	if len(m.Answer) == 0 { // NODATA response
		m.Ns = []dns.RR{s.NewSOA()}
		m.Ns[0].Header().Ttl = s.config.MinTtl
	}
}

func (s *server) AddressRecords(q dns.Question, name string, previousRecords []dns.RR, bufsize uint16, both bool) (records []dns.RR, err error) {
	services, err := s.backend.Records(name, false)
	if err != nil {
		glog.Infof("AddressRecords err  %s q name=%s\n", err.Error(),q.Name)
		return nil, err
	}

	services = msg.Group(services)

	for _, serv := range services {
		ip := net.ParseIP(serv.Host)
		switch {
		case ip == nil:
			// Try to resolve as CNAME if it's not an IP, but only if we don't create loops.
			if q.Name == dns.Fqdn(serv.Host) {
				// x CNAME x is a direct loop, don't add those
				continue
			}

			newRecord := serv.NewCNAME(q.Name, dns.Fqdn(serv.Host))
			if len(previousRecords) > 0 {
				glog.Infof("CNAME lookup limit of 1 exceeded for %s", newRecord)
				// don't add it, and just continue
				continue
			}
			if s.isDuplicateCNAME(newRecord, previousRecords) {
				glog.Infof("CNAME loop detected for record %s", newRecord)
				continue
			}

			nextRecords, err := s.AddressRecords(dns.Question{Name: dns.Fqdn(serv.Host), Qtype: q.Qtype, Qclass: q.Qclass},
				strings.ToLower(dns.Fqdn(serv.Host)), append(previousRecords, newRecord), bufsize, both)
			if err == nil {
				// Only have we found something we should add the CNAME and the IP addresses.
				if len(nextRecords) > 0 {
					records = append(records, newRecord) // we do not need the record just return the ip
					records = append(records, nextRecords...)
				}
				continue
			}
			// This means we can not complete the CNAME, try to look else where.
			target := newRecord.Target
			if dns.IsSubDomain(s.config.Domain, target) {
				// We should already have found it
				continue
			}
			m1, e1 := s.Lookup(target, q.Qtype, bufsize)
			if e1 != nil {
				glog.Infof("incomplete CNAME chain: %s", e1)
				continue
			}
			// Len(m1.Answer) > 0 here is well?
			records = append(records, newRecord)
			records = append(records, m1.Answer...)
			continue
		case ip.To4() != nil && (q.Qtype == dns.TypeA || both):
			records = append(records, serv.NewA(q.Name, ip.To4()))
		case ip.To4() == nil && (q.Qtype == dns.TypeAAAA || both):
			records = append(records, serv.NewAAAA(q.Name, ip.To16()))
		}
	}
	return records, nil
}

func (s *server) CNAMERecords(q dns.Question, name string) (records []dns.RR, err error) {
	services, err := s.backend.Records(name, true)
	if err != nil {
		return nil, err
	}

	services = msg.Group(services)

	if len(services) > 0 {
		serv := services[0]
		if ip := net.ParseIP(serv.Host); ip == nil {
			records = append(records, serv.NewCNAME(q.Name, dns.Fqdn(serv.Host)))
		}
	}
	return records, nil
}

// SOA returns a SOA record for this HADES instance.
func (s *server) NewSOA() dns.RR {
	return &dns.SOA{Hdr: dns.RR_Header{Name: s.config.Domain, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: s.config.Ttl},
		Ns:      appendDomain("ns.dns", s.config.Domain),
		Mbox:    "none",
		Serial:  uint32(time.Now().Truncate(time.Hour).Unix()),
		Refresh: 28800,
		Retry:   7200,
		Expire:  604800,
		Minttl:  s.config.MinTtl,
	}
}

func (s *server) isDuplicateCNAME(r *dns.CNAME, records []dns.RR) bool {
	for _, rec := range records {
		if v, ok := rec.(*dns.CNAME); ok {
			if v.Target == r.Target {
				return true
			}
		}
	}
	return false
}

func (s *server) NameError(m, req *dns.Msg) {
	m.SetRcode(req, dns.RcodeNameError)
	m.Ns = []dns.RR{s.NewSOA()}
	m.Ns[0].Header().Ttl = s.config.MinTtl
	promErrorCount.WithLabelValues("nxdomain")
}

func (s *server) NoDataError(m, req *dns.Msg) {
	m.SetRcode(req, dns.RcodeSuccess)
	m.Ns = []dns.RR{s.NewSOA()}
	m.Ns[0].Header().Ttl = s.config.MinTtl
	promErrorCount.WithLabelValues("nodata")
}

func (s *server) ServerFailure(m, req *dns.Msg) {
	m.SetRcode(req, dns.RcodeServerFailure)
	promErrorCount.WithLabelValues("servfail")
}

func (s *server) dedup(m *dns.Msg) *dns.Msg {
	// Answer section
	ma := make(map[string]dns.RR)
	for _, a := range m.Answer {
		// Or use Pack()... Think this function also could be placed in go dns.
		s1 := a.Header().Name
		s1 += strconv.Itoa(int(a.Header().Class))
		s1 += strconv.Itoa(int(a.Header().Rrtype))
		// there can only be one CNAME for an ownername
		if a.Header().Rrtype == dns.TypeCNAME {
			ma[s1] = a
			continue
		}
		for i := 1; i <= dns.NumField(a); i++ {
			s1 += dns.Field(a, i)
		}
		ma[s1] = a
	}
	// Only is our map is smaller than the #RR in the answer section we should reset the RRs
	// in the section it self
	if len(ma) < len(m.Answer) {
		i := 0
		for _, v := range ma {
			m.Answer[i] = v
			i++
		}
		m.Answer = m.Answer[:len(ma)]
	}

	// Additional section
	me := make(map[string]dns.RR)
	for _, e := range m.Extra {
		s1 := e.Header().Name
		s1 += strconv.Itoa(int(e.Header().Class))
		s1 += strconv.Itoa(int(e.Header().Rrtype))
		// there can only be one CNAME for an ownername
		if e.Header().Rrtype == dns.TypeCNAME {
			me[s1] = e
			continue
		}
		for i := 1; i <= dns.NumField(e); i++ {
			s1 += dns.Field(e, i)
		}
		me[s1] = e
	}

	if len(me) < len(m.Extra) {
		i := 0
		for _, v := range me {
			m.Extra[i] = v
			i++
		}
		m.Extra = m.Extra[:len(me)]
	}

	return m
}

// isTCP returns true if the client is connecting over TCP.
func isTCP(w dns.ResponseWriter) bool {
	_, ok := w.RemoteAddr().(*net.TCPAddr)
	return ok
}

// etcNameError return a NameError to the client if the error
// returned from etcd has ErrorCode == 100.
func isEtcdNameError(err error, s *server) bool {
	if e, ok := err.(*etcd.EtcdError); ok {
		if e.ErrorCode == 100 {
			return true
		}
	}
	if err != nil {
		glog.Infof("error from backend: %s", err)
	}
	return false
}
