//go:build e2e

package fs

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/client"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/config"
)

func startTestServer(t *testing.T) (addr string) {
	t.Helper()

	cfg := testutil.TestConfig(t)
	lib, err := library.Open(cfg, false)
	if err != nil {
		t.Fatalf("Open library: %v", err)
	}
	t.Cleanup(func() { lib.Close() })

	exp, err := lib.Exporter(config.ReaderConfig{})
	if err != nil {
		t.Fatalf("Exporter: %v", err)
	}

	srv, err := SetupServer(lib, exp, 0, 0)
	if err != nil {
		t.Fatalf("SetupServer: %v", err)
	}
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go go9p.ServeReadWriter(bufio.NewReader(conn), conn, srv.ebookfs.Server())
		}
	}()

	return listener.Addr().String()
}

func connectClient(t *testing.T, addr string) *client.Client {
	t.Helper()
	c, err := client.Dial("tcp", addr, "glenda", "")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return c
}

func TestE2E_CorePath(t *testing.T) {
	addr := startTestServer(t)
	c := connectClient(t, addr)

	epubData := testutil.BuildTestEpub(t, "Test Book", "Alice Author")

	t.Run("ingest", func(t *testing.T) {
		f, err := c.Create("/inbox/Test Book.epub", 0644)
		if err != nil {
			t.Fatalf("Create inbox file: %v", err)
		}
		if _, err := f.Write(epubData); err != nil {
			t.Fatalf("Write epub: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("Close inbox file: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("verify listing", func(t *testing.T) {
		entries, err := c.Readdir("/books")
		if err != nil {
			t.Fatalf("Readdir /books: %v", err)
		}
		if len(entries) == 0 {
			t.Fatal("expected at least one book in /books")
		}
		found := false
		for _, e := range entries {
			if strings.Contains(e.Name, "Test Book") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("book 'Test Book' not found in /books listing")
		}
	})

	var bookDir string
	t.Run("find book directory", func(t *testing.T) {
		entries, err := c.Readdir("/books")
		if err != nil {
			t.Fatalf("Readdir /books: %v", err)
		}
		for _, e := range entries {
			if strings.Contains(e.Name, "Test Book") {
				bookDir = e.Name
				break
			}
		}
		if bookDir == "" {
			t.Fatal("book directory not found")
		}
	})

	t.Run("read metadata", func(t *testing.T) {
		f, err := c.Open("/books/"+bookDir+"/title", proto.Oread)
		if err != nil {
			t.Fatalf("Open title: %v", err)
		}
		defer f.Close()

		buf := make([]byte, 1024)
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("Read title: %v", err)
		}
		title := strings.TrimSpace(string(buf[:n]))
		if title != "Test Book" {
			t.Errorf("title = %q, want %q", title, "Test Book")
		}
	})

	t.Run("edit status", func(t *testing.T) {
		f, err := c.Open("/books/"+bookDir+"/status", proto.Owrite)
		if err != nil {
			t.Fatalf("Open status for write: %v", err)
		}
		if _, err := f.Write([]byte("reading")); err != nil {
			t.Fatalf("Write status: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("Close status: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("verify edit persisted", func(t *testing.T) {
		f, err := c.Open("/books/"+bookDir+"/status", proto.Oread)
		if err != nil {
			t.Fatalf("Open status for read: %v", err)
		}
		defer f.Close()

		buf := make([]byte, 1024)
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("Read status: %v", err)
		}
		status := strings.TrimSpace(string(buf[:n]))
		if status != "reading" {
			t.Errorf("status = %q, want %q", status, "reading")
		}
	})

	t.Run("read epub", func(t *testing.T) {
		f, err := c.Open("/books/"+bookDir+"/Test Book - Alice Author.epub", proto.Oread)
		if err != nil {
			t.Fatalf("Open epub: %v", err)
		}
		defer f.Close()

		buf := make([]byte, 1024*1024)
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("Read epub: %v", err)
		}
		if n < 4 {
			t.Fatalf("epub too small: %d bytes", n)
		}
		if string(buf[:2]) != "PK" {
			t.Errorf("epub does not start with PK (zip signature)")
		}
	})

	t.Run("verify by-author view", func(t *testing.T) {
		entries, err := c.Readdir("/by-author")
		if err != nil {
			t.Fatalf("Readdir /by-author: %v", err)
		}
		found := false
		for _, e := range entries {
			if strings.Contains(e.Name, "Alice") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("author 'Alice' not found in /by-author listing")
		}
	})
}
