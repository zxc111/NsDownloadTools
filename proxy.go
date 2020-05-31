package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"
)

type proxyServer struct {
	ip *bestIp
}

func newProxy(ip *bestIp) *proxyServer {
	return &proxyServer{ip: ip}
}
func (ps *proxyServer) start() {

	server := &http.Server{
		Addr: "0.0.0.0:9999",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(context.Background(), "request", r)

			ps.handler(ctx, w, r)
		}),
	}
	log.Fatal(server.ListenAndServe())
}
func (ps *proxyServer) handler(ctx context.Context, w http.ResponseWriter, r *http.Request, ) {
	//log.Println(r.URL.Host)
	switch r.Method {
	case http.MethodConnect:
		hijacker, _ := w.(http.Hijacker)
		clientConn, _, err := hijacker.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer closeConn(clientConn)

		remote := "http://" + r.URL.Host
		ctx = context.WithValue(ctx, "url", remote)

		ps.CreateTunnel(ctx, clientConn)
	default:
		remote := r.URL.Scheme + "://" + r.URL.Host

		hijacker, _ := w.(http.Hijacker)
		clientConn, _, err := hijacker.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer closeConn(clientConn)
		ctx, _ := context.WithTimeout(ctx, time.Hour)
		ctx = context.WithValue(ctx, "url", remote)

		ps.GetMethod(ctx, r, clientConn)
	}
}

func closeConn(conn io.Closer) {
	err := conn.Close()
	if err != nil {
		log.Println(err)
	}
}

func (ps *proxyServer) newTr(ctx context.Context, port string) http.RoundTripper {
	req := ctx.Value("request").(*http.Request)
	host := ps.ip.ip + port
	if !strings.Contains(req.URL.String(), DOWNLOAD_URL) {
		host = req.URL.Host
		if !strings.Contains(host, ":") {
			host += port
		}
	}

	return &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Second * 2,
			}
			return d.DialContext(ctx, "tcp", host)
		},
	}
}
func (ps *proxyServer) CreateTunnel(ctx context.Context, from net.Conn) {

	req := ctx.Value("request").(*http.Request)

	host := req.Host
	if strings.Contains(req.URL.String(), DOWNLOAD_URL) {
		host = ps.ip.ip+":443"
	}
	conn, err := net.Dial("tcp", host)
	if err != nil {
		log.Println(err)
		return
	}
	_, err = fmt.Fprint(from, "HTTP/1.1 200 Connection Established\r\n\r\n")
	if err != nil {
		log.Println(err)
		return
	}

	exit1 := make(chan struct{})

	go func() {
		go io.Copy(conn, from)
		_, err := io.CopyBuffer(from, conn, make([]byte, 500*1024))
		if err != nil {
			log.Println(err)
		}
		close(exit1)

	}()
	select {
	case <-exit1:
	case <-ctx.Done():
	case <-time.Tick(time.Hour):
	}
}

func (ps *proxyServer) GetMethod(ctx context.Context, from *http.Request, to net.Conn) {
	//dump, err := httputil.DumpRequest(from, false)
	//if err != nil {
	//	log.Println(err)
	//	return
	//}
	//log.Println(string(dump))
	tr := ps.newTr(ctx, ":80")

	//remoteAddr := ctx.Value("url").(string)
	//log.Println(remoteAddr)

	req, err := http.NewRequest(
		from.Method,
		from.URL.String(),
		from.Body,
	)
	if err != nil {
		log.Println(err)
		return
	}
	req.URL = from.URL

	req.Header = from.Header

	resp, err := tr.RoundTrip(req)
	if err != nil {
		log.Println(err)
		return
	}
	defer closeConn(resp.Body)

	if resp.StatusCode != 200 {
		log.Println(resp.StatusCode)
		io.Copy(os.Stdout, resp.Body)
		fmt.Fprint(to, resp.StatusCode)
		log.Println("Connect Proxy Server Error")
		return
	}
	exit := make(chan struct{})
	rb, err := httputil.DumpResponse(resp, false)
	if err != nil {
		log.Println(err)
		return
	}
	to.Write(rb)
	go func() {
		_, err := io.Copy(to, resp.Body)
		if err != nil {
			log.Println(err)
		}
		close(exit)
	}()
	select {
	case <-exit:
	case <-ctx.Done():
	}
}
