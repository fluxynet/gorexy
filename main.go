package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

var (
	mapping map[string]string
	proxies map[string]*httputil.ReverseProxy
	api     *httputil.ReverseProxy
	webpack *httputil.ReverseProxy
)

func forwarder(w http.ResponseWriter, r *http.Request) {
	for prefix, proxy := range proxies {
		if strings.HasPrefix(r.URL.Path, prefix) {
			proxy.ServeHTTP(w, r)
			return
		}
	}

	w.WriteHeader(http.StatusBadGateway)
	fmt.Fprintf(w, "Uknown Gateway for prefix: %s", r.URL.Path)
}

func main() {
	var err error

	port := os.Getenv("PORT")
	if port == "" {
		port = "1337"
	}

	mapFile := os.Getenv("MAPPING")
	if mapFile == "" {
		mapFile = "gorexy.cfg"
	}

	mapping, err = loadMappings(mapFile)
	if err != nil {
		log.Fatalf("Failed to load mappings: %s", err)
	}

	proxies, err = createProxies(mapping)

	if err != nil {
		log.Fatalf("Invalid mapping: %s", err)
	}

	http.HandleFunc("/", forwarder)

	log.Println("Listening on :" + port)
	err = http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal("Failed to start http server: ", err)
	}
	log.Println("Server stopped")
}

func loadMappings(filename string) (map[string]string, error) {
	mapping := make(map[string]string)

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Split(line, " ")
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid mapping config. expected 2 fields separated by space, got %d for %s", len(fields), line)
		}

		mapping[fields[0]] = fields[1]
	}

	return mapping, nil
}

func createProxies(mapping map[string]string) (map[string]*httputil.ReverseProxy, error) {
	var (
		err     error
		proxies = make(map[string]*httputil.ReverseProxy)
	)

	for prefix, target := range mapping {
		var url *url.URL

		url, err = url.Parse(target)
		if err != nil {
			return nil, err
		}

		proxies[prefix] = httputil.NewSingleHostReverseProxy(url)

	}

	return proxies, err
}
