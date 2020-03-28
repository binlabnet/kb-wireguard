package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/zapu/kb-wireguard/devowner"
	"github.com/zapu/kb-wireguard/libpipe"
	"github.com/zapu/kb-wireguard/libwireguard"
)

/*
ip link add dev kbwg0 type wireguard

ip address add dev kbwg0 101.0.0.1/24



keys:
wg genkey
wg pubkey < $(priv)



conf:

wg setconf kbwg0 kbwg0.conf

wg syncconf kbwg0 kbwg0.conf


*/

const deviceName = "kbwg0"

func sudoExec(name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cmdStr := fmt.Sprintf("%s %s", name, strings.Join(args, " "))
		fmt.Fprintf(os.Stderr, "Command %q stderr:\n%s\n", cmdStr, stderr.String())
		return nil, fmt.Errorf("exec %q: %w", cmdStr, err)
	}
	return stdout.Bytes(), nil
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(3)
}

func serializeToStdout(id string, payload interface{}) {
	msg, err := libpipe.SerializeMsgInterface(id, payload)
	if err != nil {
		fail("libpipe fail: %s", err)
	}

}

func debug(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	debug(`Hello from device runner ("run-dev"): %d %d`, os.Getuid(), os.Geteuid())
	if os.Getuid() != 0 {
		fail("Needs to run as root to control wireguard...")
	}

	privKey, pubKey, err := devowner.WireguardGenKey()
	if err != nil {
		fail("%s", err)
	}

	debug(":: Priv key: %s", privKey)
	debug(":: Pub key: %s", pubKey)

	serializeToStdout("pubkey", pubKey)

	var conf libwireguard.WireguardConfig
	conf.PrivateKey = privKey
	conf.ListenPort = 51820

	tmpfile, err := ioutil.TempFile("", fmt.Sprintf("%s.*.conf", deviceName))
	if err != nil {
		fail("%s", err)
	}

	debug(":: Config filename: %s", tmpfile.Name())
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(libwireguard.SerializeConfig(conf))); err != nil {
		fail("%s", err)
	}
	if err := tmpfile.Close(); err != nil {
		fail("%s", err)
	}

	debug("Setting up device %s", deviceName)

	_, err = sudoExec("ip", "link", "add", "dev", deviceName, "type", "wireguard")
	if err != nil {
		fail("%s", err)
	}

	_, err = sudoExec("wg", "setconf", "kbwg0", tmpfile.Name())
	if err != nil {
		// fail("%s", err)
		debug("Failed to setconf: %s", err)
	}

	ipAddr := "101.0.0.1/24"
	_, err = sudoExec("ip", "address", "add", "dev", deviceName, ipAddr)
	if err != nil {
		debug("Failed to set ip: %s", err)
	} else {
		debug("Set ip address to %s", ipAddr)
	}

loop:
	for {
		select {
		case <-sigs:
			debug("Stopping on signal...")
			break loop
		}
	}

	debug("Removing device %s", deviceName)

	_, err = sudoExec("ip", "link", "delete", "dev", deviceName)
	if err != nil {
		fail("%s", err)
	}
}