package bench

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

/*
#include <time.h>

static unsigned long long get_nsecs(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (unsigned long long)ts.tv_sec * 1000000000UL + ts.tv_nsec;
}
*/
import "C"

func unixcall() unix.Timespec {
	ts := unix.Timespec{}
	unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts)
	return ts
}

func monotime() uint64 {
	return uint64(C.get_nsecs())
}

func getNSecs() int64 {
	var ts syscall.Timespec
	syscall.Syscall(syscall.SYS_CLOCK_GETTIME, 4, uintptr(unsafe.Pointer(&ts)), 0)
	return ts.Sec*1e9 + ts.Nsec
}
