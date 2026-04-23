package daemon

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"

	"fcmd/internal/discovery"
	"fcmd/internal/proto"
)

type Options struct {
	Port int
}

// Run starts the daemon: mDNS advertisement + TCP server. Blocks until the listener closes.
func Run(opts Options) error {
	port := opts.Port
	if port == 0 {
		port = proto.DefaultPort
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	defer ln.Close()

	stop, err := discovery.Register(port)
	if err != nil {
		log.Printf("warning: mDNS register failed: %v", err)
	} else {
		defer stop()
	}

	srv := &proto.Server{
		Listener: ln,
		Roots:    defaultRoots,
	}
	log.Printf("fcmd daemon listening on %s", ln.Addr())
	return srv.Serve()
}

// defaultRoots exposes a user-friendly set of top-level directories.
func defaultRoots() []proto.Root {
	home, _ := os.UserHomeDir()
	out := []proto.Root{}
	if home != "" {
		out = append(out, proto.Root{Name: "Home", Path: home})
	}
	if runtime.GOOS == "windows" {
		if sysDrive := os.Getenv("SystemDrive"); sysDrive != "" {
			out = append(out, proto.Root{Name: sysDrive + `\`, Path: sysDrive + `\`})
		}
	} else {
		out = append(out, proto.Root{Name: "/", Path: "/"})
		out = append(out, proto.Root{Name: "/tmp", Path: "/tmp"})
	}
	if home != "" {
		docs := filepath.Join(home, "Documents")
		if st, err := os.Stat(docs); err == nil && st.IsDir() {
			out = append(out, proto.Root{Name: "Documents", Path: docs})
		}
	}
	return out
}
