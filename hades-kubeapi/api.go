package main

import (
	"fmt"
	"net/http"
	"io/ioutil"
	"encoding/json"
	"hash/fnv"
	"net"
	"strings"
	hadesmsg "github.com/ipdcode/hades/msg"
	etcd "github.com/coreos/etcd/client"
	"github.com/golang/glog"
	"github.com/ipdcode/hades/utils"
)

const (
	// A subdomain added to the user specified domain for user definded.
	userSubdomain = "user"
	svcSubdomain = "svc"

	noDomainName = "ERR, no domain name"
	noFindDomainName = "ERR, no find  domain name"
	errDeleteDomainName = "ERR, delete domain name error"
	errSetDomainName    = "ERR, set domain name error"
	errSetDomainNameDupIp    = "ERR, set domain name error  exists:"
        errUpdateDomainName    = "ERR, update domain name error"
	errGetDomainName    = "ERR, get domain name error"
	noAuthorization = "ERR, no Authorization"
	errAuthorization = "ERR, Authorization error"
	noDomainIps = "ERR, no domain ips"
	notIpAddr = "ERR, it is not  IP addr"
	notSupportIpv6 = "ERR, ipv6 tbd"
	notSupportOpsType = "ERR,type not support"
	noOpsType = "ERR, no type offered "
	errBodyUpdate = "ERR, no body update"
	apiSucess  = "OK"
)

type apiService struct {
	AliasDomain string `json:"alias-domain,omitempty"`
	OpsType string `json:"type,omitempty"`
	DomainIps []string `json:"ips,omitempty"`
	DomainAlias  string `json:"alias,omitempty"`
	UpdateMap   map[string]string  `json:"update,omitempty"`
}

// a record to etcd
type apiHadesRecord struct {
	Host    string `json:"host,omitempty"`
	Dnstype string  `json:"type,omitempty"`
}
// a record to etcd for ip monitor
type apiHadesIpMonitor struct {
	Status        string `json:"status,omitempty"`
	Ports         []string  `json:"ports,omitempty"`
}

type hadesApi struct {
	etcdClient   *tools.EtcdOps
	domain  string
	auth  string
	ipMonitorPath string
}
var hapi hadesApi = hadesApi{}


func (a *hadesApi)getHashIp(text string) string {
	h := fnv.New32a()
	h.Write([]byte(text))
	return fmt.Sprintf("%x", h.Sum32())
}

func (a *hadesApi)buildDNSNameString(labels ...string) string {
	var res string
	for _, label := range labels {
		if res == "" {
			res = label
		} else {
			res = fmt.Sprintf("%s.%s", label, res)
		}
	}
	return res
}

func (a *hadesApi) setHadesRecord(name string, ipaddr string,dnsType string) string {
        var svc apiHadesRecord
	svc.Host = ipaddr
	svc.Dnstype = dnsType
	b, err := json.Marshal(svc)
	if err != nil {
		glog.Errorf("%s\n", err.Error())
		return errSetDomainName
	}
	recordValue := string(b)
	glog.V(2).Infof("setHadesRecord:%s",hadesmsg.Path(name))

	err = a.etcdClient.Set(hadesmsg.Path(name), recordValue)

	if err != nil {
		retStr := err.Error()
		if strings.HasPrefix(retStr,etcdKeyalReadyExists){
			glog.Infof("Err: %s\n",retStr)
			return errSetDomainNameDupIp + ipaddr
		}
		glog.Infof("Err: %s\n",retStr)
		return errSetDomainName
	}else{
		return  apiSucess
	}
}
func (a *hadesApi) updateHadesRecord(name string, preVal string,newVal string, dnsType string) string {
        var svc apiHadesRecord
	svc.Host = preVal
	svc.Dnstype = dnsType
	b, err := json.Marshal(svc)
	if err != nil {
		glog.Errorf("%s\n", err.Error())
		return errUpdateDomainName
	}
	recordPre := string(b)

	svc.Host = newVal
	svc.Dnstype = dnsType
	b, err = json.Marshal(svc)
	if err != nil {
		glog.Errorf("%s\n", err.Error())
		return errUpdateDomainName
	}
	recordNew := string(b)

        glog.V(2).Infof("updateHadesRecord :%s",hadesmsg.Path(name))

	err = a.etcdClient.Update(hadesmsg.Path(name), recordNew, recordPre, true)

	if err != nil {
		glog.Errorf("%s\n", err.Error())
		return errUpdateDomainName
	}else{
		return apiSucess
	}
}
func (a *hadesApi) deleteHadesRecord(name string,) string {
	res, err := a.etcdClient.Get(hadesmsg.Path(name), false, true)
	if err != nil {
		glog.Errorf("%s\n", err.Error())
		return noFindDomainName
	}
	glog.V(2).Infof("deleteHadesRecord :%s",hadesmsg.Path(name))

	err = a.etcdClient.Delete(res)

	if err != nil {
		glog.Errorf("%s\n", err.Error())
		return errDeleteDomainName
	}
	return apiSucess
}

