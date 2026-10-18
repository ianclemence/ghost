package egress

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
)

// BridgeAddr is where the proxy appears inside the sandbox's private network.
const BridgeAddr = "127.0.0.1:3128"

// RunBridge is `ghost __egress-exec <socket> -- cmd args...`, run inside the
// sandbox. The sandbox has no network, only a loopback; this listens on it,
// carries every connection to the host proxy over the unix socket, and runs the
// command with its proxy variables pointing here. It returns the exit code.
func RunBridge(sock string, argv []string) int {
	if len(argv) == 0 {
		return 2
	}
	ln, err := net.Listen("tcp", BridgeAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "egress: cannot listen inside the sandbox:", err)
		return 126
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				up, err := net.Dial("unix", sock)
				if err != nil {
					return
				}
				defer up.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(up, c); done <- struct{}{} }()
				go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
				<-done
			}()
		}
	}()

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	proxy := "http://" + BridgeAddr
	cmd.Env = append(os.Environ(),
		"HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy, "ALL_PROXY="+proxy,
		"http_proxy="+proxy, "https_proxy="+proxy, "all_proxy="+proxy,
		"NO_PROXY=", "no_proxy=")
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 127
	}
	return 0
}
