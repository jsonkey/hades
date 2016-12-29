// Copyright (c) 2014 The HADES Authors. All rights reserved.
// Use of this source code is governed by The MIT License (MIT) that can be
// found in the LICENSE file.

package server

// etcd needs to be running on http://127.0.0.1:4001

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"
	"github.com/coreos/go-etcd/etcd"
	"github.com/miekg/dns"
	backendetcd "github.com/ipdcode/hades/backends/etcd"
	"github.com/ipdcode/hades/cache"
	"github.com/ipdcode/hades/msg"
)

// Keep global port counter that increments with 10 for each
// new call to newTestServer. The dns server is started on port 'Port'.
var (
	Port        = 9400
	StrPort     = "9400" // string equivalent of Port
	metricsDone = false
)

func addService(t *testing.T, s *server, k string, ttl uint64, m *msg.Service) {
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := msg.PathWithWildcard(k)

	_, err = s.backend.(*backendetcd.Backend).Client().Create(path, string(b), ttl)
	if err != nil {
		t.Fatal(err)
	}
}

func delService(t *testing.T, s *server, k string) {
	path, _ := msg.PathWithWildcard(k)
	_, err := s.backend.(*backendetcd.Backend).Client().Delete(path, false)
	if err != nil {
		t.Fatal(err)
	}
}

func newTestServer(t *testing.T, c bool) *server {
	Port += 10
	StrPort = strconv.Itoa(Port)
	s := new(server)
	client := etcd.NewClient([]string{"http://127.0.0.1:2379"})

	s.group = new(sync.WaitGroup)
	s.rcache = cache.New(100, 0)
	if c {
		s.rcache = cache.New(100, 60) // 100 items, 60s ttl
	}
	s.config = new(Config)
	s.config.Domain = "hades.test."
	s.config.DnsAddr = "127.0.0.1:" + StrPort
	s.config.Nameservers = []string{"8.8.4.4:53"}
	SetDefaults(s.config)
	s.config.Priority = 10
	s.config.RCacheTtl = RCacheTtl
	s.config.Ttl = 3600
	s.config.Ndots = 2

	if !metricsDone {
		metricsDone = true
		Metrics("12300")
	}

	s.dnsUDPclient = &dns.Client{Net: "udp", ReadTimeout: 2 * s.config.ReadTimeout, WriteTimeout: 2 * s.config.ReadTimeout, SingleInflight: true}
	s.dnsTCPclient = &dns.Client{Net: "tcp", ReadTimeout: 2 * s.config.ReadTimeout, WriteTimeout: 2 * s.config.ReadTimeout, SingleInflight: true}

	s.backend = backendetcd.NewBackend(client, &backendetcd.Config{
		Ttl:      s.config.Ttl,
		Priority: s.config.Priority,
	})

	go s.Run()
	// Yeah, yeah, should do a proper fix.
	time.Sleep(500 * time.Millisecond)
	return s
}

