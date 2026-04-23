// smoketest exercises the fcmd daemon over loopback.
// Not built into fcmd; run via: go run ./cmd/smoketest
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"fcmd/internal/discovery"
	"fcmd/internal/proto"
)

func main() {
	c, err := proto.Dial("127.0.0.1:7891")
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	roots, err := c.Roots()
	if err != nil {
		log.Fatalf("roots: %v", err)
	}
	fmt.Printf("roots: %+v\n", roots)

	tmp, err := os.MkdirTemp("", "fcmd-smoke-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	target := filepath.Join(tmp, "hello.txt")
	payload := bytes.Repeat([]byte("abcdefghij"), 10_000) // 100KB

	if err := c.WriteFile(target, int64(len(payload)), bytes.NewReader(payload), nil); err != nil {
		log.Fatalf("write: %v", err)
	}
	fmt.Println("wrote", target)

	entries, err := c.List(tmp)
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	fmt.Println("list:")
	for _, e := range entries {
		fmt.Printf("  %-20s %d dir=%v\n", e.Name, e.Size, e.IsDir)
	}

	var buf bytes.Buffer
	if _, err := c.ReadFile(target, &buf, nil); err != nil {
		log.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		log.Fatalf("read mismatch: got %d bytes want %d", buf.Len(), len(payload))
	}
	fmt.Println("read round-trip OK")

	sub := filepath.Join(tmp, "sub")
	if err := c.Mkdir(sub); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	if err := c.Rename(target, filepath.Join(sub, "moved.txt")); err != nil {
		log.Fatalf("rename: %v", err)
	}
	if err := c.Delete(sub, true); err != nil {
		log.Fatalf("delete: %v", err)
	}
	fmt.Println("mkdir/rename/delete OK")

	entries, err = c.List(tmp)
	if err != nil {
		log.Fatalf("list-after: %v", err)
	}
	if len(entries) != 0 {
		log.Fatalf("expected empty dir, got %d entries", len(entries))
	}
	fmt.Println("cleanup verified")

	if _, err := c.ReadFile(filepath.Join(tmp, "nope"), io.Discard, nil); err == nil {
		log.Fatal("expected error reading missing file")
	}
	fmt.Println("error path OK")

	// Check mDNS discovery (may be silently empty on some networks).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	hosts, err := discovery.Browse(ctx, 2500*time.Millisecond)
	if err != nil {
		fmt.Println("mdns browse error (non-fatal):", err)
		return
	}
	fmt.Printf("mdns found %d host(s):\n", len(hosts))
	for _, h := range hosts {
		fmt.Printf("  %s @ %s\n", h.Name, h.Endpoint())
	}
}
