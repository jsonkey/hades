// Copyright (c) 2014 The HADES Authors. All rights reserved.
// Use of this source code is governed by The MIT License (MIT) that can be
// found in the LICENSE file.

package cache

import (
	"crypto/sha1"
	"sync"
	"time"
	"hash/fnv"
	"bytes"

	"github.com/miekg/dns"
	"github.com/golang/glog"
)

// Elem hold an answer and additional section that returned from the cache.
// The signature is put in answer, extra is empty there. This wastes some memory.
type elem struct {
	sync.Mutex
	expiration time.Time // time added + TTL, after this the elem is invalid
	msg        *dns.Msg
	mAnswer    map[string][]dns.RR // keep to return the pre RR for
}

// Cache is a cache that holds on the a number of RRs or DNS messages. The cache
// eviction is randomized.
type Cache struct {
	sync.RWMutex
	capacity int
	m        map[string]*elem
	AvaliableIps   map[string] bool  // the host alive
	ttl      time.Duration
}

// New returns a new cache with the capacity and the ttl specified.
func New(capacity, ttl int) *Cache {
	c := new(Cache)
	c.m = make(map[string]*elem)
	c.AvaliableIps = make(map[string]bool)
	c.capacity = capacity
	c.ttl = time.Duration(ttl) * time.Second
	return c
}

func (c *Cache) Capacity() int { return c.capacity }

func (c *Cache) Remove(s string) {
	c.Lock()
	delete(c.m, s)
	c.Unlock()
}

// the key in cache is diff from domain
func (c *Cache)keyExtendTypeA(name string, dnssec, tcp bool) string {
	h := sha1.New()
	t:=[]byte(name)  // del hades test.default.hades.local.hades. -->test.default.hades.local. 6: sizeof(hades.)
	i := append(t[:len(t)-6], packUint16(dns.TypeA)...)
	if dnssec {
		i = append(i, byte(255))
	}
	if tcp {
		i = append(i, byte(254))
	}
	return string(h.Sum(i))
}

func (c *Cache) checkCacheExitst(r interface{} ) (valueIdx int,keyExist bool,key string){

	var nameR string = ""
	var valA *dns.A = nil
	var valCname *dns.CNAME = nil

	keyExist = false
	valueIdx = -1
	key = ""
	switch r.(type) {
	case *dns.A:
		valA =  r.(*dns.A)
		nameR = valA.Hdr.Name
	case *dns.CNAME:
		valCname = r.(*dns.CNAME)
		nameR = valCname.Hdr.Name
	default:
		return valueIdx,keyExist,key
	}
	valueIdx = 0
	key = c.keyExtendTypeA(nameR, false,false)
	if e, ok := c.m[key]; ok {
		keyExist = true
		// Cname match specal value -1
		if valCname != nil{
			return -1, keyExist,key
		}
		// type A  match ,we compare the value
		for _, r := range e.msg.Answer {
			valAnswerA, b := r.(*dns.A)
			if b{
				ret := bytes.Compare(valA.A ,valAnswerA.A)
				if ret ==0 {
					return valueIdx, keyExist,key
				}
			}
			valueIdx += 1
		}
	}
	// the key find but no value use uadateset
	return valueIdx, keyExist,key
}
//del the val from the l2 level cache
func (c *Cache) delValFromDictL2(valA *dns.A ,l2Map map[string][]dns.RR ){
	for k, v := range l2Map {
		valAnswerA, b :=  v[0].(*dns.A)
		if b{
			ret := bytes.Compare(valA.A ,valAnswerA.A)
			if ret ==0 {
				delete(l2Map , k)
			}
		}

	}
	return
}
func (c *Cache) UpdateRcacheSet(val interface{}) {
	glog.V(2).Infof(" UpdateRcacheSet =%v\n",val)
	// type A we update the dict
	valA ,b := val.(*dns.A)
	if b != true {
		return
	}
	c.Lock()
	_, find, matchKey:= c.checkCacheExitst(valA)
	if find{
		//type A update the  Ansers
		e := c.m[matchKey]
		e.msg.Answer = append(e.msg.Answer, valA)
	}
	c.Unlock()
}

