package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// sockServe accepts AF_UNIX connections as mygo's single-instance lock does, with no MyGo in
// the process, so `handles` can show what serving them costs. sockDial is the second
// instance's side.
func sockServe(args []string) error {
	fs := flag.NewFlagSet("sockserve", flag.ExitOnError)
	sock := fs.String("sock", filepath.Join(os.TempDir(), "glintcheck.sock"), "socket path")
	life := fs.Duration("for", 10*time.Minute, "how long to serve")
	fs.Parse(args)
	os.Remove(*sock)
	ln, err := net.Listen("unix", *sock)
	if err != nil {
		return err
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				io.ReadAll(io.LimitReader(conn, 1<<20))
			}()
		}
	}()
	fmt.Println(os.Getpid())
	time.Sleep(*life)
	return nil
}

func sockDial(args []string) error {
	fs := flag.NewFlagSet("sockdial", flag.ExitOnError)
	sock := fs.String("sock", filepath.Join(os.TempDir(), "glintcheck.sock"), "socket path")
	fs.Parse(args)
	c, err := net.DialTimeout("unix", *sock, time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte(`{"args":["--library"]}`)); err != nil {
		return err
	}
	c.(*net.UnixConn).CloseWrite()
	_, err = io.Copy(io.Discard, c)
	return err
}
