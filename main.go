package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fluxynet/gorexy/wsutils"
	"github.com/fsnotify/fsnotify"
)

const httpMapping = "http"
const wsMapping = "ws"

//Config represents application configuration as loaded from gorexy.json
type Config struct {
	Mappings []Mapping `json:"mappings"`
	Services []Service `json:"services"`
	Port     int       `json:"port"`
	Silent   bool      `json:"silent"`
	HTTPS    struct {
		Enabled  bool   `json:"enabled"`
		Certfile string `json:"cert"`
		Keyfile  string `json:"key"`
		NoHTTP   bool   `json:"nohttp"`
	} `json:"https"`
}

//Mapping represents a proxy mapping
type Mapping struct {
	Path        string `json:"path"`
	Destination string `json:"destination"`
}

//Service represents a service to start
type Service struct {
	Name       string `json:"name"`
	Dir        string `json:"dir"`
	Cmd        string `json:"cmd"`
	Env        string `json:"env"`
	Args       string `json:"args"`
	AutoReload bool   `json:"auto_reload"`
	Silent     bool   `json:"silent"`
}

// HTTPProxy represents an http proxy service with a corresponding prefix
type HTTPProxy struct {
	Prefix string
	Proxy  *httputil.ReverseProxy
}

// WSProxy represents a websocket proxy service with a corresponding prefix
type WSProxy struct {
	Prefix string
	Proxy  *wsutils.ReverseProxy
}

var (
	mapping map[string]string
	htprox  []HTTPProxy
	wsprox  []WSProxy
	ports   map[string]string
	port    int
	silent  bool

	portRegex = regexp.MustCompile(`(?m)\{PORT(?P<port>[0-9]+)\}`)

	gopath = func() string {
		if g := os.Getenv("GOPATH"); g != "" {
			return g
		}
		return build.Default.GOPATH
	}()

	homedir = func() string {
		if usr, e := user.Current(); e == nil {
			return usr.HomeDir
		}

		return "~"
	}()
)

func main() {
	var (
		err      error
		config   *Config
		wg       sync.WaitGroup
		filename string
	)

	if len(os.Args[1:]) == 0 {
		filename = "gorexy.json"
	} else {
		filename = os.Args[1]
	}

	config, err = loadConfig(normalizePath(filename, true))
	if err != nil {
		log.Fatalf("Failed to load config file: %s", err)
	}

	if config.Port == 0 {
		config.Port = 8000
	}

	ports = initPorts(config.Port, config.Services)
	silent = config.Silent

	for _, service := range config.Services {
		if e := startService(service); e != nil {
			log.Fatalf("Failed to start service [%s]: %s", service.Name, err)
		}
	}

	htprox, wsprox, err = createProxies(config.Mappings)
	if err != nil {
		log.Fatalf("Invalid mapping: %s", err)
	}

	http.HandleFunc("/", forwarder)

	if !config.HTTPS.Enabled || !config.HTTPS.NoHTTP {
		wg.Add(1)
		port := strconv.Itoa(config.Port)
		go func() {
			e := http.ListenAndServe(":"+port, nil)
			if e != nil {
				fmt.Println("Error serving http: ", e)
			}
			wg.Done()
		}()
		log.Printf("HTTP listening on http://127.0.0.1:%s\n", port)
	}

	if config.HTTPS.Enabled {
		wg.Add(1)
		port := strconv.Itoa(config.Port + 1)
		go func() {
			e := http.ListenAndServeTLS(":"+port, normalizePath(config.HTTPS.Certfile, true), normalizePath(config.HTTPS.Keyfile, true), nil)
			if e != nil {
				fmt.Println("Error serving https: ", e)
			}
			wg.Done()
		}()
		log.Printf("HTTPS listening on : https://127.0.0.1:%s\n", port)
	}

	wg.Wait()
	log.Println("Server stopped")
}