func (c *Cache) UpdateRcacheUpdate(valAOld interface{}, valANew interface{} ) {
	glog.V(2).Infof(" UpdateRcacheUpdate valAOld=%v valANew =%v\n",valAOld,valANew)
	c.Lock()
	defer c.Unlock()
	idx, find, matchKey:= c.checkCacheExitst(valAOld)
	//glog.Infof(" UpdateRcacheSet find =%v matchKey =%v \n",find,matchKey)
	if find{
		// cname match we del the key and return
		if idx < 0{
			delete(c.m, matchKey)
			return
		}
		//type A update the  Ansers
		e := c.m[matchKey]
		e.msg.Answer[idx] = valANew.(*dns.A)
		//del the old val from dict
		e.Lock()
		c.delValFromDictL2(valAOld.(*dns.A), e.mAnswer)
		e.Unlock()
	}
}
func (c *Cache) UpdateRcacheDelete(valA interface{}) {
	glog.V(2).Infof(" UpdateRcacheDelete =%v\n",valA)
	c.Lock()
	defer c.Unlock()
	idx, find,matchKey := c.checkCacheExitst(valA)
	//glog.Infof(" UpdateRcacheSet find =%v matchKey =%v \n",find,matchKey)
	if find{
		// cname match we del the key and return
		if idx < 0{
			delete(c.m, matchKey)
			return
		}
		//del form Ansers
		e := c.m[matchKey]

		e.msg.Answer = append(e.msg.Answer[:idx], e.msg.Answer[idx+1:]...)
		//del from dict
		e.Lock()
		c.delValFromDictL2(valA.(*dns.A), e.mAnswer)
		e.Unlock()
	}
}
// EvictRandom removes a random member a the cache.
// Must be called under a write lock.
func (c *Cache) EvictRandom() {
	clen := len(c.m)
	if clen < c.capacity {
		return
	}
	i := c.capacity - clen
	for k,_ := range c.m {
		delete(c.m, k)
		i--
		if i == 0 {
			break
		}
	}
}

// InsertMessage inserts a message in the Cache. We will cache it for ttl seconds, which
// should be a small (60...300) integer.
func (c *Cache) InsertMessage(s string, msg *dns.Msg, remoteIp string) {
	if c.capacity <= 0 {
		return
	}
	c.Lock()
	if _, ok := c.m[s]; !ok {
		elm := &elem{expiration:time.Now().UTC().Add(c.ttl), msg:msg.Copy()}
		elm.mAnswer = make(map[string][]dns.RR)
		c.m[s] = elm
		if len(msg.Answer) > 0 {
			c.cacheMsgPack(elm, msg, remoteIp)
		}
	}
	c.EvictRandom()
	c.Unlock()
}

// InsertSignature inserts a signature, the expiration time is used as the cache ttl.
func (c *Cache) InsertSignature(s string, sig *dns.RRSIG) {
	if c.capacity <= 0 {
		return
	}
	c.Lock()

	if _, ok := c.m[s]; !ok {
		m := ((int64(sig.Expiration) - time.Now().Unix()) / (1 << 31)) - 1
		if m < 0 {
			m = 0
		}
		t := time.Unix(int64(sig.Expiration)-(m*(1<<31)), 0).UTC()
		c.m[s] = &elem{expiration:t, msg: &dns.Msg{Answer: []dns.RR{dns.Copy(sig)}}}
	}
	c.EvictRandom()
	c.Unlock()
}

// pack the msg return the pre svc ip or the random one

