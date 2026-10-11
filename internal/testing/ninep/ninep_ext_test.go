package ninep_test

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/ninep"
)

func serveTree(t *testing.T, root *fs.StaticDir) *ninep.Conn {
	t.Helper()
	fsys, _ := fs.NewFS("root", "root", 0755, fs.IgnorePermissions())
	fsys.Root = root

	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		go9p.ServeReadWriter(server, server, fsys.Server())
		close(done)
	}()
	t.Cleanup(func() {
		client.Close()
		<-done
	})

	conn, err := ninep.Dial(client, "glenda")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return conn
}

func TestReaddirRootListsStaticFile(t *testing.T) {
	root := fs.NewStaticDir(&proto.Stat{
		Name: "/",
		Mode: proto.DMDIR | 0755,
	})
	file := fs.NewStaticFile(&proto.Stat{
		Name:   "test.txt",
		Length: 13,
		Mode:   0644,
	}, []byte("hello, world!"))
	root.AddChild(file)

	conn := serveTree(t, root)

	stats, err := conn.Readdir("/")
	if err != nil {
		t.Fatalf("Readdir: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("Readdir returned %d entries, want 1", len(stats))
	}
	if stats[0].Name != "test.txt" {
		t.Errorf("entry name = %q, want %q", stats[0].Name, "test.txt")
	}

	fid, err := conn.Open("/test.txt", proto.Oread)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer fid.Clunk()

	data, err := fid.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hello, world!" {
		t.Errorf("ReadAll = %q, want %q", string(data), "hello, world!")
	}
}

func TestReaddirLargeDirectory(t *testing.T) {
	root := fs.NewStaticDir(&proto.Stat{
		Name: "/",
		Mode: proto.DMDIR | 0755,
	})

	const count = 2000
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("file%d.txt", i)
		file := fs.NewStaticFile(&proto.Stat{
			Name:   name,
			Length: 4,
			Mode:   0644,
		}, []byte("test"))
		root.AddChild(file)
	}

	conn := serveTree(t, root)

	stats, err := conn.Readdir("/")
	if err != nil {
		t.Fatalf("Readdir: %v", err)
	}
	if len(stats) != count {
		t.Errorf("Readdir returned %d entries, want %d", len(stats), count)
	}
}

func TestWalkMissing(t *testing.T) {
	root := fs.NewStaticDir(&proto.Stat{
		Name: "/",
		Mode: proto.DMDIR | 0755,
	})
	dir := fs.NewStaticDir(&proto.Stat{
		Name: "dir",
		Mode: proto.DMDIR | 0755,
	})
	root.AddChild(dir)

	conn := serveTree(t, root)

	_, err := conn.Walk("/missing")
	if err == nil {
		t.Error("Walk(/missing) succeeded, want error")
	}

	_, err = conn.Walk("/dir/missing")
	if err == nil {
		t.Error("Walk(/dir/missing) succeeded, want error")
	}
}

// The tree has no create hook, so the server refuses every create. Create once
// deadlocked on that refusal, so the call runs against a deadline.
func TestCreateRefused(t *testing.T) {
	root := fs.NewStaticDir(&proto.Stat{
		Name: "/",
		Mode: proto.DMDIR | 0755,
	})
	conn := serveTree(t, root)

	done := make(chan error, 1)
	go func() {
		_, err := conn.Create("/new.txt", 0644, proto.Owrite)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Create succeeded, want the server's refusal")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Create did not return after the server refused it")
	}

	if _, err := conn.Readdir("/"); err != nil {
		t.Errorf("Readdir after a refused create: %v", err)
	}
}

type openFailsFile struct {
	fs.BaseFile
}

func (f *openFailsFile) Open(fid uint64, omode proto.Mode) error {
	return errors.New("open failed")
}

