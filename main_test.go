package main

import (
	"bytes"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendReadText(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	want := &ClipboardContent{Type: 'T', Data: []byte("hello world")}
	go sendMsg(c, want)

	got, err := readMsg(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != 'T' || !bytes.Equal(got.Data, want.Data) {
		t.Errorf("got type=%c data=%q, want type=T data=%q", got.Type, got.Data, want.Data)
	}
}

func TestSendReadUTF8(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	want := &ClipboardContent{Type: 'T', Data: []byte("Hello 世界 — emoji 🎉")}
	go sendMsg(c, want)

	got, err := readMsg(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, want.Data) {
		t.Errorf("got %q, want %q", got.Data, want.Data)
	}
}

func TestSendReadImage(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	// Fake PNG header with whitespace-like bytes that must not be trimmed
	want := &ClipboardContent{Type: 'I', Data: []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x20, 0x09, 0x00}}
	go sendMsg(c, want)

	got, err := readMsg(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != 'I' || !bytes.Equal(got.Data, want.Data) {
		t.Errorf("got type=%c len=%d, want type=I len=%d", got.Type, len(got.Data), len(want.Data))
	}
}

func TestSendReadLarge(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	want := &ClipboardContent{Type: 'I', Data: data}
	go sendMsg(c, want)

	got, err := readMsg(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != 'I' || !bytes.Equal(got.Data, want.Data) {
		t.Errorf("large: type=%c len=%d, want type=I len=%d", got.Type, len(got.Data), len(want.Data))
	}
}

func TestReadMsgTooLarge(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	go c.Write([]byte("9999999999"))

	_, err := readMsg(s)
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
}

func TestReadMsgBadType(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	go c.Write([]byte("0000000005Xhello"))

	_, err := readMsg(s)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestSendReadPing(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	go sendMsg(c, pingMsg())

	got, err := readMsg(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != 'P' || len(got.Data) != 0 {
		t.Errorf("got type=%c len=%d, want type=P len=0", got.Type, len(got.Data))
	}
}

// A heartbeat proves the link is alive and nothing else: it must never be
// mistaken for clipboard content, or every ping would overwrite the clipboard
// with an empty value and be rebroadcast to the other peers.
func TestRecvClipboardIgnoresPing(t *testing.T) {
	s, c := net.Pipe()
	defer c.Close()

	mu.Lock()
	lastHash = [32]byte{}
	lastContent = nil
	mu.Unlock()

	var forwarded int
	done := make(chan struct{})
	go func() {
		recvClipboard(s, func(*ClipboardContent) { forwarded++ })
		close(done)
	}()

	sendMsg(c, pingMsg())
	sendMsg(c, pingMsg())
	s.Close()
	<-done

	if forwarded != 0 {
		t.Errorf("heartbeat treated as clipboard content and forwarded %d time(s)", forwarded)
	}
}

// Regression: a peer whose network path dies silently (Tailscale rebind, closed
// laptop) leaves a socket that never errors and never delivers. Without a read
// deadline recvClipboard blocks on it forever, so the connection is never torn
// down or re-established -- measured 2026-08-05 as up to 15 min of copies not
// crossing between air and pro, with nothing logged on either side.
func TestRecvClipboardGivesUpOnSilentPeer(t *testing.T) {
	defer func(d time.Duration) { readTimeout = d }(readTimeout)
	readTimeout = 100 * time.Millisecond

	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	done := make(chan struct{})
	go func() {
		recvClipboard(s, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recvClipboard is still waiting on a silent peer: the read deadline is not applied")
	}
}

// The flip side: the deadline must be refreshed by every message, heartbeats
// included, or a healthy but idle link would be torn down on a timer.
func TestHeartbeatKeepsConnectionAlive(t *testing.T) {
	defer func(d time.Duration) { readTimeout = d }(readTimeout)
	readTimeout = 200 * time.Millisecond

	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	done := make(chan struct{})
	go func() {
		recvClipboard(s, nil)
		close(done)
	}()

	for i := 0; i < 10; i++ { // 600ms of traffic, 3x the deadline
		sendMsg(c, pingMsg())
		time.Sleep(60 * time.Millisecond)
	}

	select {
	case <-done:
		t.Fatal("connection dropped while heartbeats were flowing")
	default:
	}
}

// The watcher loop is what sends heartbeats on a live link. Without this test
// the loop could stop calling tick and every other test would still pass,
// while real peers time each other out every readTimeout.
func TestWatchClipboardCallsTick(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep saveState off the real state file
	defer func(d time.Duration) { heartbeatInterval = d }(heartbeatInterval)
	heartbeatInterval = 10 * time.Millisecond

	var ticks int32
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		watchClipboardUntil(done, nil, func() { atomic.AddInt32(&ticks, 1) })
		close(finished)
	}()
	time.Sleep(4 * pollInterval)
	close(done)
	<-finished

	if n := atomic.LoadInt32(&ticks); n < 2 {
		t.Fatalf("tick called %d times in %v, want at least 2", n, 4*pollInterval)
	}
}
