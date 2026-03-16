package main

import (
	"log"
	"os"
	"os/exec"
)

func clipboardRead() (*ClipboardContent, error) {
	var out []byte
	var err error
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		out, err = exec.Command("wl-paste", "--no-newline").Output()
	} else {
		out, err = exec.Command("xclip", "-out", "-selection", "clipboard", "-target", "UTF8_STRING").Output()
	}
	if err != nil {
		return nil, err
	}
	return &ClipboardContent{Type: 'T', Data: out}, nil
}

func clipboardWrite(content *ClipboardContent) error {
	if content.Type == 'I' {
		log.Println("image clipboard not supported on Linux, skipping")
		return nil
	}
	var cmd *exec.Cmd
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cmd = exec.Command("wl-copy")
	} else {
		cmd = exec.Command("xclip", "-in", "-selection", "clipboard", "-target", "UTF8_STRING")
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := in.Write(content.Data); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}
	return cmd.Wait()
}
