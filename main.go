package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	pollInterval = 300 * time.Millisecond
	maxMsgSize   = 50 * 1024 * 1024
)

type ClipboardContent struct {
	Type byte   // 'T' = text, 'I' = image (PNG)
	Data []byte
	Ts   int64 // origin unix-nano; used for last-writer-wins conflict resolution
}

var (
	lastHash    [32]byte
	lastContent *ClipboardContent // last synced clipboard, for catch-up push to reconnecting clients
	mu          sync.Mutex
)

// --- cross-restart state ---
// We persist {hash, ts} of whatever is currently in this machine's clipboard so
// a freshly-started process can recover the REAL timestamp of a copy made
// before it started. Without this, a restarted peer holding an old clipboard
// value would either (a) push nothing and get overwritten (losing a genuinely
// new copy made while it was down) or (b) stamp "now" and wrongly beat the
// other side's newer copy. The persisted ts disambiguates: on restart we read
// the OS clipboard, and if its hash still matches what we last saw, we recover
// that ts (stale — let the peer win); if it changed while we were down, it's a
// genuinely new copy and we stamp now (we win). See Test 2 vs Test 3.

type persistState struct {
	Hash string `json:"hash"`
	Ts   int64  `json:"ts"`
}

func statePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "clipsync-state.json")
	}
	return filepath.Join(home, ".clipsync-state.json")
}

func saveState(h [32]byte, ts int64) {
	b, err := json.Marshal(persistState{Hash: fmt.Sprintf("%x", h[:]), Ts: ts})
	if err != nil {
		return
	}
	os.WriteFile(statePath(), b, 0600)
}

func loadState() (persistState, bool) {
	b, err := os.ReadFile(statePath())
	if err != nil {
		return persistState{}, false
	}
	var s persistState
	if json.Unmarshal(b, &s) != nil {
		return persistState{}, false
	}
	return s, true
}

// pushSnapshot returns the clipboard content to send to a peer on (re)connect.
// Prefer the cached lastContent (the watch loop already consumed the OS change,
// so a fresh clipboardRead would return nil under the darwin change-count
// guard). On a fresh process lastContent is nil while the OS clipboard may hold
// a copy made before we started; read it directly and recover its ts from the
// persisted state (stale) or stamp now (changed while down) so last-writer-wins
// resolves correctly against the peer.
func pushSnapshot() *ClipboardContent {
	mu.Lock()
	snap := lastContent
	mu.Unlock()
	if snap != nil {
		return snap
	}
	content, err := clipboardRead()
	if err != nil || content == nil {
		return nil
	}
	if content.Type == 'T' {
		content.Data = bytes.TrimSpace(content.Data)
		if len(content.Data) == 0 {
			return nil
		}
	}
	h := sha256.Sum256(content.Data)
	ts := time.Now().UnixNano()
	if s, ok := loadState(); ok && s.Hash == fmt.Sprintf("%x", h[:]) {
		ts = s.Ts // clipboard unchanged since we last saw it — recover real ts
	}
	content.Ts = ts
	mu.Lock()
	lastHash = h
	lastContent = content
	mu.Unlock()
	return content
}

func main() {
	port := flag.Int("port", 9877, "port")
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 {
		runClient(args[0], *port)
	} else {
		runServer(*port)
	}
}

// --- server ---

func runServer(port int) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	log.Printf("serving on :%d", port)

	var clients []net.Conn
	var clMu sync.Mutex

	broadcast := func(content *ClipboardContent, exclude net.Conn) {
		clMu.Lock()
		defer clMu.Unlock()
		for _, c := range clients {
			if c != exclude {
				sendMsg(c, content)
			}
		}
	}

	remove := func(c net.Conn) {
		c.Close()
		clMu.Lock()
		for i, cc := range clients {
			if cc == c {
				clients = append(clients[:i], clients[i+1:]...)
				break
			}
		}
		clMu.Unlock()
		log.Printf("disconnected: %s", c.RemoteAddr())
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Println(err)
				continue
			}
			enableKeepAlive(conn)
			log.Printf("connected: %s", conn.RemoteAddr())

			// Push the last synced clipboard to the freshly-connected client
			// so anything copied on this (server) host while the client was
			// disconnected/asleep reaches it on reconnect. Mirrors the
			// catch-up push runClient does; without it, server->client
			// updates made during a disconnect are lost forever.
			if snap := pushSnapshot(); snap != nil {
				sendMsg(conn, snap)
			}

			clMu.Lock()
			clients = append(clients, conn)
			clMu.Unlock()

			go func(c net.Conn) {
				defer remove(c)
				recvClipboard(c, func(content *ClipboardContent) {
					broadcast(content, c)
				})
			}(conn)
		}
	}()

	watchClipboard(func(content *ClipboardContent) {
		broadcast(content, nil)
	})
}

// --- client ---

