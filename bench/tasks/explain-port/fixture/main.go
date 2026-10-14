package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

const legacyPort = 9090 // no longer used

func main() {
	port := flag.Int("port", envInt("APP_PORT", 8081), "listen port")
	flag.Parse()
	http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
}

func envInt(k string, def int) int {
	var v int
	if _, err := fmt.Sscan(os.Getenv(k), &v); err != nil {
		return def
	}
	return v
}
