package bench

import (
	"testing"
	"time"
)

func BenchmarkTimeNow(b *testing.B) {
	for range b.N {
		start := time.Now()
		_ = time.Since(start)
	}
}
func BenchmarkSyscall(b *testing.B) {
	for range b.N {
		start := getNSecs()
		_ = getNSecs() - start
	}
}

func BenchmarkSyscall2(b *testing.B) {
	for range b.N {
		start := unixcall()
		end := unixcall()
		_ = end.Sec - start.Sec
	}
}

func BenchmarkMonotonic(b *testing.B) {
	for range b.N {
		start := monotime()
		_ = monotime() - start
	}
}

// func BenchmarkFloat32Add(b *testing.B) {
// 	var a float32
//
// 	for range b.N {
// 		a = a + 1
// 	}
// }
//
// func BenchmarkFloat64Add(b *testing.B) {
// 	var a float64
//
// 	for range b.N {
// 		a = a + 1
// 	}
// }