func (a *hadesApi) deleteIpMonitorRecord(ip string,) error {
	key := a.ipMonitorPath + ip
	res, err := a.etcdClient.Get(key, false, false)
	if err != nil {
		if !strings.HasPrefix(err.Error(),etcdKeyNotFound){
			return err
		}else{
			return nil
		}

	}
	glog.V(2).Infof("deleteIpMonitorRecord :%s",key)

	err = a.etcdClient.Delete(res)
	return err
}

func (a *hadesApi)  writeIpMonitorRecord(ip string) error {
        var status apiHadesIpMonitor
	status.Status = "UP"
	b, err := json.Marshal(status)
	if err != nil {
		return err
	}
	recordValue := string(b)
	key := a.ipMonitorPath  + ip
	glog.V(2).Infof("writeIpMonitorRecord:%s",key)

	res,err := a.etcdClient.Get(key, false, true)
	// the key exist
	if err == nil{
		glog.V(2).Infof(" writeIpMonitorRecord key:%s exist,val: res.Node.Value:%s",res.Node.Value)
		return nil
	}

	if strings.HasPrefix(err.Error(), etcdKeyNotFound){
		err = a.etcdClient.Set(key, recordValue)
	}

	return err
}

func (a *hadesApi) apiLoopNodes(n *etcd.Nodes,sx map[string]apiService) (err error) {
	var record apiHadesRecord

	for _, n := range *n {
		if n.Dir {
			 err := a.apiLoopNodes(&n.Nodes, sx)
			if err != nil {
				return err
			}
			continue
		}

		if err := json.Unmarshal([]byte(n.Value), &record); err != nil {
			return  err
		}

		switch record.Dnstype{
		case "A":
			key := a.getDomainNameFromKeyA(n.Key)
			if svc, ok := sx[key]; ok {
				svc.DomainIps = append(svc.DomainIps,record.Host )
				sx[key] = svc
				continue
		 	}
			serv := new(apiService)
			serv.DomainIps = append(serv.DomainIps,record.Host )
			serv.OpsType   = "A"
			sx[key] = *serv
		case "CNAME":
			key := a.getDomainNameFromKeyCname(n.Key)
			serv := new(apiService)
			serv.OpsType   = "CNAME"
			serv.AliasDomain = record.Host
			sx[key] = *serv

		default:
			glog.Infof("unknowm type: %s\n",record.Dnstype)
			continue
		}
	}
	return  nil
}

func (a *hadesApi)getDomainNameFromKeyCname(key string) string{
	keys := strings.Split(key,"/")
	domLen := len(keys)-1
	for i, j := 0,domLen; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	domainKey := strings.Join(keys[:domLen-1], ".")
	return domainKey
}

