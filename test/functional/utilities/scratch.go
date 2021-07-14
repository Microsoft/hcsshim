package testutilities

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Microsoft/hcsshim/internal/hcs"
	"github.com/Microsoft/hcsshim/internal/lcow"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/Microsoft/hcsshim/internal/wclayer"
)

const lcowGlobalSVMID = "test.lcowglobalsvm"

var (
	lcowGlobalSVM        *uvm.UtilityVM
	lcowCacheScratchFile string
)

func init() {
	if hcsSystem, err := hcs.OpenComputeSystem(context.Background(), lcowGlobalSVMID); err == nil {
		_ = hcsSystem.Terminate(context.Background())
	}
}

// CreateWCOWBlankBaseLayer creates an as-blank-as-possible base WCOW layer, which
// can be used as the base of a WCOW RW layer when it's not going to be the container's
// scratch mount.
func CreateWCOWBlankBaseLayer(ctx context.Context, t *testing.T) []string {
	tempDir := CreateTempDir(t)
	if err := wclayer.ConvertToBaseLayer(context.Background(), tempDir); err != nil {
		t.Fatalf("Failed ConvertToBaseLayer: %s", err)
	}
	return []string{tempDir}
}

// CreateWCOWBlankRWLayer uses HCS to create a temp test directory containing a
// read-write layer containing a disk that can be used as a containers scratch
// space. The VHD is created with VM group access
// TODO: This is wrong. Need to search the folders.
func CreateWCOWBlankRWLayer(t *testing.T, imageLayers []string) string {

	//	uvmFolder, err := LocateUVMFolder(imageLayers)
	//	if err != nil {
	//		t.Fatalf("failed to locate UVM folder from %+v: %s", imageLayers, err)
	//	}

	tempDir := CreateTempDir(t)
	if err := wclayer.CreateScratchLayer(context.Background(), tempDir, imageLayers); err != nil {
		t.Fatalf("Failed CreateScratchLayer: %s", err)
	}
	return tempDir
}

// CreateLCOWBlankRWLayer uses an LCOW utility VM to create a blank VHDX and
// format it ext4. This can then be used as a scratch space for a container, or
// for a "service VM".
func CreateLCOWBlankRWLayer(ctx context.Context, t *testing.T) string {
	if lcowGlobalSVM == nil {
		lcowGlobalSVM = CreateLCOWUVM(ctx, t, lcowGlobalSVMID)
		lcowCacheScratchFile = filepath.Join(CreateTempDir(t), "sandbox.vhdx")
	}
	tempDir := CreateTempDir(t)

	if err := lcow.CreateScratch(ctx, lcowGlobalSVM, filepath.Join(tempDir, "sandbox.vhdx"), lcow.DefaultScratchSizeGB, lcowCacheScratchFile); err != nil {
		t.Fatalf("failed to create EXT4 scratch for LCOW test cases: %s", err)
	}
	return tempDir
}
