package ssh

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

type SessionRequest struct {
	Host          string
	Port          int
	User          string
	PrivateKeyPEM []byte
	HostKey       ssh.PublicKey
}

func BridgeWebSocketToSSH(ws *websocket.Conn, req SessionRequest) error {
	signer, err := ssh.ParsePrivateKey(req.PrivateKeyPEM)
	if err != nil {
		return fmt.Errorf("invalid private key: %w", err)
	}

	hostKeyCallback := ssh.InsecureIgnoreHostKey()
	if req.HostKey != nil {
		hostKeyCallback = ssh.FixedHostKey(req.HostKey)
	}

	config := &ssh.ClientConfig{
		User:            req.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(req.Host, fmt.Sprintf("%d", req.Port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("ssh dial failed: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session failed: %w", err)
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 40, 120, modes); err != nil {
		return err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}

	if err := session.Shell(); err != nil {
		return err
	}

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				_ = ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
			if err != nil {
				if err != io.EOF {
					_ = ws.Close()
				}
				return
			}
		}
	}()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if _, err := stdin.Write(msg); err != nil {
			break
		}
	}
	return nil
}
