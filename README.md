# clipsync

Clipboard sync over TCP. One binary, one dependency, zero config.

Copy on one machine, paste on another. Built for Tailscale — stable IPs mean no discovery protocol needed. Works on any network where machines can reach each other.

## Why not X?

| Tool | Problem |
|------|---------|
| Apple Handoff | Unreliable, WiFi-only, Apple-only |
| uniclip | Random port on each start — can't automate |
| clipboard-sync | 30+ deps (Fyne GUI framework) for a tray icon |
| ClipCascade | Requires Docker |

clipsync: 150 lines of Go, one dependency (`atotto/clipboard`), fixed port.

## Install

```
go install github.com/evoleinik/clipsync@latest
```

Or build from source:

```
git clone https://github.com/evoleinik/clipsync.git
cd clipsync && go build -o clipsync .
```

## Usage

Server (on your main machine):

```
clipsync
```

Client (on other machines):

```
clipsync <hostname-or-ip>
```

That's it. Default port is 9877. Change with `-port`.

## With Tailscale

Tailscale hostnames are stable, so setup is trivial:

```
# machine-a (server)
clipsync

# machine-b (client)
clipsync machine-a
```

Works from anywhere — home, office, coffee shop. No LAN required.

## Auto-start (macOS)

Server (`~/Library/LaunchAgents/com.clipsync.plist`):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.clipsync</string>
  <key>ProgramArguments</key>
  <array><string>/path/to/clipsync</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
```

Client — same, but add the server hostname to `ProgramArguments`.

## How it works

- Server listens on a fixed TCP port
- Clients connect and stay connected
- Both sides poll the clipboard every 300ms
- Changes are broadcast as length-prefixed messages
- Client auto-reconnects on disconnect (3s retry)
- SHA-256 dedup prevents echo loops

## Limitations

- Text only (no images/files — covers 99% of clipboard use)
- No encryption (use Tailscale or SSH tunnel for untrusted networks)
- Polls at 300ms (not event-driven — keeps the code simple)

## License

MIT
