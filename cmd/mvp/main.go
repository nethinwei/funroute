package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"funroute/examples/payment"
	"funroute/mvp"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	registry, err := payment.NewRegistry()
	if err != nil {
		log.Fatal(err)
	}
	server, err := mvp.NewServer(registry)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("FunRoute MVP: http://%s", *address)
	httpServer := &http.Server{
		Addr:              *address,
		Handler:           server,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Fatal(httpServer.ListenAndServe())
}
