package execclient

import (
	"errors"
	"time"
)

const DefaultBytes int64 = 64 << 20

type Limits struct {
	MaxRecordBytes, MaxResultBytes, MaxShutdownBytes int64
	StderrTailBytes, EventQueueSize                  int
	ShutdownGrace                                    time.Duration
}

func (l Limits) normalized() (Limits, error) {
	if l.MaxShutdownBytes < 0 || l.MaxRecordBytes < 0 || l.MaxResultBytes < 0 || l.StderrTailBytes < 0 || l.EventQueueSize < 0 || l.ShutdownGrace < 0 {
		return l, errors.New("negative process limit")
	}
	if l.MaxRecordBytes == 0 {
		l.MaxRecordBytes = DefaultBytes
	}
	if l.MaxResultBytes == 0 {
		l.MaxResultBytes = DefaultBytes
	}
	if l.MaxShutdownBytes == 0 {
		l.MaxShutdownBytes = DefaultBytes
	}
	if l.StderrTailBytes == 0 {
		l.StderrTailBytes = 64 << 10
	}
	if l.EventQueueSize == 0 {
		l.EventQueueSize = 16
	}
	if l.ShutdownGrace == 0 {
		l.ShutdownGrace = 10 * time.Second
	}
	return l, nil
}
