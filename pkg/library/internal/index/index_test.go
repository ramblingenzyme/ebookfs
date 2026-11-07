package index

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/fsutil"
)

func TestMmapOffOnNetworkFilesystems(t *testing.T) {
	cases := map[fsutil.FSType]string{
		fsutil.FSTypeLocal: "mmap_size(134217728)",
		fsutil.FSTypeFUSE:  "mmap_size(134217728)",
		fsutil.FSTypeNFS:   "mmap_size(0)",
		fsutil.FSTypeCIFS:  "mmap_size(0)",
		fsutil.FSTypeSMB2:  "mmap_size(0)",
	}
	for fsType, want := range cases {
		if !slices.Contains(writerPragmas(fsType), want) {
			t.Errorf("%v writer pragmas lack %s", fsType, want)
		}
		if !slices.Contains(readerPragmas(fsType), want) {
			t.Errorf("%v reader pragmas lack %s", fsType, want)
		}
	}
}

// crashHelperEnv selects the work TestCrashHelper does in a child process.
const crashHelperEnv = "INDEX_CRASH_HELPER"

// TestCrashHelper is not a test of its own. It dies by SIGKILL mid-work, so the
// files it leaves are the ones a real crash leaves.
func TestCrashHelper(t *testing.T) {
	path := os.Getenv("INDEX_CRASH_PATH")
	switch os.Getenv(crashHelperEnv) {
	case "wal":
		db := openPragmas(t, path, "journal_mode(WAL)", "synchronous(NORMAL)")
		mustExec(t, db, "CREATE TABLE t(x BLOB)")
		mustExec(t, db, "INSERT INTO t VALUES (zeroblob(1000))")
	case "mid-write":
		db := openPragmas(t, path, writerPragmas(fsutil.FSTypeCIFS)...)
		// A small cache spills uncommitted pages to the database file, which is
		// the state only the journal can undo.
		mustExec(t, db, "PRAGMA cache_size=10")
		mustExec(t, db, "BEGIN")
		for range 100 {
			mustExec(t, db, "INSERT INTO t VALUES (zeroblob(10000))")
		}
	default:
		t.Skip("runs only as a child of the crash tests")
	}
	syscall.Kill(os.Getpid(), syscall.SIGKILL)
}

func crashChild(t *testing.T, work, path string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
	cmd.Env = append(os.Environ(), crashHelperEnv+"="+work, "INDEX_CRASH_PATH="+path)
	var exitErr *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exitErr) || exitErr.ExitCode() != -1 {
		t.Fatalf("crash helper err = %v, want death by signal", err)
	}
}

func openPragmas(t *testing.T, path string, pragmas ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn(path, pragmas...))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func countRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM t").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// An index a crashed WAL build left behind, -wal and all, converts to DELETE on
// the next open with its committed rows. PRAGMA journal_mode doesn't fail when
// it can't switch, so only the read-back shows the conversion happened.
func TestSMBPragmasConvertCrashedWALIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	crashChild(t, "wal", path)
	if fi, err := os.Stat(path + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("crash left no -wal to convert: %v, %v", fi, err)
	}

	for range 2 {
		db := openPragmas(t, path, writerPragmas(fsutil.FSTypeCIFS)...)
		var mode string
		if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if mode != "delete" {
			t.Errorf("journal_mode = %q, want delete", mode)
		}
		if n := countRows(t, db); n != 1 {
			t.Errorf("rows = %d, want 1", n)
		}
		for _, suffix := range []string{"-shm", "-wal"} {
			if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s present while open: %v", suffix, err)
			}
		}
		db.Close()
	}
}

// A crash mid-transaction leaves a hot journal, and the next open rolls it back.
func TestSMBPragmasRecoverCrashMidWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	db := openPragmas(t, path, writerPragmas(fsutil.FSTypeCIFS)...)
	mustExec(t, db, "CREATE TABLE t(x BLOB)")
	mustExec(t, db, "INSERT INTO t VALUES (zeroblob(1000))")
	db.Close()

	crashChild(t, "mid-write", path)
	if fi, err := os.Stat(path + "-journal"); err != nil || fi.Size() == 0 {
		t.Fatalf("crash left no hot journal: %v, %v", fi, err)
	}

	db = openPragmas(t, path, writerPragmas(fsutil.FSTypeCIFS)...)
	defer db.Close()
	if n := countRows(t, db); n != 1 {
		t.Errorf("rows = %d, want the 1 committed before the crash", n)
	}
	var check string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		t.Errorf("integrity_check = %q, %v", check, err)
	}
}
