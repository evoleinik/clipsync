package main

import (
	"os"
	"os/exec"
)

func clipboardRead() (string, error) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		out, err := exec.Command("wl-paste", "--no-newline").Output()
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
	out, err := exec.Command("xclip", "-out", "-selection", "clipboard", "-target", "UTF8_STRING").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func clipboardWrite(text string) error {
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
	if _, err := in.Write([]byte(text)); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}
	return cmd.Wait()
}
