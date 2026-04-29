# ifrit

A command-line tool for monitoring network ports and managing processes.

> [!NOTE]
> This is a learning project -- built while picking up Go. Contributions and feedback are welcome.

## Install

Requires [Go](https://go.dev/dl/) 1.25+.

```
go install github.com/rizqishq/ifrit@latest
```

## Usage

### Watch (interactive TUI)

```
ifrit watch
```

Real-time dashboard that auto-refreshes every 2 seconds.

| Key | Action |
|-----|--------|
| `j`/`k` or arrows | Navigate |
| `x` | Kill process (with confirmation) |
| `s` | Cycle sort (port, PID, status, process) |
| `/` | Filter |
| `r` | Refresh |
| `q` | Quit |

```
ifrit watch --interval 5
```

### List connections

```
ifrit list
```

```
PID      PORT    PROTO   STATUS         PROCESS              USER
────────────────────────────────────────────────────────────────────────
1234     8080    TCP     LISTEN         nginx                www-data
5678     3000    TCP     LISTEN         node                 dev
9012     5432    TCP     ESTABLISHED    postgres             postgres

Total: 3 connections
```

Filter by protocol, state, or port:

```
ifrit list --proto tcp
ifrit list --state LISTEN
ifrit list --port 8080
ifrit list --state ESTABLISHED --proto tcp
```

Output as JSON or CSV:

```
ifrit list --format json
ifrit list --format csv
ifrit list --state LISTEN --format json
```

### Kill a process

```
ifrit kill 1234
ifrit kill 1234 --force
```

Without `--force`, sends SIGTERM. With `--force`, sends SIGKILL.

### Scan ports

```
ifrit scan localhost
ifrit scan 192.168.1.1 --ports 80-443
ifrit scan example.com --ports 22-80 --timeout 200
```

Default range is 1-1024. Timeout is in milliseconds (default 500).

## Permissions

Some connections (owned by root or the kernel) will show PID `0` and process `-`. Run with `sudo` to see full process information:

```
sudo ifrit list
```

## License

[MIT](LICENSE)
