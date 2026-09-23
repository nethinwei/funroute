// Command mvp serves the workbench. The language server runs in the page, as
// WebAssembly in a worker, so all this does is hand out the files make site
// puts in site/: the same files the GitHub Pages workflow publishes.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	dir := flag.String("dir", "site", "the workbench's files, assembled by make site")
	flag.Parse()
	log.Printf("FunRoute workbench: http://%s", *address)
	server := &http.Server{
		Addr:              *address,
		Handler:           revalidated(http.FileServer(http.Dir(*dir))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

// revalidated makes the browser ask again before using a file it has: the
// files change with every build, the language server's module included.
func revalidated(files http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(response, request)
	})
}