func (a *hadesApi)getDomainNameFromKeyA(key string) string{
	keys := strings.Split(key,"/")
	domLen := len(keys)-1
	for i, j := 0,domLen; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	domainKey := strings.Join(keys[1:domLen-1], ".") // ingoore the first
	return domainKey
}

func (a *hadesApi)doGetHadesRecords(n string,sx map[string]apiService) error {
	r, err := a.etcdClient.Get(hadesmsg.Path(n), true,true)
	if err != nil {
		return err
	}
	switch {
	case r.Node.Dir:
		return a.apiLoopNodes(&r.Node.Nodes,sx)
	default:
		return a.apiLoopNodes(&etcd.Nodes{r.Node},sx)
	}
}

func (a *hadesApi) getHadesRecords(name string,sx map[string]apiService) error {

	n :=""
	if name !=""{
		n = a.buildDNSNameString(a.domain,userSubdomain,name)
		a.doGetHadesRecords(n, sx)
		n = a.buildDNSNameString(a.domain,svcSubdomain,name)
		return a.doGetHadesRecords(n, sx)
	}else{  // show all
		n = a.buildDNSNameString(a.domain)
		return a.doGetHadesRecords(n, sx)
	}
}

func (a *hadesApi)processTypeAPost(s *apiService,domain string )string{
	if len(s.DomainIps) == 0{
		return noDomainIps
	}
	for _, ipaddr := range s.DomainIps{
		ip := net.ParseIP(ipaddr)
		switch {
		case ip == nil:
			return notIpAddr
		case ip.To4() != nil:
			name := a.buildDNSNameString(a.domain,userSubdomain,domain,a.getHashIp(ipaddr))
			ret := a.setHadesRecord(name,ipaddr,"A")
			if ret != apiSucess{
				return ret
			}
			a.writeIpMonitorRecord(ipaddr)
		default:
			return notSupportIpv6
		}
	}
	return apiSucess
}

func (a *hadesApi)processTypeADelete(s *apiService,domain string )string{

	name :=""
	// no ips del all
	if len(s.DomainIps) == 0{
		name = a.buildDNSNameString(a.domain,userSubdomain,domain)
		ret := a.deleteHadesRecord(name)
		return ret
	}
	for _, ipaddr := range s.DomainIps{
		ip := net.ParseIP(ipaddr)
		switch {
		case ip == nil:
			return notIpAddr
		case ip.To4() != nil:
			name = a.buildDNSNameString(a.domain,userSubdomain,domain,a.getHashIp(ipaddr))
			ret := a.deleteHadesRecord(name)
			if ret != apiSucess{
				return ret
			}
			a.deleteIpMonitorRecord(ipaddr)

		default:
			return notSupportIpv6
		}
	}
	return apiSucess
}

func (a *hadesApi )processTypeAPut(s *apiService, domain string)string {
	for key, val := range s.UpdateMap {
		ipPre := net.ParseIP(key)
		ipNew := net.ParseIP(val)

		if ipPre.To4() != nil && ipNew.To4() != nil {
			name := a.buildDNSNameString(a.domain, userSubdomain, domain, a.getHashIp(key))
			ret := a.deleteHadesRecord(name)
			if ret != apiSucess {
				return ret
			}
			a.deleteIpMonitorRecord(key)

			name = a.buildDNSNameString(a.domain, userSubdomain, domain, a.getHashIp(val))
			ret = a.setHadesRecord(name, val,"A")
			if ret != apiSucess {
				return ret
			}

			a.writeIpMonitorRecord(val)

		} else {
			return notIpAddr
		}
	}
	return apiSucess
}


