// Package xs contains eXtended actions (xactions) except storage services
// (mirror, ec) and extensions (downloader, lru).
/*
 * Copyright (c) 2018-2021, NVIDIA CORPORATION. All rights reserved.
 */
package xs

import (
	"sync"

	"github.com/NVIDIA/aistore/3rdparty/glog"
	"github.com/NVIDIA/aistore/cluster"
	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/fs"
	"github.com/NVIDIA/aistore/fs/mpather"
	"github.com/NVIDIA/aistore/xact"
	"github.com/NVIDIA/aistore/xact/xreg"
)

type (
	llcFactory struct {
		xreg.RenewBase
		xctn *xactLLC
	}
	xactLLC struct {
		xact.BckJog
	}
)

// interface guard
var (
	_ cluster.Xact   = (*xactLLC)(nil)
	_ xreg.Renewable = (*llcFactory)(nil)
)

////////////////
// llcFactory //
////////////////

func (*llcFactory) New(args xreg.Args, bck *cluster.Bck) xreg.Renewable {
	p := &llcFactory{RenewBase: xreg.RenewBase{Args: args, Bck: bck}}
	p.Bck = bck
	return p
}

func (p *llcFactory) Start() error {
	xctn := newXactLLC(p.T, p.UUID(), p.Bck)
	p.xctn = xctn
	go xctn.Run(nil)
	return nil
}

func (*llcFactory) Kind() string        { return cmn.ActLoadLomCache }
func (p *llcFactory) Get() cluster.Xact { return p.xctn }

func (*llcFactory) WhenPrevIsRunning(xreg.Renewable) (xreg.WPR, error) { return xreg.WprUse, nil }

/////////////
// xactLLC //
/////////////

func newXactLLC(t cluster.Target, uuid string, bck *cluster.Bck) (r *xactLLC) {
	r = &xactLLC{}
	mpopts := &mpather.JoggerGroupOpts{
		T:        t,
		Bck:      bck.Bck,
		CTs:      []string{fs.ObjectType},
		VisitObj: func(*cluster.LOM, []byte) error { return nil },
		DoLoad:   mpather.Load,
	}
	r.BckJog.Init(uuid, cmn.ActLoadLomCache, bck, mpopts)
	return
}

func (r *xactLLC) Run(*sync.WaitGroup) {
	r.BckJog.Run()
	glog.Infoln(r.Name())
	err := r.BckJog.Wait()
	r.Finish(err)
}