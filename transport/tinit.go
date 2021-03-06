// Package transport provides streaming object-based transport over http for intra-cluster continuous
// intra-cluster communications (see README for details and usage example).
/*
 * Copyright (c) 2018-2022, NVIDIA CORPORATION. All rights reserved.
 */
package transport

import (
	"container/heap"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/NVIDIA/aistore/3rdparty/glog"
	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/cmn/cos"
	"github.com/NVIDIA/aistore/memsys"
)

// transport defaults
const (
	dfltMaxHeaderSize = memsys.PageSize // max header buffer size (see: maxHeaderSize, config.Transport.MaxHeaderSize)
	dfltBurstNum      = 32              // burst size (see: config.Transport.Burst)
	dfltTick          = time.Second
	dfltIdleTeardown  = 4 * time.Second // (see config.Transport.IdleTeardown)
)

var (
	maxHeaderSize int
	verbose       bool
)

func init() {
	nextSID.Store(100)
	handlers = make(map[string]*handler, 16)
	mu = &sync.RWMutex{}
	verbose = bool(glog.FastV(4, glog.SmoduleTransport))
}

func Init(st cos.StatsTracker, config *cmn.Config) *StreamCollector {
	maxHeaderSize = dfltMaxHeaderSize
	if config.Transport.MaxHeaderSize > 0 {
		maxHeaderSize = config.Transport.MaxHeaderSize
	}

	statsTracker = st
	// real stream collector
	gc = &collector{
		stopCh:  cos.NewStopCh(),
		ctrlCh:  make(chan ctrl, 64),
		streams: make(map[string]*streamBase, 64),
		heap:    make([]*streamBase, 0, 64), // min-heap sorted by stream.time.ticks
	}
	heap.Init(gc)

	sc = &StreamCollector{}
	return sc
}

func burst(config *cmn.Config) (burst int) {
	if burst = config.Transport.Burst; burst == 0 {
		burst = dfltBurstNum
	}
	if a := os.Getenv("AIS_STREAM_BURST_NUM"); a != "" {
		if burst64, err := strconv.ParseInt(a, 10, 0); err != nil {
			glog.Error(err)
		} else {
			burst = int(burst64)
		}
	}
	return
}
