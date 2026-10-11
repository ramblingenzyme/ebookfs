package ctl

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func newTestCtl(t *testing.T, search mock.SearchDeleter, edit mock.Editor) *CtlFile {
	t.Helper()
	fsys := util.NewTestFS(t)
	lib := mock.Library{Editor: edit, SearchDeleter: search}
	return NewCtlFile(fsys, lib, NewCommandLog(10))
}
