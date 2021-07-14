// +build functional uvmscsi

package functional

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Microsoft/hcsshim/internal/wclayer"

	"github.com/Microsoft/hcsshim/internal/lcow"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/Microsoft/hcsshim/osversion"
	testutilities "github.com/Microsoft/hcsshim/test/functional/utilities"
	"github.com/sirupsen/logrus"
)

// TestSCSIAddRemovev2LCOW validates adding and removing SCSI disks
// from a utility VM in both attach-only and with a container path.
func TestSCSIAddRemoveLCOW(t *testing.T) {
	testutilities.RequiresBuild(t, osversion.RS5)
	u := testutilities.CreateLCOWUVM(context.Background(), t, t.Name())
	defer u.Close()

	testSCSIAddRemoveMultiple(t, u, `/run/gcs/c/0/scsi`, "linux", []string{})

}

// TestSCSIAddRemoveWCOW validates adding and removing SCSI disks
// from a utility VM in both attach-only and with a container path.
func TestSCSIAddRemoveWCOW(t *testing.T) {
	testutilities.RequiresBuild(t, osversion.RS5)
	// TODO make the image configurable to the build we're testing on
	u, uvmScratchDir := testutilities.CreateWCOWUVM(context.Background(), t, t.Name(), "mcr.microsoft.com/windows/nanoserver:1903")
	defer os.RemoveAll(uvmScratchDir)
	defer u.Close()
	layers := testutilities.CreateWCOWBlankBaseLayer(context.Background(), t)
	defer os.RemoveAll(layers[0])

	testSCSIAddRemoveSingle(t, u, `c:\`, "windows", layers)
}

func testAddSCSI(u *uvm.UtilityVM, disks []string, pathPrefix string, usePath bool, reAdd bool) error {
	for i := range disks {
		uvmPath := ""
		if usePath {
			uvmPath = fmt.Sprintf(`%s%d`, pathPrefix, i)
		}
		var options []string
		scsiMount, err := u.AddSCSI(context.Background(), disks[i], uvmPath, false, options, uvm.VMAccessTypeIndividual)
		if err != nil {
			return err
		}
		if reAdd && scsiMount.UVMPath != uvmPath {
			return fmt.Errorf("expecting existing path to be %s but it is %s", uvmPath, scsiMount.UVMPath)
		}
	}
	return nil
}

func testRemoveAllSCSI(u *uvm.UtilityVM, disks []string) error {
	for i := range disks {
		if err := u.RemoveSCSI(context.Background(), disks[i]); err != nil {
			return err
		}
	}
	return nil
}

// TODO this test is only needed until WCOW supports adding the same scsi device to
// multiple containers
func testSCSIAddRemoveSingle(t *testing.T, u *uvm.UtilityVM, pathPrefix string, operatingSystem string, wcowImageLayerFolders []string) {
	numDisks := 63 // Windows: 63 as the UVM scratch is at 0:0
	if operatingSystem == "linux" {
		numDisks++ //
	}

	// Create a bunch of directories each containing sandbox.vhdx
	disks := make([]string, numDisks)
	for i := 0; i < numDisks; i++ {
		tempDir := ""
		if operatingSystem == "windows" {
			tempDir = testutilities.CreateWCOWBlankRWLayer(t, wcowImageLayerFolders)
		} else {
			tempDir = testutilities.CreateLCOWBlankRWLayer(context.Background(), t)
		}
		defer os.RemoveAll(tempDir)
		disks[i] = filepath.Join(tempDir, `sandbox.vhdx`)
	}

	// Add each of the disks to the utility VM. Attach-only, no container path
	useUvmPathPrefix := false
	logrus.Debugln("First - adding in attach-only")
	err := testAddSCSI(u, disks, pathPrefix, useUvmPathPrefix, false)
	if err != nil {
		t.Fatalf("failed to add SCSI device: %v", err)
	}

	// Remove them all
	logrus.Debugln("Removing them all")
	err = testRemoveAllSCSI(u, disks)
	if err != nil {
		t.Fatalf("failed to remove SCSI disk: %v", err)
	}

	// Now re-add but providing a container path
	useUvmPathPrefix = true
	logrus.Debugln("Next - re-adding with a container path")
	err = testAddSCSI(u, disks, pathPrefix, useUvmPathPrefix, false)
	if err != nil {
		t.Fatalf("failed to add SCSI device: %v", err)
	}

	logrus.Debugln("Next - Removing them")
	err = testRemoveAllSCSI(u, disks)
	if err != nil {
		t.Fatalf("failed to remove SCSI disk: %v", err)
	}

	// TODO: Could extend to validate can't add a 64th disk (windows). 65th (linux).
}

func testSCSIAddRemoveMultiple(t *testing.T, u *uvm.UtilityVM, pathPrefix string, operatingSystem string, wcowImageLayerFolders []string) {
	numDisks := 63 // Windows: 63 as the UVM scratch is at 0:0
	if operatingSystem == "linux" {
		numDisks++ //
	}

	// Create a bunch of directories each containing sandbox.vhdx
	disks := make([]string, numDisks)
	for i := 0; i < numDisks; i++ {
		tempDir := ""
		if operatingSystem == "windows" {
			tempDir = testutilities.CreateWCOWBlankRWLayer(t, wcowImageLayerFolders)
		} else {
			tempDir = testutilities.CreateLCOWBlankRWLayer(context.Background(), t)
		}
		defer os.RemoveAll(tempDir)
		disks[i] = filepath.Join(tempDir, `sandbox.vhdx`)
	}

	// Add each of the disks to the utility VM. Attach-only, no container path
	useUvmPathPrefix := false
	logrus.Debugln("First - adding in attach-only")
	err := testAddSCSI(u, disks, pathPrefix, useUvmPathPrefix, false)
	if err != nil {
		t.Fatalf("failed to add SCSI device: %v", err)
	}

	// Try to re-add.
	// We only support re-adding the same scsi device for lcow right now
	logrus.Debugln("Next - trying to re-add")
	err = testAddSCSI(u, disks, pathPrefix, useUvmPathPrefix, true)
	if err != nil {
		t.Fatalf("failed to re-add SCSI device: %v", err)
	}

	// Remove them all
	logrus.Debugln("Removing them all")
	// first removal decrements ref count
	err = testRemoveAllSCSI(u, disks)
	if err != nil {
		t.Fatalf("failed to remove SCSI disk: %v", err)
	}
	// second removal actually removes the device
	err = testRemoveAllSCSI(u, disks)
	if err != nil {
		t.Fatalf("failed to remove SCSI disk: %v", err)
	}

	// Now re-add but providing a container path
	logrus.Debugln("Next - re-adding with a container path")
	useUvmPathPrefix = true
	err = testAddSCSI(u, disks, pathPrefix, useUvmPathPrefix, false)
	if err != nil {
		t.Fatalf("failed to add SCSI device: %v", err)
	}

	// Try to re-add
	logrus.Debugln("Next - trying to re-add")
	err = testAddSCSI(u, disks, pathPrefix, useUvmPathPrefix, true)
	if err != nil {
		t.Fatalf("failed to add SCSI device: %v", err)
	}

	logrus.Debugln("Next - Removing them")
	// first removal decrements ref count
	err = testRemoveAllSCSI(u, disks)
	if err != nil {
		t.Fatalf("failed to remove SCSI disk: %v", err)
	}
	// second removal actually removes the device
	err = testRemoveAllSCSI(u, disks)
	if err != nil {
		t.Fatalf("failed to remove SCSI disk: %v", err)
	}

	// check the devices are no longer present on the uvm
	targetNamespace := "k8s.io"
	for i := 0; i < numDisks; i++ {
		uvmPath := fmt.Sprintf(`%s%d`, pathPrefix, i)
		out, err := exec.Command(`shimdiag.exe`, `exec`, targetNamespace, `ls`, uvmPath).Output()
		if err == nil {
			t.Fatalf("expected to no longer have scsi device files, instead returned %s", string(out))
		}
	}

	// TODO: Could extend to validate can't add a 64th disk (windows). 65th (linux).
}

func TestParallelScsiOps(t *testing.T) {
	testutilities.RequiresBuild(t, osversion.RS5)
	u := testutilities.CreateLCOWUVM(context.Background(), t, t.Name())
	defer u.Close()

	// Create a sandbox to use
	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("failed to create tmpdir for test: %v", err)
	}
	if err := lcow.CreateScratch(context.Background(), u, filepath.Join(tempDir, "sandbox.vhdx"), lcow.DefaultScratchSizeGB, ""); err != nil {
		t.Fatalf("failed to create EXT4 scratch for LCOW test cases: %s", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("failed to remove sandbox tmpdir: %v", err)
		}
	}()
	copySandbox := func(dir string, workerId, iteration int) (string, error) {
		orig, err := os.Open(filepath.Join(dir, "sandbox.vhdx"))
		if err != nil {
			return "", err
		}
		defer orig.Close()
		path := filepath.Join(dir, fmt.Sprintf("%d-%d-sandbox.vhdx", workerId, iteration))
		new, err := os.Create(path)
		if err != nil {
			return "", err
		}
		defer new.Close()

		_, err = io.Copy(new, orig)
		if err != nil {
			return "", err
		}
		return path, nil
	}

	// Note: maxWorkers cannot be > 64 for this code to work
	maxWorkers := 16
	opsChan := make(chan int, maxWorkers)
	opsWg := sync.WaitGroup{}
	opsWg.Add(maxWorkers)
	for i := 0; i < maxWorkers; i++ {
		go func(scsiIndex int) {
			for {
				iteration, ok := <-opsChan
				if !ok {
					break
				}
				// Copy the goal sandbox.vhdx to a new path so we don't get the cached location
				path, err := copySandbox(tempDir, scsiIndex, iteration)
				if err != nil {
					t.Errorf("failed to copy sandbox for worker: %d, iteration: %d with err: %v", scsiIndex, iteration, err)
					continue
				}
				err = wclayer.GrantVmAccess(context.Background(), u.ID(), path)
				if err != nil {
					os.Remove(path)
					t.Errorf("failed to grantvmaccess for worker: %d, iteration: %d with err: %v", scsiIndex, iteration, err)
					continue
				}

				var options []string
				_, err = u.AddSCSI(context.Background(), path, "", false, options, uvm.VMAccessTypeIndividual)
				if err != nil {
					os.Remove(path)
					t.Errorf("failed to AddSCSI for worker: %d, iteration: %d with err: %v", scsiIndex, iteration, err)
					continue
				}
				err = u.RemoveSCSI(context.Background(), path)
				if err != nil {
					t.Errorf("failed to RemoveSCSI for worker: %d, iteration: %d with err: %v", scsiIndex, iteration, err)
					// This worker cant continue because the index is dead. We have to stop
					break
				}

				_, err = u.AddSCSI(context.Background(), path, fmt.Sprintf("/run/gcs/c/0/scsi/%d", iteration), false, options, uvm.VMAccessTypeIndividual)
				if err != nil {
					os.Remove(path)
					t.Errorf("failed to AddSCSI for worker: %d, iteration: %d with err: %v", scsiIndex, iteration, err)
					continue
				}
				err = u.RemoveSCSI(context.Background(), path)
				if err != nil {
					t.Errorf("failed to RemoveSCSI for worker: %d, iteration: %d with err: %v", scsiIndex, iteration, err)
					// This worker cant continue because the index is dead. We have to stop
					break
				}
				os.Remove(path)
			}
			opsWg.Done()
		}(i)
	}

	scsiOps := 1000
	for i := 0; i < scsiOps; i++ {
		opsChan <- i
	}
	close(opsChan)

	opsWg.Wait()
}
