package main

import (
	"context"
	"time"

	"github.com/containerd/containerd/errdefs"
)

var _ = (shimTask)(&testShimTask{})

type testShimTask struct {
	id string

	exec  *testShimExec
	execs map[string]*testShimExec
}

func (tst *testShimTask) ID() string {
	return tst.id
}

func (tst *testShimTask) GetExec(eid string) (shimExec, error) {
	if eid == "" {
		return tst.exec, nil
	}
	e, ok := tst.execs[eid]
	if ok {
		return e, nil
	}
	return nil, errdefs.ErrNotFound
}

func (tst *testShimTask) KillExec(ctx context.Context, eid string, signal uint32, all bool) error {
	e, err := tst.GetExec(eid)
	if err != nil {
		return err
	}
	return e.Kill(ctx, signal)
}

func (tst *testShimTask) DeleteExec(ctx context.Context, eid string) (int, uint32, time.Time, error) {
	e, err := tst.GetExec(eid)
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	status := e.Status()
	if eid != "" {
		delete(tst.execs, eid)
	}
	return int(status.Pid), status.ExitStatus, status.ExitedAt, nil
}

func (tst *testShimTask) Pids(ctx context.Context) ([]shimTaskPidPair, error) {
	pairs := []shimTaskPidPair{
		shimTaskPidPair{
			Pid:    tst.exec.Pid(),
			ExecID: tst.exec.ID(),
		},
	}
	for _, p := range tst.execs {
		pairs = append(pairs, shimTaskPidPair{
			Pid:    p.pid,
			ExecID: p.id,
		})
	}
	return pairs, nil
}