package main

import (
	"flag"
	"log"
	"net/http"

	"funroute/extensions/paymentdemo"
	"funroute/mvp"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	registry, err := paymentdemo.NewRegistry()
	if err != nil {
		log.Fatal(err)
	}
	server, err := mvp.NewServer(registry)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("FunRoute MVP: http://%s", *address)
	log.Fatal(http.ListenAndServe(*address, server))
}
