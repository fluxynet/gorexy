package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/fluxynet/gorexy/wsutils"
)

const httpMapping = "http"
const wsMapping = "ws"

//Config represents application configuration as loaded from gorexy.json
type Config struct {
	Mappings []*Mapping `json:"mappings"`
	Port     *int       `json:"port"`
}

//Mapping represents a proxy mapping
type Mapping struct {
	Path        *string `json:"path"`
	Destination *string `json:"destination"`
}

var (
	mapping map[string]string
	htprox  map[string]*httputil.ReverseProxy
	wsprox  map[string]*wsutils.ReverseProxy
	port    int
)

func main() {
	var (
		err    error
		config *Config
	)

	confile := os.Getenv("CONF")
	if confile == "" {
		confile = "gorexy.json"
	}

	config, err = loadConfig(confile)
	if err != nil {
		log.Fatalf("Failed to load config file: %s", err)
	}

	htprox, wsprox, err = createProxies(config.Mappings)

	if err != nil {
		log.Fatalf("Invalid mapping: %s", err)
	}

	http.HandleFunc("/", forwarder)

	port := strconv.Itoa(*config.Port)
	log.Println("Listening on :" + port)
	err = http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal("Failed to start http server: ", err)
	}
	log.Println("Server stopped")
}

func forwarder(w http.ResponseWriter, r *http.Request) {
	if wsutils.IsWebsocket(r) {
		for prefix, proxy := range wsprox {
			if strings.HasPrefix(r.URL.Path, prefix) {
				proxy.ServeHTTP(w, r)
				return
			}
		}
	} else {
		for prefix, proxy := range htprox {
			if strings.HasPrefix(r.URL.Path, prefix) {
				proxy.ServeHTTP(w, r)
				return
			}
		}
	}

	w.WriteHeader(http.StatusBadGateway)
	fmt.Fprintf(w, "Uknown Gateway for prefix: %s", r.URL.Path)
}

func loadConfig(filename string) (*Config, error) {
	var (
		err    error
		data   []byte
		config = new(Config)
	)

	data, err = ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	config = new(Config)
	err = json.Unmarshal(data, config)
	if err != nil {
		return nil, err
	}

	envport := os.Getenv("PORT")
	if envport != "" {
		*config.Port, err = strconv.Atoi(envport)
	} else if config.Port == nil {
		*config.Port = 1337
	}

	return config, err
}

func createProxies(mappings []*Mapping) (map[string]*httputil.ReverseProxy, map[string]*wsutils.ReverseProxy, error) {
	var (
		err    error
		htprox = make(map[string]*httputil.ReverseProxy)
		wsprox = make(map[string]*wsutils.ReverseProxy)
	)

	for i, mapping := range mappings {
		var url *url.URL

		if mapping.Path == nil {
			return nil, nil, fmt.Errorf("mapping path not found at element %d", i+1)
		}

		if mapping.Destination == nil {
			return nil, nil, fmt.Errorf("mapping destination not found at element %d", i+1)
		}

		url, err = url.Parse(*mapping.Destination)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid url %s: %s", *mapping.Destination, err)
		}

		if url.Scheme == httpMapping {
			htprox[*mapping.Path] = httputil.NewSingleHostReverseProxy(url)
		} else if url.Scheme == wsMapping {
			wsprox[*mapping.Path] = wsutils.NewReverseProxy(url)
		} else {
			return nil, nil, fmt.Errorf("invalid mapping type %s for %s -> %s", url.Scheme, *mapping.Path, *mapping.Destination)
		}
	}

	return htprox, wsprox, err
}