func (c *Cache) cacheMsgPack(e *elem,msg *dns.Msg,remoteIp string){
	//if has pre query value return pre
	e.Lock()
	defer e.Unlock()
	if answer, ok := e.mAnswer[remoteIp]; ok {
		// check avliable
		avliable  := false
		for _, r := range answer{
			if valA ,ok := r.(*dns.A); ok{
				key := valA.A.String()
				if _,e:= c.AvaliableIps[key]; e{
					avliable = true
				}
			}
		}
            if avliable{
		    msg.Answer = answer
		    return
	    }else{
		    delete(e.mAnswer,remoteIp )
	    }
	}
	//Cname return many vals : aliases names and svcip ; others just svc ip
	var ips []int

	// when each ip is not avaliable retrun the fist one
	ensureOneIp := 0
	for i, r := range msg.Answer {
		 switch r.(type) {
		 	case *dns.A:
				// choose the first one
				if ensureOneIp ==0{
					ensureOneIp = i
				}
				valA := r.(*dns.A)
				key := valA.A.String()
				if _,e:= c.AvaliableIps[key]; e{
					ips = append(ips,i)
				}
		 	case *dns.CNAME:
				e.mAnswer[remoteIp] = append(e.mAnswer[remoteIp] ,r)
			 // other type return org val
		    default:
			 	return

		 }
	}
	// no hosts avaluable choose one
	if len(ips) ==0{
		ips = append(ips,ensureOneIp)
	}
	h := fnv.New32a()
	h.Write([]byte(remoteIp))
	idx := int(h.Sum32()) % len(ips)
	answerNew := msg.Answer[ips[idx]]

	e.mAnswer[remoteIp] = append(e.mAnswer[remoteIp] ,answerNew)
	msg.Answer = e.mAnswer[remoteIp]

	return
}

// Search returns a dns.Msg, the expiration time and a boolean indicating if we found something
// in the cache.
func (c *Cache) DoSearch(s string,remoteIp string) (*dns.Msg, time.Time, bool) {
	if c.capacity <= 0 {
		return nil, time.Time{}, false
	}
	c.RLock()
	if e, ok := c.m[s]; ok {
		e1 := e.msg.Copy()
		if len(e1.Answer) > 0 {
			c.cacheMsgPack(e, e1, remoteIp)
		}
		c.RUnlock()
		return e1, e.expiration, true
	}
	c.RUnlock()
	return nil, time.Time{}, false
}

// Key creates a hash key from a question section.

func Key(q dns.Question, tcp bool) string {
	h := sha1.New()
	i := append([]byte(q.Name), packUint16(q.Qtype)...)
	if tcp {
		i = append(i, byte(254))
	}
	return string(h.Sum(i))
}

// Key uses the name, type and rdata, which is serialized and then hashed as the key for the lookup.
func KeyRRset(rrs []dns.RR) string {
	h := sha1.New()
	i := []byte(rrs[0].Header().Name)
	i = append(i, packUint16(rrs[0].Header().Rrtype)...)
	for _, r := range rrs {
		switch t := r.(type) { // we only do a few type, serialize these manually
		case *dns.SOA:
			// We only fiddle with the serial so store that.
			i = append(i, packUint32(t.Serial)...)
		case *dns.SRV:
			i = append(i, packUint16(t.Priority)...)
			i = append(i, packUint16(t.Weight)...)
			i = append(i, packUint16(t.Weight)...)
			i = append(i, []byte(t.Target)...)
		case *dns.A:
			i = append(i, []byte(t.A)...)
		case *dns.AAAA:
			i = append(i, []byte(t.AAAA)...)
		case *dns.NSEC3:
			i = append(i, []byte(t.NextDomain)...)
			// Bitmap does not differentiate in HADES.
		case *dns.DNSKEY:
		case *dns.NS:
		case *dns.TXT:
		}
	}
	return string(h.Sum(i))
}

func (c *Cache) Search(question dns.Question, tcp bool, msgid uint16,remoteIp string) *dns.Msg {
	key := Key(question, tcp)
	m1, exp, hit := c.DoSearch(key,remoteIp)
	if hit {
		// Cache hit! \o/
		if time.Since(exp) < 0 {
			m1.Id = msgid
			m1.Compress = true
			// Even if something ended up with the TC bit *in* the cache, set it to off
			m1.Truncated = false
			return m1
		}
		// Expired! /o\
		c.Remove(key)
	}
	return nil
}

func packUint16(i uint16) []byte { return []byte{byte(i >> 8), byte(i)} }
func packUint32(i uint32) []byte { return []byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)} }
