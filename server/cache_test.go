// Copyright (c) 2014 The HADES Authors. All rights reserved.
// Use of this source code is governed by The MIT License (MIT) that can be
// found in the LICENSE file.

package server

import (
	"testing"

	"github.com/miekg/dns"
	"github.com/ipdcode/hades/cache"
)

func TestFit(t *testing.T) {
	m := new(dns.Msg)
	m.SetQuestion("hades.nl", dns.TypeA)

	rr, _ := dns.NewRR("www.hades.nl. IN SRV 10 10 8080 blaat.hades.nl.")
	for i := 0; i < 101; i++ {
		m.Answer = append(m.Answer, rr)
	}
	// Uncompresses length is now 4424. Try trimming this to 1927
	Fit(m, 1927, true)

	if m.Len() > 1927 {
		t.Fatalf("failed to fix message, expected < %d, got %d", 1927, m.Len())
	}
}

func TestCacheTruncated(t *testing.T) {
	s := newTestServer(t, true)
	remoteIp := "192.168.0.1"
	m := &dns.Msg{}
	m.SetQuestion("hades.test.", dns.TypeA)
	m.Truncated = true
	s.rcache.InsertMessage(cache.Key(m.Question[0], false), m,remoteIp)

	// Now asking for this should result in a non-truncated answer.
	resp, _ := dns.Exchange(m, "127.0.0.1:"+StrPort)
	if resp.Truncated {
		t.Fatal("truncated bit should be false")
	}
}
