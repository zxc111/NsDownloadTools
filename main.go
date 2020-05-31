package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	TEST_URL     = "http://ctest-dl-lp1.cdn.nintendo.net/30m"
	USER_AGENT   = "Nintendo NX"
	DOWNLOAD_URL = "atum.hac.lp1.d4c.nintendo.net"

	MAX_THREAD = 50
)

var (
	cli     http.Client
	ipMap   *ips
	dnsChan chan string

	dnsList = []string{
		"1.0.0.1",
		"208.67.222.123",
		"4.2.2.1",
		"8.8.4.4",
		"1.1.1.1",
		"208.67.222.123",
		"4.2.2.2",
		"8.8.8.8",
		"223.5.5.5",
		"123.255.90.246",
		"218.102.23.228",
		"203.145.81.133",
		"223.6.6.6",
		"202.55.21.85",
		"168.95.192.1",
		"115.159.146.99",

		"1.2.4.8",
		"210.2.4.8",
		"203.80.96.9",
		"203.80.96.10",
		"203.112.2.4",
		"203.112.2.5",

		"101.226.4.6",
		"123.125.81.6",
		"119.29.29.29",
		"119.28.28.28",
		"180.76.76.76",
		"101.6.6.6",
		"182.254.116.116",
		"115.159.220.214",
		"218.108.23.1",
		"123.206.21.48",
		"114.114.114.119",
		"114.114.115.119",
		"114.114.114.114",
		"114.114.115.115",
		"114.114.114.110",
		"114.114.115.110",
		"168.126.63.1",
		"168.126.63.2",

		"202.67.240.221",
		"202.67.240.222",
		"202.45.84.58",
		"202.45.84.59",
		"202.81.252.1",
		"202.81.252.2",
		"202.14.67.4",
		"202.14.67.14",
	}
	current *bestIp
)

type bestIp struct {
	rate float64
	ip   string
	sync.Mutex
	finish bool
	speed  int
}

type ips struct {
	m map[string]struct{}
	sync.RWMutex
}

func (p *ips) Add(ip string) {
	p.Lock()
	defer p.Unlock()

	p.m[ip] = struct{}{}
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	dnsChan = make(chan string, MAX_THREAD)

	cli = http.Client{}
	ipMap = &ips{
		m: make(map[string]struct{}, 256),
	}
	current = &bestIp{}
}

func main() {
	wg := new(sync.WaitGroup)
	for i := 0; i < MAX_THREAD; i++ {
		wg.Add(1)
		go getIpFromDns(wg)
	}
	for _, ipStr := range dnsList {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		dnsChan <- ip.To4().String()
	}
	close(dnsChan)
	wg.Wait()
	//current.ip = "104.75.169.11"
	//current.finish = true
	for ip, _ := range ipMap.m {
		fetch(ip)
		if current.finish {
			log.Println(ip)
			break
		}
	}
	log.Printf("best is, %v", current)
	ps := newProxy(current)
	ps.start()
}

func fetch(ip string) {
	cli = http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Second * 2,
			}
			return d.DialContext(ctx, "tcp", ip+":80")
		}},
		Timeout: 5 * time.Second,
	}
	req, _ := http.NewRequest(http.MethodGet, TEST_URL, nil)
	req.Header.Add("User-Agent", USER_AGENT)
	resp, err := cli.Do(req)
	if err != nil {
		log.Println(err)
		return
	}
	readWg := new(sync.WaitGroup)
	readWg.Add(1)
	timeMark := time.Now().UnixNano()
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	size := 0
	finish := false
	go func(wg *sync.WaitGroup, ctx context.Context) {
		defer func() {
			recover()
		}()
		d := make([]byte, 1024*200)
		defer resp.Body.Close()
		defer wg.Done()
		for {
			n, err := resp.Body.Read(d)
			size += n
			if err != nil {
				if err == io.EOF {
					finish = true
				}
				return
			}
		}
	}(readWg, ctx)
	readWg.Wait()
	current.Lock()
	defer current.Unlock()
	if finish {
		current.ip = ip
		current.finish = true
		return
	}
	cost := time.Now().UnixNano() - timeMark
	result := float64(size) / float64(cost)

	if result > current.rate {
		current.rate = result
		current.ip = ip
	}
	log.Println(ip, "checked", strconv.Itoa(size/1024/int(cost/1000000000)), "KB/S")
}

func getIpFromDns(wg *sync.WaitGroup) {
	log.Println("check start")
	defer wg.Done()
	for {
		select {
		case ip, ok := <-dnsChan:
			if !ok {
				return
			}
			log.Println("check dns:", ip)
			getIp(ip)
		}
	}
}

func getIp(ip string) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Second * 2,
			}
			return d.DialContext(ctx, "udp", ip+":53")
		},
	}
	ctx, _ := context.WithTimeout(context.Background(), time.Second*3)
	ipFromDns, err := r.LookupIPAddr(ctx, DOWNLOAD_URL)
	if err != nil {
		log.Println(err)
	}
	for _, ip := range ipFromDns {
		ipMap.Add(ip.IP.To4().String())
	}
}
