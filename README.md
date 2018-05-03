# Gorexy

Gorexy is a very simple reverse proxy server for development environments. It is written in go and supports `http` and `ws` connections.

It is also able to start the services used for reverse proxy.

## Installation
Install using:

`go get -u github.com/fluxynet/gorexy`

## Arguments

Parameters | Default       | Description
-----------|---------------|--------------
`-conf`    | `gorexy.json` | Config file to use. May contain `~` (user home directory) or `$GOPATH`
`-port`    | `8000`        | Port where gorexy listens to. Takes precedence over `gorexy.json`

Parameters may be used as follows:

```
gorexy -conf=/path/to/myconfig.json
gorexy -port=1337
gorexy -conf=/path/to/myconfig.json -port=1337
```

## Configuration file
Configuration file must be in json format. Sample configuration file:

```json
{
    "services": [
        {
            "cmd": "myprogram",
            "dir": "$GOPATH/src/github.com/project",
            "env": "PORT={PORT1}"
        },
        {
            "cmd": "otherprogram"
        },
        {
            "cmd": "npm",
            "dir": "~/Projects/mynpm",
            "args": "run serve -- --port={PORT2}"
        }
    ],
    "mappings": [
        {
            "path": "/api",
            "destination": "http://localhost:{PORT1}"
        },
        {
            "path": "/admin",
            "destination": "http://localhost:9000"
        },
        {
            "path": "/",
            "destination": "http://localhost:{PORT2}"
        },
        {
            "path": "/",
            "destination": "ws://localhost:{PORT2}"
        }
    ],
    "port": 8000,
    "parallel": true
}
```

## Base configuration

Variable   | Default | Description
-----------|---------|---------------
`port`     | 8000    | Port where gorexy runs
`parallel` | true    | Whether or not services are started in parallel

## Service configuration

Variable   | Description
-----------|---------------
`cmd`      | **[Required]** The name of the executable to run. Must be present in `$PATH` or an absolute path to the executable or relative to `dir`
`dir`      | The directory to start the service from. If `cmd` is not found in `$PATH` and is not an absolute path, `cmd` will be relative to `dir`
`env`      | Environment variables for service; format is `VAR1=VAL1 VAR2=VAL2`
`args`     | Arguments to pass to service

**Note**

`cmd` and `dir` may include `~` (user home directory) or `$GOPATH`

## Mappings

Variable      | Description
--------------|---------------
`path`        | Path portion of url to be matched
`destination` | Destination url to forward to

**Notes**
1. Paths are matched sequentially using `HasPrefix` rule. `/api` will match any path starting with api whereas `/` will match all paths.
2. `destination` must start either with `http://` for http forwarding or `ws://` for websocket forwarding

## Ports

Services may have, zero, one or more dynamic port variables. Dynamic port variables can be  declared in `services` section simply by using the format `{PORTxxx}`, e.g. `{PORT1}`. A port will be assigned to the variable and substituted when used in `services` and `mappings` section. 


## Credits
Websocket information adapted from bradfitz and Fatih Arslan contributions on groups.google.com [thread](https://groups.google.com/forum/#!topic/golang-nuts/KBx9pDlvFOc).
