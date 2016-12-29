# Hades
*Version 1.0.1*

Hades is a distributed service for announcement and discovery of services built
on top of [etcd](https://github.com/coreos/etcd). It utilizes DNS queries to
discover available services. 
Hades is used as internal dns server for k8s cluster. hades-kubeapi will monitor 
the services in k8s cluster,when the service is created and has been assigend a vip, 
the user(docker)in cluster can access the service with the domain.
When the domain has mutiple ips, the hades will radom choose one actived for the user, 
it seems like loadbalance.
Also the hades offer "session persistence", that means we qury one domain from one user ip,
then the user accessthe domain later, the user will get the same service ip.   

## Components
* `hades`: the main service to offer dns query.
* `hades-kubeapi`: monitor the changes of k8s services, and record the change in the etcd. It offered the
   original data for hades, meanwhille hades-kubeapi offers the resful api for users to maintain domain records.
* `hades-apicmd`: it is a shell cmd for user to query\update domain record, it is based on hades-kubeapi.
 

## Setup / Install

Download/compile and run etcd. See the documentation for etcd at <https://github.com/coreos/etcd>.

Then get and compile Hades:

    go get github.com/ipdcode/hades
    cd $GOPATH/src/github.com/ipdcode/hades
    go build -v
	cd $GOPATH/src/github.com/ipdcode/hades/hades-kubeapi
	go build -v
	...


## Configuration

### hades

* `addr`: IP:port on which Hades should listen, defaults to `127.0.0.1:53`.
* `domain`: domain for which Hades is authoritative, defaults to `hades.local.`.
* `machines`: machine address(es) running etcd.
* `metrics-port`: metrics-port, default .
* `nameservers`: fnameserver address(es) to forward (non-local) queries to e.g. 8.8.8.8:53,8.8.4.4:53,
   This defaults to the servers listed in `/etc/resolv.conf`. 
* `ip-monitor-path`: the ips to check available, default "/hades/monitor/status/".
* `read_timeout`: network read timeout, for DNS and talking with etcd, default 2s.
* `version`: Print version information.
* `rcache`: the capacity of the response cache, defaults to 0 messages if not set.
* `rcache_ttl`: the TTL of the response cache, defaults to 60 if not set.
* `logtostderr`: log to standard error instead of files.
* `log_dir`: log files directory.
* `v`: log level for V logs.


### hades-kubeapi

* `config-file`: read configs from the file, default "/etc/hades/hades-api.conf".
* `version`: Print version information.
* `logtostderr`: log to standard error instead of files.
* `log_dir`: log files directory.
* `v`: log level for V logs.

The config file is  like ./hades-kubeapi/hades-api.conf

* `etcd-server`: etcd server address.
* `kube-master-url`: k8s api-server address.
* `api-address`: api server address for users
* `hades-auth`: it is a token for simple authentication

### hades-scanner

* `config-file`: read configs from the file, default "/etc/hades/hades-scanner.conf".

the config file like this:
...
[General]
core = 0
enable-check = true
hostname = hostname1
log-dir = /export/log/hades
log-level = 100
heartbeat-interval = 30
[Check]
check-timeout = 2
check-interval = 10
scann-ports = 22, 80, 8080
enable-icmp = true
ping-timeout = 1000
ping-count = 2
[Etcd]
etcd-machine = http://127.0.0.1:2379
tls-key =
tls-pem =
ca-cert =
status-path = /hades/monitor/status
report-path = /hades/monitor/report
heart-path = /hades/monitor/heart
...

### hades-schedule

* `config-file`: read configs from the file, default "/etc/hades/hades-schedule.conf".

the config file like this:
...
[General]
schedule-interval = 60
agent-downtime = 60
log-dir = /export/log/hades
log-level = 100
hostname = hostname1
force-lock-time = 1800

[Etcd]
etcd-machine = http://127.0.0.1:2379
status-path = /hades/monitor/status
report-path = /hades/monitor/report
heart-path = /hades/monitor/heart
lock-path = /hades/monitor/lock
...

### hades-apicmd

* `addr`: hades api address,such as 127.0.0.1:9001 or form env(HADES_API_ADDR).
* `domain`: the domain to show
* `show`: show one domain
* `list`: show all domains

### SSL Usage and Authentication with Client Certificates

In order to connect to an SSL-secured etcd, you will at least need to set
ETCD_CACERT to be the public key of the Certificate Authority which signed the
server certificate.

If the SSL-secured etcd expects client certificates to authorize connections,
you also need to set ETCD_TLSKEY to the *private* key of the client, and
ETCD_TLSPEM to the *public* key of the client.


## Testing

### hades-apicmd
    export HADES_API_ADDR=127.0.0.1:9001
    export HADES_API_TOKEN=abcdef123456789
	
    hades-apicmd -list	
	domain:                      qiyf-nginx-5.default.svc.hades.local       val: { type:A  ips:[192.168.19.113] }
	domain:                      qiyf-nginx-9.default.svc.hades.local       val: { type:A  ips:[192.168.19.120] }
	domain:                      qiyf-nginx-4.default.svc.hades.local       val: { type:A  ips:[192.168.19.114] }
	domain:                      qiyf-nginx-6.default.svc.hades.local       val: { type:A  ips:[192.168.19.116] }
	domain:                     qiyf-nginx-14.default.svc.hades.local       val: { type:A  ips:[192.168.19.125] }
	domain:                     qiyf-nginx-27.default.svc.hades.local       val: { type:A  ips:[192.168.19.147] }
	domain:                     qiyf-nginx-15.default.svc.hades.local       val: { type:A  ips:[192.168.19.126] }
	domain:                     qiyf-nginx-19.default.svc.hades.local       val: { type:A  ips:[192.168.19.13] }
	domain:                     qiyf-nginx-30.default.svc.hades.local       val: { type:A  ips:[192.168.19.148] }
	domain:                      qiyf-nginx-1.default.svc.hades.local       val: { type:A  ips:[192.168.19.115] }
	domain:                     qiyf-nginx-10.default.svc.hades.local       val: { type:A  ips:[192.168.19.121] }
	domain:                     qiyf-nginx-25.default.svc.hades.local       val: { type:A  ips:[192.168.19.146] }

	
	hades-apicmd -show qiyf-nginx-5.default
    domain:                      qiyf-nginx-5.default.svc.hades.local       val: { type:A  ips:[192.168.19.113] }
	
### hades-kubeapi
    we use curl to test the user api.
####  typeA
	% curl -H "Content-Type:application/json;charset=UTF-8"  -H "token:abcdef123456789" -H "domain:cctv1" -X POST -d '{"type":"A","ips":["192.168.10.1","192.168.10.2","192.168.10.3"]}'  http://127.0.0.1:9001/hades
    OK
#### typeCname
	% curl -H "Content-Type:application/json;charset=UTF-8"  -H "token:abcdef123456789"-H "domain:cctv1.user.hades.local" -X POST -d '{"type":"cname","alias":"tv1"}' http://127.0.0.1:9001/hades
   OK

### hades

####  typeA
	% nslookup qiyf-nginx-5.default.svc.hades.local 127.0.0.1
	Server:         127.0.0.1
	Address:        127.0.0.1#53

	Name:   qiyf-nginx-5.default.svc.hades.local
	Address: 192.168.19.113

	if the domain have more than one ip, hades will return a radom one.

	% nslookup cctv1.user.hades.local 127.0.0.1
	Server:         127.0.0.1
	Address:        127.0.0.1#53

	Name:   cctv1.user.hades.local
	Address: 192.168.10.3

	 
####  typeCname
	% nslookup tv1.user.hades.local 127.0.0.1
	Server:         127.0.0.1
	Address:        127.0.0.1#53

	tv1.user.hades.local    canonical name = cctv1.user.hades.local.
	Name:   cctv1.user.hades.local
	Address: 192.168.10.3
	
####  monitor
	 If the domain may have multiple ips, then dns-scanner is used to monitor the ips behand the domain. 
	 When the service is not reachable, dns-scanner will change the status of the ip. And the hades will monitor the ip staus, 
	 when it comes down, hades will choose a good one.
	 
	 cctv1.user.hades.local    ips[192.168.10.1,192.168.10.2,192.168.10.3]
	 
	% nslookup cctv1.user.hades.local 127.0.0.1
	Server:         127.0.0.1
	Address:        127.0.0.1#53

	Name:   cctv1.user.hades.local
	Address: 192.168.10.3
	
	% etcdctl get /hades/monitor/status/192.168.10.3
	{"status":"DOWN"}

	% nslookup cctv1.user.hades.local 127.0.0.1
	Server:         127.0.0.1
	Address:        127.0.0.1#53

	Name:   cctv1.user.hades.local
	Address: 192.168.10.1
	
	we query the domain cctv1.user.hades.local form hades we get the ip 192.168.10.3, then we shut down the servic, we query the domain again
	we get the ip 192.168.10.1.


## Future

### improve the performance of UDP packets (DNS use UDP)
    Help Hades (DNS) services improve throughput performace with DPDK technology
