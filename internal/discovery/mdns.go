package discovery

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"

	"fcmd/internal/proto"
)

type Host struct {
	Name string
	Host string
	Addr string
	Port int
}

func (h Host) Endpoint() string {
	return net.JoinHostPort(h.Addr, fmt.Sprintf("%d", h.Port))
}

// Register advertises this daemon on the LAN and returns a shutdown func.
func Register(port int) (func(), error) {
	host, _ := os.Hostname()
	if host == "" {
		host = "fcmd"
	}
	server, err := zeroconf.Register(
		host,
		proto.ServiceName,
		"local.",
		port,
		[]string{"version=" + proto.ProtoVersion},
		nil,
	)
	if err != nil {
		return nil, err
	}
	return func() { server.Shutdown() }, nil
}

// Browse runs an mDNS query for the given window and returns all unique hosts discovered.
func Browse(ctx context.Context, window time.Duration) ([]Host, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, err
	}
	entries := make(chan *zeroconf.ServiceEntry, 16)

	browseCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()

	var mu sync.Mutex
	seen := map[string]Host{}

	done := make(chan struct{})
	go func() {
		for e := range entries {
			addr := ""
			if len(e.AddrIPv4) > 0 {
				addr = e.AddrIPv4[0].String()
			} else if len(e.AddrIPv6) > 0 {
				addr = e.AddrIPv6[0].String()
			}
			if addr == "" {
				continue
			}
			h := Host{
				Name: e.Instance,
				Host: e.HostName,
				Addr: addr,
				Port: e.Port,
			}
			mu.Lock()
			seen[h.Endpoint()] = h
			mu.Unlock()
		}
		close(done)
	}()

	if err := resolver.Browse(browseCtx, proto.ServiceName, "local.", entries); err != nil {
		return nil, err
	}
	<-browseCtx.Done()
	<-done

	mu.Lock()
	out := make([]Host, 0, len(seen))
	for _, h := range seen {
		out = append(out, h)
	}
	mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
