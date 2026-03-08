package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
)

const pollInterval = 300 * time.Millisecond

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

	broadcast := func(data string, exclude net.Conn) {
		clMu.Lock()
		defer clMu.Unlock()
		for _, c := range clients {
			if c != exclude {
				sendMsg(c, data)
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
				recvClipboard(c, func(data string) {
					broadcast(data, c)
				})
			}(conn)
		}
	}()

	watchClipboard(func(data string) {
		broadcast(data, nil)
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

		// recv in background; when it returns, connection is dead
		dead := make(chan struct{})
		go func() {
			recvClipboard(conn, nil)
			close(dead)
		}()

		// watch clipboard, send until connection dies
		go func() {
			for {
				select {
				case <-dead:
					return
				default:
				}
				text, err := clipboard.ReadAll()
				if err != nil {
					time.Sleep(pollInterval)
					continue
				}
				text = strings.TrimSpace(text)
				if text == "" {
					time.Sleep(pollInterval)
					continue
				}
				h := sha256.Sum256([]byte(text))
				mu.Lock()
				changed := h != lastHash
				if changed {
					lastHash = h
				}
				mu.Unlock()
				if changed {
					sendMsg(conn, text)
				}
				time.Sleep(pollInterval)
			}
		}()

		<-dead
		conn.Close()
		log.Println("disconnected, reconnecting in 3s...")
		time.Sleep(3 * time.Second)
	}
}

// --- protocol: length-prefixed messages ---

func sendMsg(conn net.Conn, data string) {
	msg := []byte(data)
	header := fmt.Sprintf("%010d", len(msg))
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(append([]byte(header), msg...)); err != nil {
		log.Printf("send error: %v", err)
	}
}

func readMsg(conn net.Conn) (string, error) {
	header := make([]byte, 10)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}
	var length int
	fmt.Sscanf(string(header), "%d", &length)
	if length > 10*1024*1024 {
		return "", fmt.Errorf("message too large: %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return "", err
	}
	return string(body), nil
}

// --- clipboard ---

func watchClipboard(onchange func(string)) {
	for {
		text, err := clipboard.ReadAll()
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			time.Sleep(pollInterval)
			continue
		}
		h := sha256.Sum256([]byte(text))
		mu.Lock()
		changed := h != lastHash
		if changed {
			lastHash = h
		}
		mu.Unlock()
		if changed && onchange != nil {
			onchange(text)
		}
		time.Sleep(pollInterval)
	}
}

func recvClipboard(conn net.Conn, also func(string)) {
	for {
		msg, err := readMsg(conn)
		if err != nil {
			log.Printf("connection lost: %v", err)
			return
		}
		msg = strings.TrimSpace(msg)
		if msg == "" {
			continue
		}
		h := sha256.Sum256([]byte(msg))
		mu.Lock()
		lastHash = h
		mu.Unlock()
		clipboard.WriteAll(msg)
		if also != nil {
			also(msg)
		}
	}
}
