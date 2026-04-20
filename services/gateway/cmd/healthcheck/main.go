package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	c := &http.Client{Timeout: 2 * time.Second}
	r, err := c.Get("http://localhost:8080/health")
	if err != nil || r.StatusCode != 200 {
		os.Exit(1)
	}
}
