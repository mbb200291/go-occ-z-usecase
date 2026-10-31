package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mbb200291/go-occ-z/occ"
)

//go:embed web/index.html web/style.css web/app.mjs
var assets embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "HTTP listen address")
	jitter := flag.Bool("jitter", true, "randomize conflict retry delays")
	flag.Parse()
	// OCC uses a working-directory journal. One fresh directory per server process
	// keeps this demo away from project data and other Managers.
	dir, err := os.MkdirTemp("", "occ-live-*")
	if err != nil {
		log.Fatal(err)
	}
	if err = os.Chdir(dir); err != nil {
		log.Fatal(err)
	}
	file, err := os.OpenFile("counters.bin", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	if err = file.Truncate(targetCount * 8); err != nil {
		log.Fatal(err)
	}
	adapter := &diskAdapter{file: file, writeDelay: 200 * time.Millisecond}
	server := &liveServer{adapter: adapter, start: time.Now()}
	retry := demoRetry(*jitter)
	mgr, err := occ.NewManager(adapter, occ.Config{Retry: &retry, Observer: server.observe})
	if err != nil {
		log.Fatal(err)
	}
	server.runner = mgr
	web, err := fs.Sub(assets, "web")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("OCC live visualizer: http://%s\nData and journal: %s\n", *addr, dir)
	log.Fatal(http.ListenAndServe(*addr, server.routes(http.FileServer(http.FS(web)))))
}
