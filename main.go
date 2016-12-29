// Copyright (c) 2016 The HADES Authors. All rights reserved.


package main

import (

	"flag"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"
	"github.com/coreos/go-etcd/etcd"
	backendetcd "github.com/ipdcode/hades/backends/etcd"
	"github.com/ipdcode/hades/msg"
	"github.com/ipdcode/hades/server"
	"github.com/golang/glog"
)

const (
	glogFlushPeriod       = 5 * time.Second
	syncIpStatusPeriod    = 60 * time.Second
	HadesVersion          = "1.0.1"
)
var (
	tlskey     = ""
	tlspem     = ""
	cacert     = ""
	config     = &server.Config{ReadTimeout: 0, Domain: "", DnsAddr: ""}
	nameserver = ""
	machine    = ""
	version    = false

)

func init() {
	flag.StringVar(&config.Domain, "domain",  "hades.local.", "domain to anchor requests")
	flag.StringVar(&config.DnsAddr, "addr",  "127.0.0.1:53", "ip:port mode , the addr to be bind)")
	flag.StringVar(&config.MetricsPort, "metrics-port",  "", "the metrics port")

	flag.StringVar(&config.IpMonitorPath, "ip-monitor-path", "/hades/monitor/status/", "the ips to check available")
	flag.StringVar(&nameserver, "nameservers", "", "nameserver address(es) to forward (non-local) queries to e.g. 8.8.8.8:53,8.8.4.4:53")
	flag.StringVar(&machine, "machines", "", "machine address(es) running etcd")
	flag.StringVar(&tlskey, "tls-key",  "", "TLS Private Key path")
	flag.StringVar(&tlspem, "tls-pem", "", "X509 Certificate")
	flag.BoolVar(&version, "version",false, "Print version information and quit")
	flag.StringVar(&cacert, "ca-cert",  "", "CA Certificate")
	flag.DurationVar(&config.ReadTimeout, "rtimeout", 2*time.Second, "read timeout")

	flag.IntVar(&config.RCache, "rcache", server.RCacheCapacity, "capacity of the response cache")
	flag.IntVar(&config.RCacheTtl, "rcache-ttl", server.RCacheTtl, "TTL of the response cache")
}

func glogFlush(period time.Duration) {
	for range time.Tick(period) {
		glog.Flush()
        }
}
func main() {
	flag.Parse()
	if version{
		fmt.Printf("%s\n",HadesVersion)
		return
	}
	go glogFlush(glogFlushPeriod)
	defer glog.Flush()

	machines := strings.Split(machine, ",")
	client := newEtcdClient(machines, tlspem, tlskey, cacert)

	if nameserver != "" {
		for _, hostPort := range strings.Split(nameserver, ",") {
			if err := validateHostPort(hostPort); err != nil {
				glog.Fatalf("hades: nameserver is invalid: %s", err)
			}
			config.Nameservers = append(config.Nameservers, hostPort)
		}
	}
	if err := validateHostPort(config.DnsAddr); err != nil {
		glog.Fatalf("hades: addr is invalid: %s", err)
	}

	if err := server.SetDefaults(config); err != nil {
		glog.Fatalf("hades: defaults could not be set from /etc/resolv.conf: %v", err)
	}

	backend := backendetcd.NewBackend(client, &backendetcd.Config{
		Ttl:      config.Ttl,
		Priority: config.Priority,
	})
	s := server.New(backend, config)

	go func() {
		recv := make(chan *etcd.Response)
		go client.Watch(msg.Path(config.Domain), 0, true, recv, nil)
		duration := 1 * time.Second
		for {
			select {
			case n := <-recv:
				if n != nil {
					s.UpdateRcache(n)
					duration = 1 * time.Second // reset
				} else {

					glog.Infof("hades: etcd machine cluster update failed, sleeping %s + ~3s", duration)
					time.Sleep(duration + (time.Duration(rand.Float32() * 3e9))) // Add some random.
					duration *= 2
					if duration > 32*time.Second {
						duration = 32 * time.Second
					}
				}
			}
		}
	}()

	server.Metrics(config.MetricsPort)

        // watch ip status
	go func() {
		recv := make(chan *etcd.Response)
		go client.Watch(config.IpMonitorPath, 0, true, recv, nil)
		duration := 1 * time.Second
		for {
			select {
			case n := <-recv:
				if n != nil {
					s.UpdateHostStatus(n)
					duration = 1 * time.Second // reset
				} else {

					glog.Infof("hades: etcd machine cluster update failed, sleeping %s + ~3s", duration)
					time.Sleep(duration + (time.Duration(rand.Float32() * 3e9))) // Add some random.
					duration *= 2
					if duration > 32*time.Second {
						duration = 32 * time.Second
					}
				}
			}
		}
	}()
	// before server run we get the active ips
	s.SyncHadesHostStatus()

	go s.HostStatusSync(syncIpStatusPeriod)

	if err := s.Run(); err != nil {
		glog.Fatalf("hades: %s", err)
	}
}

func validateHostPort(hostPort string) error {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil {
		return fmt.Errorf("bad IP address: %s", host)
	}

	if p, _ := strconv.Atoi(port); p < 1 || p > 65535 {
		return fmt.Errorf("bad port number %s", port)
	}
	return nil
}

func newEtcdClient(machines []string, tlsCert, tlsKey, tlsCACert string) (client *etcd.Client) {
	// set default if not specified in env
	if len(machines) == 1 && machines[0] == "" {
		machines[0] = "http://127.0.0.1:4001"
	}
	if strings.HasPrefix(machines[0], "https://") {
		var err error
		if client, err = etcd.NewTLSClient(machines, tlsCert, tlsKey, tlsCACert); err != nil {
			glog.Fatalf("hades: failure to connect: %s", err)
		}
		return client
	}
	return etcd.NewClient(machines)
}

