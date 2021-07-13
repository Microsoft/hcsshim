package exec

import (
	"context"
	"os"
	"testing"
	"unsafe"

	"github.com/Microsoft/hcsshim/internal/jobobject"
	"github.com/Microsoft/hcsshim/internal/winapi"
	"golang.org/x/sys/windows"
)

// Tests that the standard os/exec.Cmd functionality works the same.
func TestExecProcess(t *testing.T) {
	cmd := Command("ping", "127.0.0.1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to run process: %s", err)
	}
}

// Tests that the procattrlist functionality works as expected.
func TestExecProcessAttrList(t *testing.T) {
	procAttrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		t.Fatal(err)
	}

	pHandle, err := windows.OpenProcess(winapi.PROCESS_ALL_ACCESS, false, uint32(os.Getpid()))
	if err != nil {
		t.Fatalf("failed to open process: %s", err)
	}
	// The call to UpdateProcThreadAttribute here does nothing essentially.
	// PROC_THREAD_ATTRIBUTE_PARENT_PROCESS is used to be able to create the process
	// to be launched as a child of a DIFFERENT process than the one it is being
	// launched from. Since the pid we're using is just the PID of the process launching
	// it anyways, this basically does nothing. This just tests that all of the necessary winapi
	// calls work end to end.
	err = procAttrList.Update(
		windows.PROC_THREAD_ATTRIBUTE_PARENT_PROCESS,
		0,
		unsafe.Pointer(&pHandle),
		unsafe.Sizeof(pHandle),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("UpdateProcThreadAttribute failed: %s", err)
	}

	cmd := Command("ping", "127.0.0.1")
	cmd.SysProcAttr = &SysProcAttr{
		ProcThreadAttrList: procAttrList,
	}

	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to run process: %s", err)
	}
}

func TestJobObject(t *testing.T) {
	procAttrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		t.Fatal(err)
	}

	options := &jobobject.Options{
		Name: "test",
	}
	job, err := jobobject.Create(context.Background(), options)
	if err != nil {
		t.Fatalf("failed to create job object: %s", err)
	}
	defer job.Close()

	if err := job.SetTerminateOnLastHandleClose(); err != nil {
		t.Fatal(err)
	}

	if err := job.AssignAtStart(procAttrList); err != nil {
		t.Fatal(err)
	}

	cmd := Command("ping", "-t", "127.0.0.1")
	cmd.SysProcAttr = &SysProcAttr{
		ProcThreadAttrList: procAttrList,
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %s", err)
	}

	pids, err := job.Pids()
	if err != nil {
		t.Fatal(err)
	}

	if pids[0] != uint32(cmd.Process.Pid) {
		t.Fatalf("expected only process in the job to be %d", cmd.Process.Pid)
	}
}
