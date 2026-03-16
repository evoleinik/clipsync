package main

import "os/exec"

func pbCmd(name string) *exec.Cmd {
	cmd := exec.Command(name)
	cmd.Env = []string{"LANG=en_US.UTF-8"}
	return cmd
}

func clipboardRead() (string, error) {
	out, err := pbCmd("pbpaste").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func clipboardWrite(text string) error {
	cmd := pbCmd("pbcopy")
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
