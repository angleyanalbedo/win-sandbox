package cmd

import "time"

// 共享的命令行标志变量
var (
	image   string
	memory  int
	cpus    int
	network string
	timeout time.Duration
	verbose bool
)
