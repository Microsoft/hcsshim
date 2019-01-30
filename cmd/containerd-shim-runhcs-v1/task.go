package main

import (
	"context"
	"time"
)

// shimTaskPidPair groups a process pid to its execID if it was user generated.
type shimTaskPidPair struct {
	// Pid is the pid of the container process.
	Pid int
	// ExecID is the id of the exec if this container process was user
	// generated.
	ExecID string
}

type shimTask interface {
	// ID returns the original id used at `Create`.
	ID() string
	// GetExec returns an exec in this task that matches `eid`. If `eid == ""`
	// returns the init exec from the initial call to `Create`.
	//
	// If `eid` is not found this task MUST return `errdefs.ErrNotFound`.
	GetExec(eid string) (shimExec, error)
	// KillExec sends `signal` to the exec that matches `eid`. If `all==true`
	// `eid` MUST be empty and this task will send `signal` to all exec's in the
	// task and lastly send `signal` to the init exec.
	//
	// If `all == true && eid != ""` this task MUST return
	// `errdefs.ErrFailedPrecondition`.
	//
	// A call to `KillExec` is only valid when the exec is in the
	// `shimExecStateRunning` state. If the exec is not in this state this task
	// MUST return `errdefs.ErrFailedPrecondition`.
	KillExec(ctx context.Context, eid string, signal uint32, all bool) error
	// DeleteExec deletes a `shimExec` in this `shimTask` that matches `eid`. If
	// `eid == ""` deletes the init `shimExec` AND this `shimTask`.
	//
	// If `eid` is not found `shimExec` MUST return `errdefs.ErrNotFound`.
	//
	// A call to `DeleteExec` is only valid in `shimExecStateCreated` and
	// `shimExecStateExited` states and MUST return
	// `errdefs.ErrFailedPrecondition` if not in these states.
	DeleteExec(ctx context.Context, eid string) (int, uint32, time.Time, error)
	// Pids returns all process pid's in this `shimTask` including ones not
	// created by the caller via a `CreateExec`.
	Pids(ctx context.Context) ([]shimTaskPidPair, error)
}