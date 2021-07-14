package jobcontainers

import (
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestNamePipeDeny(t *testing.T) {
	s := &specs.Spec{
		Mounts: []specs.Mount{
			{
				Destination: "/path/in/container",
				Source:      `\\.\pipe\dummy\path`,
			},
		},
	}
	if err := setupMounts(s, "/test"); err == nil {
		t.Fatal("expected named pipe mount validation to fail for job container")
	}
}
