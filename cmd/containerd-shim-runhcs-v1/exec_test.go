package main

import (
	"context"
	"time"

	"github.com/containerd/containerd/runtime/v2/task"
)

var _ = (shimExec)(&testShimExec{})

type testShimExec struct {
	id  string
	pid int

	state shimExecState
}

func (tse *testShimExec) ID() string {
	return tse.id
}
func (tse *testShimExec) Parent() shimTask {
	return nil
}
func (tse *testShimExec) Pid() int {
	return tse.pid
}
func (tse *testShimExec) State() shimExecState {
	return tse.state
}
func (tse *testShimExec) Status() *task.StateResponse {
	return &task.StateResponse{
		ID:         tse.id,
		Pid:        uint32(tse.pid),
		ExitStatus: 255,
		ExitedAt:   time.Time{},
	}
}
func (tse *testShimExec) Start(ctx context.Context) error {
	tse.state = shimExecStateRunning
	return nil
}
func (tse *testShimExec) Kill(ctx context.Context, signal uint32) error {
	tse.state = shimExecStateExited
	return nil
}
func (tse *testShimExec) ResizePty(ctx context.Context, width, height uint32) error {
	return nil
}
func (tse *testShimExec) CloseIO(ctx context.Context, stdin bool) error {
	return nil
}
func (tse *testShimExec) Wait(ctx context.Context) *task.StateResponse {
	return tse.Status()
}