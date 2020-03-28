package kbwg

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/zapu/kb-wireguard/libpipe"
	"github.com/zapu/kb-wireguard/libwireguard"
)

// Run process that sets up and "owns" the wireguard device, and also tears it
// down when it exits.

type DevRunnerProcess struct {
	DoneCh  chan struct{}
	Process *os.Process

	PubKeyCh chan libwireguard.WireguardPubKey

	PipeWriter *bufio.Writer

	cmd *exec.Cmd
}

func makePipe() (string, error) {
	f, err := ioutil.TempFile("", "*.pipe.tmp")
	if err != nil {
		return "", err
	}
	name := f.Name()
	err = f.Close()
	if err != nil {
		return "", err
	}
	err = os.Remove(name)
	if err != nil {
		return "", err
	}
	err = syscall.Mkfifo(name, 0600)
	if err != nil {
		return "", err
	}
	return name, err
}

func RunDevRunner() (ret *DevRunnerProcess, err error) {
	ret = &DevRunnerProcess{}
	ret.DoneCh = make(chan struct{})
	ret.PubKeyCh = make(chan libwireguard.WireguardPubKey)

	wrPipeFilename, err := makePipe()
	if err != nil {
		return nil, fmt.Errorf("Failed to make pipe: %w", err)
	}

	cmd := exec.Command("sudo", "./run-dev", "-pipe", wrPipeFilename)
	ret.cmd = cmd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdoutReader := bufio.NewReader(stdout)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stderrReader := bufio.NewReader(stderr)

	cmd.Stdin = os.Stdin

	go func() {
		for {
			line, err := stdoutReader.ReadBytes('\n')
			if err != nil || len(line) == 0 {
				continue
			}
			var msg libpipe.PipeMsg
			err = json.Unmarshal(line, &msg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to unmarshall from RunDev: %s", err)
				continue
			}
			err = ret.handleDevRunnerControlMsg(msg)
			if err != nil {

			}
		}
	}()

	go func() {
		for {
			line, err := stderrReader.ReadBytes('\n')
			if err != nil || len(line) == 0 {
				continue
			}
			fmt.Printf("[RunDev]: %s\n", strings.TrimRight(string(line), "\n"))

			// TODO: Push these through channel as well
		}
	}()

	go func() {
		select {
		case <-ret.DoneCh:
			fmt.Printf("[xx] Sending SIGTERM to device owner\n")
			cmd.Process.Signal(syscall.SIGTERM)
		}
	}()

	cmd.Start()
	ret.Process = cmd.Process

	go func() {
		fd, err := os.OpenFile(wrPipeFilename, os.O_WRONLY, os.ModeNamedPipe)
		if err != nil {
			fmt.Printf("Failed to open pipe: %s\n", err)
			return
		}
		fmt.Printf("[%%] Opened write side of pipe: %s\n", wrPipeFilename)
		ret.PipeWriter = bufio.NewWriter(fd)
	}()

	return ret, nil
}

func (runner *DevRunnerProcess) handleDevRunnerControlMsg(msg libpipe.PipeMsg) error {
	if msg.ID == "pubkey" {
		pubkey := libwireguard.WireguardPubKey(msg.Payload)
		fmt.Printf("Received pub key from device runner: %s\n", pubkey)
		runner.PubKeyCh <- pubkey
	}
	return nil
}

func (runner *DevRunnerProcess) WriteLine(str string) {
	runner.PipeWriter.WriteString(str + "\n")
	runner.PipeWriter.Flush()
}
