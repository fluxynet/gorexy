# Gorexy

Gorexy is a very simple go http reverse proxy server for development environments.

## Configuration

Environment Variable | Default      | Description
---------------------|--------------|--------------
`PORT`               | `1337`       | Port where gorexy listens to
`MAPPING`            | `gorexy.cfg` | Mapping file to used

## Mapping
Sample mapping

```
/admin http://localhost:8888
/ http://localhost:8080
```

Each line must have the format `[path] [target]`.
