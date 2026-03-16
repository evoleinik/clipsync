package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
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
}

var (
	lastHash [32]byte
	mu       sync.Mutex
)

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
			log.Printf("connected: %s", conn.RemoteAddr())
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
		log.Println("connected")

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

// --- protocol: length-prefixed messages with type byte ---
// Wire format: [10-byte length][1-byte type][payload]
// Length = payload size only (excludes type byte)

func sendMsg(conn net.Conn, content *ClipboardContent) {
	header := fmt.Sprintf("%010d%c", len(content.Data), content.Type)
	deadline := time.Duration(len(content.Data)/(1024*1024)+5) * time.Second
	conn.SetWriteDeadline(time.Now().Add(deadline))
	bufs := net.Buffers{[]byte(header), content.Data}
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
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	return &ClipboardContent{Type: typeBuf[0], Data: body}, nil
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
			lastHash = h
		}
		mu.Unlock()
		if changed && onchange != nil {
			onchange(content)
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
		lastHash = h
		mu.Unlock()
		if err := clipboardWrite(content); err != nil {
			log.Printf("clipboard write error: %v", err)
		}
		if also != nil {
			also(content)
		}
	}
}