func (a *hadesApi )processTypeCnamePut(s *apiService, domain string)string {
	for key, val := range s.UpdateMap {
		name := a.buildDNSNameString(a.domain,userSubdomain,key)
		ret := a.deleteHadesRecord(name)
		if ret == apiSucess{
			nameNew := a.buildDNSNameString(a.domain,userSubdomain,val)
			return a.setHadesRecord(nameNew, domain,"CNAME")
		}
		return ret
	}
	return apiSucess
}
func (a *hadesApi)processPost(s *apiService, domain string )string{

	if domain == ""{
		return noDomainName
	}
	if "" == s.OpsType{
		return notSupportOpsType
	}
	ret :=""
	switch strings.ToUpper(s.OpsType){
	case "A":
		ret = a.processTypeAPost(s,domain)
	case "CNAME":
		name := a.buildDNSNameString(a.domain,userSubdomain,s.DomainAlias)
		ret = a.setHadesRecord(name,domain,"CNAME")
	default:
		return noOpsType
	}
	return ret
}

func (a *hadesApi)processPut(s *apiService, domain string)string{

	if domain == ""{
		return noDomainName
	}
	if "" == s.OpsType{
		return notSupportOpsType
	}
	if len(s.UpdateMap) ==0{
		return errBodyUpdate
	}
	ret :=""
	switch strings.ToUpper(s.OpsType){
	case "A":
		ret = a.processTypeAPut(s,domain)
	case "CNAME":
		ret = a.processTypeCnamePut(s, domain)
	default:
		return noOpsType
	}
	return ret

}
func (a *hadesApi)processGet(s *apiService,domain string)string{

        svc := make(map[string]apiService)
	err := a.getHadesRecords(domain, svc)
	if err != nil{
		glog.Errorf("%s\n", err.Error())
		return errGetDomainName
	}

	b, err := json.Marshal(svc)
	if err != nil {
		glog.Errorf("%s\n", err.Error())
		return errGetDomainName
	}
	return string(b)

}
func (a *hadesApi)processDelete(s *apiService,domain string)string{

	if domain == ""{
		return noDomainName
	}
	ret := ""
	if s.OpsType == ""{
		name := a.buildDNSNameString(a.domain,userSubdomain,domain)
		ret = a.deleteHadesRecord(name)
		return ret
	}
	switch strings.ToUpper(s.OpsType){
	case "A":
		ret = a.processTypeADelete(s,domain)
	case "CNAME":
		name := a.buildDNSNameString(a.domain,userSubdomain,domain)
		ret = a.deleteHadesRecord(name)
	default:
		return noOpsType
	}
	return ret
}

func (a *hadesApi) handler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	val,ok := r.Header["Token"]
	if !ok{
		fmt.Fprintf(w, "%s",noAuthorization)
		return
	}
	if strings.Compare(val[0], hapi.auth) != 0 {
		fmt.Fprintf(w, "%s",errAuthorization)
		return
	}
	result, _:= ioutil.ReadAll(r.Body)
	r.Body.Close()

	glog.V(4).Infof("api req body :%s\n" ,result)

	var s apiService;
	json.Unmarshal([]byte(result), &s)

	domainName := ""
	domain,ok := r.Header["Domain"]
	if ok{
		domainName = strings.ToLower(domain[0])
	}

	ret :=""
	switch strings.ToUpper(r.Method){
	case "PUT":
		ret = a.processPut(&s,domainName)
	case "POST":
		ret = a.processPost(&s,domainName)
	case "GET":
		ret = a.processGet(&s,domainName)
	case "DELETE":
		ret = a.processDelete(&s,domainName)
	default:
		ret = notSupportOpsType
	}

	fmt.Fprintf(w, "%s\n",ret)
}

func RunApi(client *tools.EtcdOps, apiAddr string, domain string, auth string, ipMonitorPath string) {

	_, err := net.Dial("tcp", apiAddr)
       if err == nil {
           glog.Fatalf("the addr is used:%s\n",apiAddr)
       }

	glog.Infof("hades api run  with addr =%s domain : %s\n",apiAddr,domain)
	hapi.etcdClient = client
	hapi.domain = domain
	hapi.auth   = auth
	hapi.ipMonitorPath = ipMonitorPath
	http.HandleFunc("/hades",hapi.handler)

	go http.ListenAndServe(apiAddr, nil)
}