func TestDNSForward(t *testing.T) {
	s := newTestServer(t, false)
	defer s.Stop()

	c := new(dns.Client)
	m := new(dns.Msg)
	m.SetQuestion("www.example.com.", dns.TypeA)
	resp, _, err := c.Exchange(m, "127.0.0.1:"+StrPort)
	if err != nil {
		// try twice
		resp, _, err = c.Exchange(m, "127.0.0.1:"+StrPort)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(resp.Answer) == 0 || resp.Rcode != dns.RcodeSuccess {
		t.Fatal("answer expected to have A records or rcode not equal to RcodeSuccess")
	}
}

func anserAdjust( Answer []dns.RR) int{
	typeA := false
	typeCname := false
	for _, r := range Answer {
		 switch r.(type) {
		 	case *dns.A: typeA = true
		 	case *dns.CNAME: typeCname = true
		 }
	}
	n := 0
        if typeA && typeCname{
		n =2
	}else if typeA{
		n =1
	}else{
		n= len(Answer)
	}
	return n

}
func typeACnameCheck( valAnser string ,Answer []dns.RR) bool{
	for _, r := range Answer {
		switch r.(type) {
		 	case *dns.A:
				valA := r.(*dns.A)
				val := valA.A.String()
			        if val == valAnser{
					return true
				}
		 	case *dns.CNAME:
				valCname := r.(*dns.CNAME)
			        if valAnser == valCname.Target{
					return true
				}
		 }
	}
	return false
}
func TestDNS(t *testing.T) {
	s := newTestServer(t, false)
	defer s.Stop()

	for _, serv := range services {
		addService(t, s, serv.Key, 0, serv)
		defer delService(t, s, serv.Key)
	}
	c := new(dns.Client)
	for _, tc := range dnsTestCases {
		m := new(dns.Msg)
		m.SetQuestion(tc.Qname, tc.Qtype)
		resp, _, err := c.Exchange(m, "127.0.0.1:"+StrPort)
		if err != nil {
			// try twice, be more resilent against remote lookups
			// timing out.
			resp, _, err = c.Exchange(m, "127.0.0.1:"+StrPort)
			if err != nil {
				t.Logf("\n ****** \n: %s \n ******** \n",err.Error())
				t.Fatalf("failing: %s: %s\n", m.String(), err.Error())

			}
		}
		fatal := false
		defer func() {
			if fatal {
				t.Logf("question: %s\n", m.Question[0].String())
				t.Logf("%s\n", resp)
			}
		}()
		if resp.Rcode != tc.Rcode {
			fatal = true
			t.Fatalf("rcode is %q, expected %q", dns.RcodeToString[resp.Rcode], dns.RcodeToString[tc.Rcode])
		}
		if len(resp.Answer) != anserAdjust(tc.Answer) {
			fatal = true
			t.Fatalf("answer for %q contained %d results, %d expected", tc.Qname, len(resp.Answer), len(tc.Answer))
		}
		for i, a := range resp.Answer {
			if a.Header().Name != tc.Answer[i].Header().Name {
				fatal = true
				t.Fatalf("answer %d should have a Header Name of %q, but has %q", i, tc.Answer[i].Header().Name, a.Header().Name)
			}
			if a.Header().Ttl != tc.Answer[i].Header().Ttl {
				fatal = true
				t.Fatalf("Answer %d should have a Header TTL of %d, but has %d", i, tc.Answer[i].Header().Ttl, a.Header().Ttl)
			}
			if a.Header().Rrtype != tc.Answer[i].Header().Rrtype {
				fatal = true
				t.Fatalf("answer %d should have a header response type of %d, but has %d", i, tc.Answer[i].Header().Rrtype, a.Header().Rrtype)
			}
			switch x := a.(type) {
			case *dns.A:
				if ! typeACnameCheck(x.A.String(),tc.Answer ) {
					fatal = true
					t.Fatalf("answer %d should have a Address of %q, but has %q", i, tc.Answer[i].(*dns.A).A.String(), x.A.String())
				}
			case *dns.AAAA:
				if x.AAAA.String() != tc.Answer[i].(*dns.AAAA).AAAA.String() {
					fatal = true
					t.Fatalf("answer %d should have a Address of %q, but has %q", i, tc.Answer[i].(*dns.AAAA).AAAA.String(), x.AAAA.String())
				}

			case *dns.CNAME:
				tt := tc.Answer[i].(*dns.CNAME)
				if ! typeACnameCheck(x.Target,tc.Answer ) {
					fatal = true
					t.Fatalf("CNAME target should be %q, but is %q", x.Target, tt.Target)
				}

			}
		}
	}
}

type dnsTestCase struct {
	Qname  string
	Qtype  uint16
	Rcode  int
	Answer []dns.RR
}

// Note the key is encoded as DNS name, while in "reality" it is a etcd path.
var services = []*msg.Service{
	// type A
	{Host: "10.0.0.1", Key: "a.ns.dns.hades.test."},
	{Host: "10.0.0.2", Key: "b.ns.dns.hades.test."},
        // cname
	{Host: "a.ns.dns.hades.test.", Key: "1.cname.hades.test"},

}

var dnsTestCases = []dnsTestCase{
	// type A  Test
	{
		Qname: "a.ns.dns.hades.test.", Qtype: dns.TypeA,
		Answer: []dns.RR{
			newA("a.ns.dns.hades.test. 3600 A 10.0.0.1"),
		},
	},
	{
		Qname: "b.ns.dns.hades.test.", Qtype: dns.TypeA,
		Answer: []dns.RR{
			newA("b.ns.dns.hades.test. 3600 A 10.0.0.2"),
		},
	},

	// CNAME Test
	{
		Qname: "1.cname.hades.test.", Qtype: dns.TypeA,
		Answer: []dns.RR{
			newCNAME("1.cname.hades.test. 3600 CNAME a.ns.dns.hades.test."),
			newA("a.ns.dns.hades.test. 3600 A 10.0.0.1"),
		},
	},
}

func newA(rr string) *dns.A           { r, _ := dns.NewRR(rr); return r.(*dns.A) }
func newCNAME(rr string) *dns.CNAME   { r, _ := dns.NewRR(rr); return r.(*dns.CNAME) }


func BenchmarkDNSSingleCache(b *testing.B) {
	b.StopTimer()
	t := new(testing.T)
	s := newTestServer(t, true)
	defer s.Stop()

	serv := services[0]
	addService(t, s, serv.Key, 0, serv)
	defer delService(t, s, serv.Key)

	c := new(dns.Client)
	tc := dnsTestCases[0]
	m := new(dns.Msg)
	m.SetQuestion(tc.Qname, tc.Qtype)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		c.Exchange(m, "127.0.0.1:"+StrPort)
	}
}

func BenchmarkDNSSingleNoCache(b *testing.B) {
	b.StopTimer()
	t := new(testing.T)
	s := newTestServer(t, false)
	defer s.Stop()

	serv := services[0]
	addService(t, s, serv.Key, 0, serv)
	defer delService(t, s, serv.Key)

	c := new(dns.Client)
	tc := dnsTestCases[0]
	m := new(dns.Msg)
	m.SetQuestion(tc.Qname, tc.Qtype)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		c.Exchange(m, "127.0.0.1:"+StrPort)
	}
}