// Open once deadlocked clunking the walked fid after a refusal, so the call
// runs against a deadline.
func TestOpenRefused(t *testing.T) {
	root := fs.NewStaticDir(&proto.Stat{
		Name: "/",
		Mode: proto.DMDIR | 0755,
	})
	file := &openFailsFile{}
	file.BaseFile = *fs.NewBaseFile(&proto.Stat{
		Name: "fails.txt",
		Mode: 0644,
	})
	root.AddChild(file)
	conn := serveTree(t, root)

	done := make(chan error, 1)
	go func() {
		_, err := conn.Open("/fails.txt", proto.Oread)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || err.Error() != "open failed" {
			t.Fatalf("Open error = %v, want %q", err, "open failed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Open did not return after the server refused it")
	}

	if _, err := conn.Readdir("/"); err != nil {
		t.Errorf("Readdir after a refused open: %v", err)
	}
}

type closeFailsFile struct {
	fs.BaseFile
}

func (f *closeFailsFile) Close(fid uint64) error {
	return errors.New("close failed")
}

func TestClunkReturnsCloseError(t *testing.T) {
	root := fs.NewStaticDir(&proto.Stat{
		Name: "/",
		Mode: proto.DMDIR | 0755,
	})
	file := &closeFailsFile{}
	file.BaseFile = *fs.NewBaseFile(&proto.Stat{
		Name:   "fails.txt",
		Length: 0,
		Mode:   0644,
	})
	root.AddChild(file)

	conn := serveTree(t, root)

	fid, err := conn.Open("/fails.txt", proto.Oread)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	err = fid.Clunk()
	if err == nil {
		t.Error("Clunk succeeded, want error")
	}
	if err != nil && err.Error() != "close failed" {
		t.Errorf("Clunk error = %v, want %q", err, "close failed")
	}
}

type wstatFile struct {
	fs.BaseFile
	length uint64
}

func (f *wstatFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	if offset >= f.length {
		return nil, nil
	}
	if offset+count > f.length {
		count = f.length - offset
	}
	return make([]byte, count), nil
}

func (f *wstatFile) WriteStat(s *proto.Stat) error {
	if s.Length != ^uint64(0) {
		f.length = s.Length
	}
	return nil
}

func (f *wstatFile) Stat() proto.Stat {
	s := f.BaseFile.Stat()
	s.Length = f.length
	return s
}

func TestWstatLengthZero(t *testing.T) {
	root := fs.NewStaticDir(&proto.Stat{
		Name: "/",
		Mode: proto.DMDIR | 0755,
	})
	file := &wstatFile{length: 100}
	file.BaseFile = *fs.NewBaseFile(&proto.Stat{
		Name:   "resizable.txt",
		Length: 100,
		Mode:   0644,
	})
	root.AddChild(file)

	conn := serveTree(t, root)

	fid, err := conn.Open("/resizable.txt", proto.Ordwr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer fid.Clunk()

	stat, err := fid.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.Length != 100 {
		t.Fatalf("initial Length = %d, want 100", stat.Length)
	}
	if stat.Name != "resizable.txt" {
		t.Fatalf("initial Name = %q, want %q", stat.Name, "resizable.txt")
	}
	if stat.Mode != 0644 {
		t.Fatalf("initial Mode = %o, want %o", stat.Mode, 0644)
	}

	wstat := ninep.DontTouch()
	wstat.Length = 0
	err = fid.WStat(wstat)
	if err != nil {
		t.Fatalf("WStat: %v", err)
	}

	stat, err = fid.Stat()
	if err != nil {
		t.Fatalf("Stat after WStat: %v", err)
	}
	if stat.Length != 0 {
		t.Errorf("Length after WStat = %d, want 0", stat.Length)
	}
	if stat.Name != "resizable.txt" {
		t.Errorf("Name after WStat = %q, want %q", stat.Name, "resizable.txt")
	}
	if stat.Mode != 0644 {
		t.Errorf("Mode after WStat = %o, want %o", stat.Mode, 0644)
	}
}

func TestDialRejectsHandshake(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		msize   uint32
	}{
		{"unknown version", "unknown", 8192},
		{"msize too small", "9P2000", 23},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, client := net.Pipe()
			t.Cleanup(func() { client.Close() })
			go func() {
				defer server.Close()
				if _, err := proto.ParseCall(server); err != nil {
					return
				}
				rver := &proto.TRVersion{
					Header:  proto.Header{Type: proto.Rversion},
					Msize:   tc.msize,
					Version: tc.version,
				}
				server.Write(rver.Compose())
				if _, err := proto.ParseCall(server); err != nil {
					return
				}
				rattach := &proto.RAttach{Header: proto.Header{Type: proto.Rattach, Tag: 1}}
				server.Write(rattach.Compose())
			}()

			if _, err := ninep.Dial(client, "glenda"); err == nil {
				t.Fatal("Dial succeeded, want a handshake error")
			}
		})
	}
}
