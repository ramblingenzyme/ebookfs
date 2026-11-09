package ctl

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func newTestCtl(t *testing.T, lib mock.Library) *CtlFile {
	t.Helper()
	return NewCtlFile(util.NewTestFS(t), lib, NewCommandLog(10))
}
