package main

import (
	"os"
	"path/filepath"
	"testing"
)

func profilePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// pprof profiles are gzipped protobuf streams, so a written profile file
// must exist and start with the gzip magic bytes.
func assertGzipProfile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read profile %s: %s", path, err)
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		t.Fatalf("profile %s is not a gzip stream (%d bytes)", path, len(data))
	}
}

func TestProfilerFinalizeWritesMemProfile(t *testing.T) {
	path := profilePath(t, "mem.prof")

	StartProfiler("", path).Finalize()

	assertGzipProfile(t, path)
}

func TestProfilerFinalizeWritesCpuProfile(t *testing.T) {
	path := profilePath(t, "cpu.prof")

	p := StartProfiler(path, "")
	p.Finalize()
	p.Finalize() // idempotent: must not panic or double-stop

	assertGzipProfile(t, path)
}

// The mid-run snapshot must survive Finalize: once a heap snapshot is on
// disk, the exit snapshot must not overwrite it.
func TestProfilerKeepsMidRunMemSnapshot(t *testing.T) {
	path := profilePath(t, "mem.prof")

	p := StartProfiler("", path)
	p.WriteMemProfile()
	if err := os.WriteFile(path, []byte("marker"), 0o644); err != nil {
		t.Fatalf("could not mark the profile file: %s", err)
	}

	p.Finalize()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read profile %s: %s", path, err)
	}
	if string(data) != "marker" {
		t.Error("Finalize overwrote the mid-run mem profile")
	}
}

func TestProfilerWithoutFilesWritesNothing(t *testing.T) {
	StartProfiler("", "").Finalize()
}

func TestProfilerNilReceiverIsSafe(t *testing.T) {
	var p *Profiler
	p.WriteMemProfile()
	p.Finalize()
}
