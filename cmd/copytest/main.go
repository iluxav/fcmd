// copytest exercises vfs.Copy across Local<->Remote in all four directions.
// Run: go run ./cmd/copytest (requires fcmd daemon on 127.0.0.1:7891)
package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"fcmd/internal/vfs"
)

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func seedTree(root string) int64 {
	must(os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	var total int64
	write := func(p string, n int) {
		b := make([]byte, n)
		_, _ = rand.Read(b)
		must(os.WriteFile(p, b, 0o644))
		total += int64(n)
	}
	write(filepath.Join(root, "a.bin"), 1024)
	write(filepath.Join(root, "b.bin"), 200*1024)
	write(filepath.Join(root, "sub", "c.bin"), 1024*1024)
	return total
}

func diffTrees(a, b string) error {
	ea, err := os.ReadDir(a)
	if err != nil {
		return err
	}
	eb, err := os.ReadDir(b)
	if err != nil {
		return err
	}
	if len(ea) != len(eb) {
		return fmt.Errorf("entry count differs in %s vs %s: %d vs %d", a, b, len(ea), len(eb))
	}
	for _, de := range ea {
		pa := filepath.Join(a, de.Name())
		pb := filepath.Join(b, de.Name())
		if de.IsDir() {
			if err := diffTrees(pa, pb); err != nil {
				return err
			}
			continue
		}
		ba, err := os.ReadFile(pa)
		if err != nil {
			return err
		}
		bb, err := os.ReadFile(pb)
		if err != nil {
			return err
		}
		if !bytes.Equal(ba, bb) {
			return fmt.Errorf("content differs: %s vs %s", pa, pb)
		}
	}
	return nil
}

func main() {
	tmp, err := os.MkdirTemp("", "fcmd-copytest-")
	must(err)
	defer os.RemoveAll(tmp)

	src := filepath.Join(tmp, "src")
	dstLocal := filepath.Join(tmp, "dst-local")
	dstRemote := filepath.Join(tmp, "dst-remote")
	must(os.MkdirAll(src, 0o755))
	must(os.MkdirAll(dstLocal, 0o755))
	must(os.MkdirAll(dstRemote, 0o755))
	totalBytes := seedTree(src)
	fmt.Printf("seeded %d bytes in %s\n", totalBytes, src)

	local := vfs.NewLocal()
	remote, err := vfs.NewRemote("127.0.0.1:7891", "loopback")
	must(err)
	defer remote.Close()

	progress := func(bytesDone, tb, filesDone, totalFiles int64, name string, cur, curTotal int64) {
		// uncomment for noisy progress
		// fmt.Printf("  %s  %d/%d  (%s: %d/%d)\n", name, bytesDone, tb, name, cur, curTotal)
	}

	type tcase struct {
		name             string
		srcFS, dstFS     vfs.FS
		srcPath, dstPath string
		verify           string // local path we should diff against src
	}
	cases := []tcase{
		{"local -> local", local, local, src, filepath.Join(dstLocal, "tree-local"), filepath.Join(dstLocal, "tree-local")},
		{"local -> remote", local, remote, src, filepath.Join(dstRemote, "tree-remote"), filepath.Join(dstRemote, "tree-remote")},
		{"remote -> local", remote, local, src, filepath.Join(dstLocal, "tree-round"), filepath.Join(dstLocal, "tree-round")},
		{"remote -> remote", remote, remote, src, filepath.Join(dstRemote, "tree-rr"), filepath.Join(dstRemote, "tree-rr")},
	}

	for _, tc := range cases {
		fmt.Printf("=== %s\n", tc.name)
		f, b, err := vfs.Plan(tc.srcFS, tc.srcPath)
		must(err)
		fmt.Printf("  plan: %d files, %d bytes\n", f, b)
		if err := vfs.Copy(tc.srcFS, tc.dstFS, tc.srcPath, tc.dstPath, f, b, progress); err != nil {
			log.Fatalf("copy failed: %v", err)
		}
		if err := diffTrees(src, tc.verify); err != nil {
			log.Fatalf("verify failed: %v", err)
		}
		fmt.Printf("  OK\n")
	}
	fmt.Println("ALL COPY DIRECTIONS PASSED")
}
