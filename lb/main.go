package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Server struct {
	Id                       int
	MaximumActiveConnections int64
	CurrentActiveConnections int64
	Status                   atomic.Bool
	URL                      string
	ReverseProxy             *httputil.ReverseProxy
}

type LoadBalancer struct {
	Servers                []Server
	TotalActiveConnections int64
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	server := lb.selectServer()
	if server == nil {
		http.Error(w, "No available servers", http.StatusServiceUnavailable)
		return
	}

	atomic.AddInt64(&server.CurrentActiveConnections, 1)
	atomic.AddInt64(&lb.TotalActiveConnections, 1)

	defer func() {
		atomic.AddInt64(&server.CurrentActiveConnections, -1)
		atomic.AddInt64(&lb.TotalActiveConnections, -1)
	}()

	server.ReverseProxy.ServeHTTP(w, r)
}

func (lb *LoadBalancer) selectServer() *Server {
	var optimalServer *Server
	for i := 0; i < len(lb.Servers); i++ {
		currentLoad := float64(lb.Servers[i].CurrentActiveConnections) / float64(lb.Servers[i].MaximumActiveConnections)
		if !lb.Servers[i].Status.Load() || currentLoad >= 1.0 {
			continue
		}
		if optimalServer == nil {
			optimalServer = &lb.Servers[i]
			continue
		}
		optimalLoad := float64(optimalServer.CurrentActiveConnections) / float64(optimalServer.MaximumActiveConnections)
		if currentLoad < optimalLoad {
			optimalServer = &lb.Servers[i]
		}
	}
	return optimalServer
}

func setupReversProxies(lb *LoadBalancer) {
	for i := range lb.Servers {
		serverURL, _ := url.Parse(lb.Servers[i].URL)
		lb.Servers[i].ReverseProxy = httputil.NewSingleHostReverseProxy(serverURL)
	}
}

func healthCheck(lb *LoadBalancer, pause time.Duration) {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(pause)
	defer ticker.Stop()

	for {
		var wg sync.WaitGroup
		for i := range lb.Servers {
			wg.Add(1)
			go func(serv *Server) {
				defer wg.Done()
				resp, err := client.Get(serv.URL + "/health")

				newStatus := false
				if err == nil && resp.StatusCode == 200 {
					newStatus = true
				}
				if resp != nil {
					resp.Body.Close()
				}

				serv.Status.Store(newStatus)

			}(&lb.Servers[i])
		}
		wg.Wait()
		<-ticker.C
	}
}

func main() {
	filepath := flag.String("servers", "", "path to a file with servers info")
	healthPauseNum := flag.Int("health-timeout", 120, "Pause in seconds between running health check on servers")
	flag.Parse()

	healthPause := time.Duration(*healthPauseNum) * time.Second

	serversInfoFile, err := os.ReadFile(*filepath)
	if err != nil {
		fmt.Print(err)
		return
	}

	var lb LoadBalancer
	err = json.Unmarshal(serversInfoFile, &lb)
	if err != nil {
		fmt.Print(err)
		return
	}

	setupReversProxies(&lb)

	go healthCheck(&lb, healthPause)

	http.Handle("/", &lb)

	err = http.ListenAndServeTLS(":8443", "creds/cert.pem", "creds/key.pem", nil)

	if err != nil {
		fmt.Println("Error starting server", err)
	}
}