func forwarder(w http.ResponseWriter, r *http.Request) {
	if wsutils.IsWebsocket(r) {
		for _, s := range wsprox {
			if strings.HasPrefix(r.URL.Path, s.Prefix) {
				s.Proxy.ServeHTTP(w, r)
				return
			}
		}
	} else {
		for _, s := range htprox {
			if strings.HasPrefix(r.URL.Path, s.Prefix) {
				s.Proxy.ServeHTTP(w, r)
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
		config.Port, err = strconv.Atoi(envport)
	} else if config.Port == 0 {
		config.Port = 8000
	}

	return config, err
}

func createProxies(mappings []Mapping) ([]HTTPProxy, []WSProxy, error) {
	var (
		err    error
		htprox []HTTPProxy
		wsprox []WSProxy
	)

	for i, mapping := range mappings {
		var url *url.URL

		if mapping.Path == "" {
			return nil, nil, fmt.Errorf("mapping path not found at element %d", i+1)
		}

		if mapping.Destination == "" {
			return nil, nil, fmt.Errorf("mapping destination not found at element %d", i+1)
		}

		mapping.Destination = parsePorts(mapping.Destination)

		url, err = url.Parse(mapping.Destination)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid url %s: %s", mapping.Destination, err)
		}

		if url.Scheme == httpMapping {
			htprox = append(htprox, HTTPProxy{Prefix: mapping.Path, Proxy: httputil.NewSingleHostReverseProxy(url)})
		} else if url.Scheme == wsMapping {
			wsprox = append(wsprox, WSProxy{Prefix: mapping.Path, Proxy: wsutils.NewReverseProxy(url)})
		} else {
			return nil, nil, fmt.Errorf("invalid mapping type %s for %s -> %s", url.Scheme, mapping.Path, mapping.Destination)
		}
	}

	return htprox, wsprox, err
}

func initPorts(basePort int, services []Service) map[string]string {
	ports := make(map[string]string)
	p := basePort + 2

	for _, service := range services {
		matches := portRegex.FindAllStringSubmatch(service.Args+service.Env, -1)
		for _, match := range matches {
			if _, exists := ports[match[0]]; !exists {
				ports[match[0]] = strconv.Itoa(p)
				p++
			}
		}
	}

	return ports
}

func parsePorts(str string) string {
	matches := portRegex.FindAllStringSubmatch(str, -1)
	for _, match := range matches {
		if _, exists := ports[match[0]]; exists {
			str = strings.Replace(str, match[0], ports[match[0]], 1)
		}
	}

	return str
}

func commandGetAbsolute(cmd string) (string, error) {
	var err error

	if cmd, err = filepath.Abs(cmd); err != nil {
		return "", err
	}

	var info os.FileInfo
	if info, err = os.Stat(cmd); err == nil && !info.IsDir() {
		return cmd, nil
	}

	return "", nil
}

func normalizePath(path string, absolute bool) string {
	path = strings.Replace(path, "$GOPATH", gopath, -1)
	path = strings.TrimRight(path, "/\\")
	path = strings.Replace(path, "~", homedir, 1)

	if !absolute {
		return path
	}

	if r, e := filepath.Abs(path); e == nil {
		path = r
	}

	return path
}

func startService(service Service) error {
	var cmd *exec.Cmd

	if service.Cmd == "" {
		return fmt.Errorf("cmd must not be empty")
	}

	service.Env = parsePorts(service.Env)
	service.Args = parsePorts(service.Args)

	if service.Dir == "" {
		if r, e := exec.LookPath(service.Cmd); e == nil {
			service.Cmd = r
		} else if r, e := commandGetAbsolute(service.Cmd); e == nil {
			service.Cmd = r
		} else {
			return fmt.Errorf("command %s not found in PATH and %s is not a file", service.Cmd, service.Dir)
		}
	} else {
		service.Dir = normalizePath(service.Dir, true)
		service.Cmd = normalizePath(service.Cmd, false)

		if r, e := exec.LookPath(service.Cmd); e == nil {
			service.Cmd = r
		} else if r, e := commandGetAbsolute(service.Cmd); e == nil {
			service.Cmd = r
		} else {
			relpath := service.Dir + string(os.PathSeparator) + service.Cmd
			info, e := os.Stat(relpath)
			if e != nil {
				return fmt.Errorf("command %s not found in PATH and %s", service.Cmd, service.Dir)
			} else if info.IsDir() {
				return fmt.Errorf("command %s not found in PATH and %s is not a file", service.Cmd, service.Dir)
			}
			service.Cmd = relpath
		}
	}

	if service.Args == "" {
		cmd = exec.Command(service.Cmd)
	} else {
		args := strings.Split(service.Args, " ")
		cmd = exec.Command(service.Cmd, args...)
	}

	if service.Dir != "" {
		cmd.Dir = service.Dir
	}

	if service.Env != "" {
		cmd.Env = strings.Split(service.Env, " ")
	}

	go runCmd(cmd, service)

	return nil
}

func runCmd(cmd *exec.Cmd, service Service) error {
	var err error

	if !silent && !service.Silent {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	err = func() error {
		var tries = 10
		for {
			cmd.Start()
			if err == nil {
				log.Printf("[started] %s\n", service.Name)
				tries--
				return nil
			} else if tries == 0 {
				log.Printf("[failed] %s - %s\n", service.Name, err)
				return err
			}

			time.Sleep(time.Millisecond * 200)
		}
	}()

	if service.AutoReload {
		go watchService(service, cmd)
	}

	return err
}

func watchService(service Service, cmd *exec.Cmd) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("[watch failed] %s: %s", service.Name, err)
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case <-watcher.Events:
				log.Printf("[reloading] %s\n", service.Name)
				cmd.Process.Kill()
				startService(service)
				return
			}
		}
	}()

	log.Printf("[watching] %s\n", service.Name)
	err = watcher.Add(service.Cmd)
	if err != nil {
		log.Fatalf("[watch failed] %s: %s", service.Name, err)
	}
}
