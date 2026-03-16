# clipsync

Clipboard sync over TCP. Zero dependencies, zero config.

Copy text or screenshots on one machine, paste on another. Built for Tailscale — stable IPs mean no discovery protocol needed. Works on any network where machines can reach each other.

## Why not X?

| Tool | Problem |
|------|---------|
| Apple Handoff | Unreliable, WiFi-only, Apple-only |
| uniclip | Random port on each start — can't automate |
| clipboard-sync | 30+ deps (Fyne GUI framework) for a tray icon |
| ClipCascade | Requires Docker |

clipsync: ~200 lines of Go, zero external dependencies, fixed port. Uses CGo + AppKit on macOS for native clipboard access.

## Install

Requires Xcode command-line tools (for CGo/AppKit):

```
xcode-select --install
```

Build from source:

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
- Clipboard access via NSPasteboard (macOS) — uses `changeCount` for efficient change detection
- Changes are broadcast as type-prefixed, length-prefixed messages (`T` for text, `I` for image)
- Images (PNG, TIFF screenshots) are auto-converted to PNG for transfer
- Client auto-reconnects on disconnect (3s retry)
- SHA-256 dedup prevents echo loops

## Limitations

- Text and PNG images only (no files or rich text)
- Image support is macOS only (Linux nodes sync text, skip received images)
- No encryption (use Tailscale or SSH tunnel for untrusted networks)
- Polls at 300ms (not event-driven — keeps the code simple)

## License

MIT
