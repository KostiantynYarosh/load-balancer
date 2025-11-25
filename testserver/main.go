package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"
)

func main() {
	port := flag.String("port", "8080", "server port")
	timeToProcess := flag.Int("time", 2, "time to process a request")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		delay := time.Duration(*timeToProcess) * time.Second
		time.Sleep(delay)
		fmt.Fprintf(w, "Response from server on port %s (delay: %v)\n", *port, delay)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := ":" + *port
	fmt.Printf("Server starting on %s\n", addr)
	http.ListenAndServe(addr, nil)
}
