package main

import (
	"bytes"
	"net"
	"testing"
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
