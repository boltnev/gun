package main

import (
	"flag"
	"fmt"
	"time"
)

var concurrency int
var timeout time.Duration

func init() {
	flag.IntVar(&concurrency, "c", 1, "concurrency threads")
	flag.IntVar(&concurrency, "concurrency", 1, "concurrency threads")

	flag.DurationVar(&timeout, "t", time.Second*10, "timeout to run")
	flag.DurationVar(&timeout, "timeout", time.Second*10, "timeout to run")

	flag.Parse()
}

func main() {
	fmt.Printf("concurrency: %d\n", concurrency)
	fmt.Printf("timeout: %s\n", timeout)
}