func runClient(host string, port int) {
	addr := fmt.Sprintf("%s:%d", host, port)
	for {
		log.Printf("connecting to %s", addr)
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			log.Printf("connect failed: %v (retry in 3s)", err)
			time.Sleep(3 * time.Second)
			continue
		}
		enableKeepAlive(conn)
		log.Println("connected")

		// Push the current clipboard so anything copied during the prior
		// disconnect (or while this process was down, or silently lost when the
		// last connection RST'd mid-frame) reaches the server. pushSnapshot
		// recovers the copy's real timestamp from persisted state, so the
		// server's last-writer-wins can't let a stale value revert a newer copy
		// (nor drop a genuinely new copy made while we were down). See Test 3.
		if snap := pushSnapshot(); snap != nil {
			sendMsg(conn, snap)
		}

		dead := make(chan struct{})
		go func() {
			recvClipboard(conn, nil)
			close(dead)
		}()

		watchClipboardUntil(dead, func(content *ClipboardContent) {
			sendMsg(conn, content)
		})

		conn.Close()
		log.Println("disconnected, reconnecting in 3s...")
		time.Sleep(3 * time.Second)
	}
}

// enableKeepAlive turns on TCP keepalive with a short idle so NAT/Tailscale
// doesn't silently drop the connection mid-session and lose clipboard updates.
func enableKeepAlive(conn net.Conn) {
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(30 * time.Second)
	}
}

// --- protocol: length-prefixed messages with type byte + timestamp ---
// Wire format: [10-byte length][1-byte type][8-byte big-endian unix-nano ts][payload]
// Length = payload size only (excludes type + ts bytes)

func sendMsg(conn net.Conn, content *ClipboardContent) {
	header := fmt.Sprintf("%010d%c", len(content.Data), content.Type)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(content.Ts))
	deadline := time.Duration(len(content.Data)/(1024*1024)+5) * time.Second
	conn.SetWriteDeadline(time.Now().Add(deadline))
	bufs := net.Buffers{[]byte(header), ts[:], content.Data}
	if _, err := bufs.WriteTo(conn); err != nil {
		log.Printf("send error: %v", err)
	}
}

func readMsg(conn net.Conn) (*ClipboardContent, error) {
	header := make([]byte, 10)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	var length int
	fmt.Sscanf(string(header), "%d", &length)
	if length > maxMsgSize {
		return nil, fmt.Errorf("message too large: %d", length)
	}
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, typeBuf); err != nil {
		return nil, err
	}
	if typeBuf[0] != 'T' && typeBuf[0] != 'I' {
		return nil, fmt.Errorf("unknown message type: %c", typeBuf[0])
	}
	tsBuf := make([]byte, 8)
	if _, err := io.ReadFull(conn, tsBuf); err != nil {
		return nil, err
	}
	ts := int64(binary.BigEndian.Uint64(tsBuf))
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	return &ClipboardContent{Type: typeBuf[0], Data: body, Ts: ts}, nil
}

// --- clipboard ---

func watchClipboard(onchange func(*ClipboardContent)) {
	watchClipboardUntil(nil, onchange)
}

func watchClipboardUntil(done <-chan struct{}, onchange func(*ClipboardContent)) {
	for {
		if done != nil {
			select {
			case <-done:
				return
			default:
			}
		}
		content, err := clipboardRead()
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		if content == nil {
			time.Sleep(pollInterval)
			continue
		}
		if content.Type == 'T' {
			content.Data = bytes.TrimSpace(content.Data)
			if len(content.Data) == 0 {
				time.Sleep(pollInterval)
				continue
			}
		}
		h := sha256.Sum256(content.Data)
		mu.Lock()
		changed := h != lastHash
		if changed {
			content.Ts = time.Now().UnixNano() // local copy: stamp now (authoritative)
			lastHash = h
			lastContent = content
		}
		mu.Unlock()
		if changed {
			saveState(h, content.Ts) // persist so a restart recovers this ts
			if onchange != nil {
				onchange(content)
			}
		}
		time.Sleep(pollInterval)
	}
}

func recvClipboard(conn net.Conn, also func(*ClipboardContent)) {
	for {
		content, err := readMsg(conn)
		if err != nil {
			log.Printf("connection lost: %v", err)
			return
		}
		if content.Type == 'T' {
			content.Data = bytes.TrimSpace(content.Data)
			if len(content.Data) == 0 {
				continue
			}
		}
		h := sha256.Sum256(content.Data)
		mu.Lock()
		var curTs int64
		if lastContent != nil {
			curTs = lastContent.Ts
		}
		// Last-writer-wins: ignore content we already hold (same hash) or that
		// is older than what we have. This stops a reconnecting peer's stale
		// clipboard from reverting a newer copy made on this side.
		stale := h == lastHash || content.Ts < curTs
		if !stale {
			lastHash = h
			lastContent = content
		}
		mu.Unlock()
		if stale {
			continue
		}
		saveState(h, content.Ts) // persist remote ts so a restart recovers it
		if err := clipboardWrite(content); err != nil {
			log.Printf("clipboard write error: %v", err)
		}
		if also != nil {
			also(content)
		}
	}
}
