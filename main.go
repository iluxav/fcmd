package main

import (
	"flag"
	"fmt"
	"os"

	"fcmd/internal/daemon"
	"fcmd/internal/proto"
	"fcmd/internal/tui"
)

const version = "0.1.0"

func usage() {
	fmt.Fprintf(os.Stderr, `fcmd %s — LAN file commander

Usage:
  fcmd            Launch the dual-pane TUI
  fcmd run        Run the fcmd daemon (advertise on mDNS, serve file ops)
  fcmd version    Print version and exit

Daemon flags (after "run"):
  -port N         TCP port to listen on (default %d)
`, version, proto.DefaultPort)
}

func main() {
	if len(os.Args) < 2 {
		if err := tui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	switch os.Args[1] {
	case "run":
		fs := flag.NewFlagSet("run", flag.ExitOnError)
		port := fs.Int("port", proto.DefaultPort, "TCP port")
		_ = fs.Parse(os.Args[2:])
		if err := daemon.Run(daemon.Options{Port: *port}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "version", "-v", "--version":
		fmt.Println(version)
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}
