package main

import (
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"sync"
)

// profiler owns the profile files for the run; nil when StartProfiler was
// never called. It is package-level so fatal() can flush pending profiles
// from anywhere.
var profiler *Profiler

// Profiler makes sure the profiles reach their files on every exit path:
// normal end, SIGINT, or a mid-run fatal (log.Fatalf skips defers via
// os.Exit, which would lose the buffered cpu samples). Finalize is
// idempotent and nil-safe.
type Profiler struct {
	cpuFile *os.File
	memPath string

	mu        sync.Mutex
	finalized bool
	wroteMem  bool
}

// StartProfiler starts cpu profiling when cpuPath is set; heap snapshots go
// to memPath on WriteMemProfile/Finalize.
func StartProfiler(cpuPath, memPath string) *Profiler {
	p := &Profiler{memPath: memPath}
	if cpuPath == "" {
		return p
	}
	f, err := os.Create(cpuPath)
	if err != nil {
		log.Fatalf("could not create cpu profile file %s: %s", cpuPath, err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		log.Fatalf("could not start cpu profile: %s", err)
	}
	p.cpuFile = f
	return p
}

// WriteMemProfile writes a heap snapshot to the mem profile file. It is a
// no-op without -memprofile and after Finalize, so the final snapshot can
// never overwrite an earlier mid-run one.
func (p *Profiler) WriteMemProfile() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writeHeapLocked()
}

// Finalize stops cpu profiling, closes its file, and writes the final heap
// snapshot — unless a mid-run snapshot is already on disk.
func (p *Profiler) Finalize() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return
	}
	p.finalized = true
	if p.cpuFile != nil {
		pprof.StopCPUProfile() // flushes the buffered samples
		p.cpuFile.Close()
	}
	p.writeHeapLocked()
}

func (p *Profiler) writeHeapLocked() {
	if p.memPath == "" || p.wroteMem {
		return
	}
	f, err := os.Create(p.memPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create mem profile file %s: %s\n", p.memPath, err)
		return
	}
	defer f.Close()
	if err := pprof.WriteHeapProfile(f); err != nil {
		fmt.Fprintf(os.Stderr, "could not write mem profile: %s\n", err)
		return
	}
	p.wroteMem = true
}

// fatal reports the error like log.Fatalf but flushes pending profile files
// first: log.Fatalf exits through os.Exit, which skips defers.
func fatal(format string, args ...any) {
	profiler.Finalize()
	log.Fatalf(format, args...)
}
