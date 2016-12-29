// Copyright (c) 2014 The HADES Authors. All rights reserved.

package server

import (
	"net"
	"os"
	"strings"
	"time"

	"github.com/miekg/dns"
)

const (
	RCacheCapacity = 100000
	RCacheTtl      = 60
)

// Config provides options to the HADES resolver.
type Config struct {
	// The ip:port HADES should be listening on for incoming DNS requests.
	DnsAddr string `json:"dns_addr,omitempty"`
	// The domain HADES is authoritative for, defaults to hades.local.
	Domain string `json:"domain,omitempty"`
	// The ip-monitor-path  is watched to check if the ip is avaliable, defaults to /hades/monitor/status/
	IpMonitorPath string `json:"ip-monitor-path,omitempty"`
	// Domain pointing to a key where service info is stored when being queried

	// List of ip:port, seperated by commas of recursive nameservers to forward queries to.
	Nameservers []string `json:"nameservers,omitempty"`

	ReadTimeout time.Duration `json:"read_timeout,omitempty"`
	// Default priority on SRV records when none is given. Defaults to 10.
	Priority uint16 `json:"priority"`
	// Default TTL, in seconds, when none is given in etcd. Defaults to 3600.
	Ttl uint32 `json:"ttl,omitempty"`
	// Minimum TTL, in seconds, for NXDOMAIN responses. Defaults to 300.
	MinTtl uint32 `json:"min_ttl,omitempty"`
	// RCache, capacity of response cache in resource records stored.
	RCache int `json:"rcache,omitempty"`
	// RCacheTtl, how long to cache in seconds.
	RCacheTtl int `json:"rcache_ttl,omitempty"`

	// How many labels a name should have before we allow forwarding. Default to 2.
	Ndots int `json:"ndot,omitempty"`

	MetricsPort string `json:"metrics_port,omitempty"`
}

func SetDefaults(config *Config) error {
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 2 * time.Second
	}
	if config.DnsAddr == "" {
		config.DnsAddr = "127.0.0.1:53"
	}
	if config.Domain == "" {
		config.Domain = "hades.local."
	}
	if config.MinTtl == 0 {
		config.MinTtl = 60
	}
	if config.Ttl == 0 {
		config.Ttl = 3600
	}
	if config.Priority == 0 {
		config.Priority = 10
	}
	if config.RCache < 0 {
		config.RCache = 0
	}
	if config.RCacheTtl == 0 {
		config.RCacheTtl = RCacheTtl
	}
	if config.Ndots <= 0 {
		config.Ndots = 2
	}

	if len(config.Nameservers) == 0 {
		c, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if !os.IsNotExist(err) {
			if err != nil {
				return err
			}
			for _, s := range c.Servers {
				config.Nameservers = append(config.Nameservers, net.JoinHostPort(s, c.Port))
			}
		}
	}
	config.Domain = dns.Fqdn(strings.ToLower(config.Domain))

	return nil
}

func appendDomain(s1, s2 string) string {
	if len(s2) > 0 && s2[0] == '.' {
		return s1 + s2
	}
	return s1 + "." + s2
}
