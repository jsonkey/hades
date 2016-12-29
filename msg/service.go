// Copyright (c) 2014 The HADES Authors. All rights reserved.
// Use of this source code is governed by The MIT License (MIT) that can be
// found in the LICENSE file.

package msg

import (
	"net"
	"path"
	"strings"

	"github.com/miekg/dns"
)

// PathPrefix is the prefix used to store HADES data in the backend.

var PathPrefix string = "hades"

// This *is* the rdata from a SRV record, but with a twist.
// Host (Target in SRV) must be a domain name, but if it looks like an IP
// address (4/6), we will treat it like an IP address.
type Service struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Weight   int    `json:"weight,omitempty"`
	Text     string `json:"text,omitempty"`
	Mail     bool   `json:"mail,omitempty"` // Be an MX record. Priority becomes Preference.
	Ttl      uint32 `json:"ttl,omitempty"`
	TargetStrip int `json:"targetstrip,omitempty"`

	Group string `json:"group,omitempty"`

	// Etcd key where we found this service and ignored from json un-/marshalling
	Key string `json:"-"`
      // dns type
	Dnstype     string  `json:"type,omitempty"`
}

// NewA returns a new A record based on the Service.
func (s *Service) NewA(name string, ip net.IP) *dns.A {
	return &dns.A{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: s.Ttl}, A: ip}
}

// NewAAAA returns a new AAAA record based on the Service.
func (s *Service) NewAAAA(name string, ip net.IP) *dns.AAAA {
	return &dns.AAAA{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: s.Ttl}, AAAA: ip}
}

// NewCNAME returns a new CNAME record based on the Service.
func (s *Service) NewCNAME(name string, target string) *dns.CNAME {
	return &dns.CNAME{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: s.Ttl}, Target: target}
}

func PathWithWildcard(s string) (string, bool) {
	l := dns.SplitDomainName(s)
	for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
		l[i], l[j] = l[j], l[i]
	}
	for i, k := range l {
		if k == "*" || k == "any" {
			return path.Join(append([]string{"/" + PathPrefix + "/"}, l[:i]...)...), true
		}
	}
	return path.Join(append([]string{"/" + PathPrefix + "/"}, l...)...), false
}

// Path converts a domainname to an etcd path. If s looks like service.staging.hades.local.,
// the resulting key will be /hades/local/hades/staging/service .
func Path(s string) string {
	l := dns.SplitDomainName(s)
	for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
		l[i], l[j] = l[j], l[i]
	}
	return path.Join(append([]string{"/" + PathPrefix + "/"}, l...)...)
}

func Group(sx []Service) []Service {
	if len(sx) == 0 {
		return sx
	}

	group := sx[0].Group
	slashes := strings.Count(sx[0].Key, "/")
	length := make([]int, len(sx))
	for i, s := range sx {
		x := strings.Count(s.Key, "/")
		length[i] = x
		if x < slashes {
			if s.Group == "" {
				break
			}
			slashes = x
			group = s.Group
		}
	}
	if group == "" {
		return sx
	}
	ret := []Service{}

	for i, s := range sx {
		if s.Group == "" {
			ret = append(ret, s)
			continue
		}
		// Disagreement on the same level
		if length[i] == slashes && s.Group != group {
			return sx
		}

		if s.Group == group {
			ret = append(ret, s)
		}
	}
	return ret
}