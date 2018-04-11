package main

import (
	"encoding/json"
	"flag"
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
	"strconv"
	"strings"

	"github.com/fluxynet/gorexy/wsutils"
)

const httpMapping = "http"
const wsMapping = "ws"

//Config represents application configuration as loaded from gorexy.json
type Config struct {
	Mappings []*Mapping `json:"mappings"`
	Services []*Service `json:"services"`
	Port     int        `json:"port"`
	Parallel bool       `json:"parallel"`
}

//Mapping represents a proxy mapping
type Mapping struct {
	Path        string `json:"path"`
	Destination string `json:"destination"`
}

//Service represents a service to start
type Service struct {
	Dir  string `json:"dir"`
	Cmd  string `json:"cmd"`
	Env  string `json:"env"`
	Args string `json:"args"`
}

var (
	mapping map[string]string
	htprox  map[string]*httputil.ReverseProxy
	wsprox  map[string]*wsutils.ReverseProxy
	ports   []string
	port    int

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
		err    error
		config *Config
	)

	flagset := flag.NewFlagSet("gorexy", flag.ExitOnError)
	cport := flagset.String("port", "", "Port to listen to")
	confile := flagset.String("conf", "gorexy.json", "Path to config file")
	flagset.Parse(os.Args[1:])

	config, err = loadConfig(normalizePath(*confile, true))
	if err != nil {
		log.Fatalf("Failed to load config file: %s", err)
	}

	if *cport != "" {
		config.Port, err = strconv.Atoi(*cport)
		if err != nil {
			log.Fatalf("Invalid port: %s", *cport)
		}
	}

	if config.Port == 0 {
		config.Port = 8000
	}

	err = startServices(config.Services, config.Port, config.Parallel)
	if err != nil {
		log.Fatalf("Failed to start services: %s", err)
	}

	htprox, wsprox, err = createProxies(config.Mappings)
	if err != nil {
		log.Fatalf("Invalid mapping: %s", err)
	}

	http.HandleFunc("/", forwarder)

	port := strconv.Itoa(config.Port)
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
		config.Port, err = strconv.Atoi(envport)
	} else if config.Port == 0 {
		config.Port = 8000
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

		if mapping.Path == "" {
			return nil, nil, fmt.Errorf("mapping path not found at element %d", i+1)
		}

		if mapping.Destination == "" {
			return nil, nil, fmt.Errorf("mapping destination not found at element %d", i+1)
		}

		if strings.Index(mapping.Destination, "{PORT") != -1 {
			for p, port := range ports {
				mapping.Destination = strings.Replace(mapping.Destination, "{PORT"+strconv.Itoa(p+1)+"}", port, -1)
			}
		}

		url, err = url.Parse(mapping.Destination)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid url %s: %s", mapping.Destination, err)
		}

		if url.Scheme == httpMapping {
			htprox[mapping.Path] = httputil.NewSingleHostReverseProxy(url)
		} else if url.Scheme == wsMapping {
			wsprox[mapping.Path] = wsutils.NewReverseProxy(url)
		} else {
			return nil, nil, fmt.Errorf("invalid mapping type %s for %s -> %s", url.Scheme, mapping.Path, mapping.Destination)
		}
	}

	return htprox, wsprox, err
}

func startServices(services []*Service, port int, parallel bool) error {
	var err error

	run := func(cmd *exec.Cmd) error {
		var err error
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err == nil {
			fmt.Printf("[started] %s\n", cmd.Path)
		} else {
			fmt.Printf("[failed] %s - %s\n", cmd.Path, err)
			return err
		}

		return err
	}

	for i, service := range services {
		var cmd *exec.Cmd

		if service.Cmd == "" {
			return fmt.Errorf("cmd must not be empty - service %d", i+1)
		}

		if service.Env != "" && strings.Index(service.Env, "{PORT}") != -1 {
			p := strconv.Itoa(port + i + 1)
			ports = append(ports, p)
			service.Env = strings.Replace(service.Env, "{PORT}", p, -1)

			if service.Args != "" {
				service.Args = strings.Replace(service.Args, "{PORT}", p, -1)
			}
		} else if service.Args != "" && strings.Index(service.Args, "{PORT}") != -1 {
			p := strconv.Itoa(port + i + 1)
			ports = append(ports, p)
			service.Args = strings.Replace(service.Args, "{PORT}", p, -1)
		}

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

		if parallel {
			go run(cmd)
		} else {
			err = run(cmd)
			if err != nil {
				return err
			}
		}
	}

	return err
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
