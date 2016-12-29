package main

import (
	"testing"
	"fmt"
	"bytes"
	"github.com/ipdcode/hades/utils"
	"encoding/json"
	"net/http"
	"net"
	"io/ioutil"
        "time"
        "strings"
        "os"
)

const (
        tpyeABase  = "hades-api-test-type-a"
        typeCnameBase = "hades-api-test-type-cname"
)
var domainTpyeA string =""
var domainTpyeCname string =""

func startApiServer(){
        _, err := net.Dial("tcp", gConfig.HadesApi.ApiAddr)
        if err == nil {
                return
        }
        checkConfigOps()
	var etcdClient *tools.EtcdOps = newEtcdClient(gConfig.General.EtcdServer)
	RunApi(etcdClient, gConfig.HadesApi.ApiAddr ,gConfig.General.HadesDomain,gConfig.HadesApi.ApiAuth, gConfig.General.IpMonitorPath )
        time.Sleep(10)
        domainTpyeA = fmt.Sprintf("%s-%d",tpyeABase,os.Getpid())
        domainTpyeCname = fmt.Sprintf("%s-%d",typeCnameBase,os.Getpid())
}

func doreq( a apiService, domain string, method string )(error,string) {

       b, err := json.Marshal(a)
        if err != nil {
                return err,""
        }

        body := bytes.NewBuffer([]byte(b))
        saddr := "http://" + gConfig.HadesApi.ApiAddr +"/hades"
        client := &http.Client{}
        req, err := http.NewRequest(method, saddr,body)
        if err != nil {
                return err, ""
        }
        req.Header.Set("Content-Type", "application/json;charset=UTF-8")
        req.Header.Set("token", gConfig.HadesApi.ApiAuth)
        req.Header.Set("domain", domain)
        res, err := client.Do(req)
        defer res.Body.Close()
        if err != nil {
              return err,""
        }
        result, err := ioutil.ReadAll(res.Body)
        res.Body.Close()
        if err != nil {
                return err,""
        }
        ret := fmt.Sprintf("%s", result)
       // fmt.Print(ret)
        return err,ret

}
func TestApiPost(t *testing.T) {
	startApiServer()
	var a apiService
	a.OpsType = "A"
	a.DomainIps = make([]string,2)
	a.DomainIps[0] = "10.0.0.1"
	a.DomainIps[1] = "10.0.0.2"
        err,ret := doreq(a,domainTpyeA,"POST")
        if err !=nil || !strings.HasPrefix(ret,"OK"){
                t.Fatalf("err: %v  ret:%s",err,ret)
        }

	var c apiService
	c.OpsType = "CNAME"
	c.DomainAlias = domainTpyeCname
        err ,ret = doreq(c,domainTpyeA,"POST")
        if err !=nil || !strings.HasPrefix(ret,"OK") {
                t.Fatalf("err: %v  ret:%s",err,ret)
        }
}
func TestApiGet(t *testing.T) {
	startApiServer()
	var a apiService

        err,ret:= doreq(a,domainTpyeA,"GET")
        if err !=nil || !strings.Contains(ret,"10.0.0.1"){
                t.Fatalf("err: %v  ret:%s",err,ret)
        }

	var c apiService
        err ,ret= doreq(c,domainTpyeCname,"GET")
        if err !=nil || !strings.Contains(ret,domainTpyeA){
                t.Fatalf("err: %v  ret:%s",err,ret)
        }
}

func TestApiPut(t *testing.T) {
	startApiServer()
	var a apiService
	a.OpsType = "A"
	a.UpdateMap = make(map[string]string,2)
	a.UpdateMap["10.0.0.1"] = "192.0.0.1"
	a.UpdateMap["10.0.0.2"] = "192.0.0.2"
        err,ret := doreq(a,domainTpyeA,"PUT")
        if err !=nil || !strings.HasPrefix(ret,"OK") {
                t.Fatalf("err: %v  ret:%s",err,ret)
        }

	var c apiService
	c.OpsType = "CNAME"
	c.UpdateMap = make(map[string]string,1)
        c.UpdateMap[domainTpyeCname] = domainTpyeCname

        err,ret = doreq(c,domainTpyeA,"PUT")
        if err !=nil || !strings.HasPrefix(ret,"OK") {
                t.Fatalf("err: %v  ret:%s",err,ret)
        }
}
func TestApiDelete(t *testing.T) {
	startApiServer()
	var a apiService

        err,ret:= doreq(a,domainTpyeA,"DELETE")
        if err !=nil || !strings.HasPrefix(ret,"OK") {
                t.Fatalf("err: %v  ret:%s",err,ret)
        }

	var c apiService
        err,ret = doreq(c,domainTpyeCname,"DELETE")
        if err !=nil || !strings.HasPrefix(ret,"OK") {
                t.Fatalf("err: %v  ret:%s",err,ret)
        }
}
