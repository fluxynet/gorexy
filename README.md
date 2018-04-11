# Gorexy

Gorexy is a very simple reverse proxy server for development environments. It is written in go and supports `http` and `ws` connections.

## Configuration

Environment Variable | Default       | Description
---------------------|---------------|--------------
`CONF`               | `gorexy.json` | Config file to use
`PORT`               | `1337`        | Port where gorexy listens to. Takes precedence over `gorexy.json`

Environment variables may be used as follows:

```
CONF=myconfig.json gorexy
PORT=1337 gorexy
PORT=1337 CONF=myconfig.json gorexy
```

## Config file
Configuration file must be in json format. Sample configuration file:

```json
{
    "mappings": [
        {
            "path": "/api", //destination to reverse proxy from
            "destination": "http://localhost:8888" //url to reverse proxy to
        },
        {
            "path": "/", //different path
            "destination": "http://localhost:8080"
        },
        {
            "path": "/",
            "destination": "ws://localhost:8080" //use websocket
        }
    ],
    "port": 8000 //port that gorexy binds to
}
```

Paths are matched sequentially using `HasPrefix` rule. `/api` will match any path starting with api whereas `/` will match all paths.

## Credits
Websocket information adapted from bradfitz and Fatih Arslan contributions on groups.google.com [thread](https://groups.google.com/forum/#!topic/golang-nuts/KBx9pDlvFOc).