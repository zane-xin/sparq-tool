package dev

import "io"

// Streams 描述开发会话启动的子进程输入输出。
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (s Streams) normalized() Streams {
	if s.Stdout == nil {
		s.Stdout = io.Discard
	}
	if s.Stderr == nil {
		s.Stderr = io.Discard
	}
	return s
}
