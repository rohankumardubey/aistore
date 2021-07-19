// Package xs contains eXtended actions (xactions) except storage services
// (mirror, ec) and extensions (downloader, lru).
/*
 * Copyright (c) 2018-2020, NVIDIA CORPORATION. All rights reserved.
 */
package xs

import (
	"github.com/NVIDIA/aistore/3rdparty/glog"
	"github.com/NVIDIA/aistore/cluster"
	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/fs"
	"github.com/NVIDIA/aistore/fs/mpather"
	"github.com/NVIDIA/aistore/xaction"
	"github.com/NVIDIA/aistore/xaction/xreg"
)

type (
	llcFactory struct {
		xreg.BaseEntry
		t    cluster.Target
		xact *xactLLC
		uuid string
	}
	xactLLC struct {
		xaction.XactBckJog
	}
)

// interface guard
var (
	_ cluster.Xact = (*xactLLC)(nil)
	_ xreg.Factory = (*llcFactory)(nil)
)

////////////////
// llcFactory //
////////////////

func (*llcFactory) New(args xreg.Args) xreg.Renewable {
	return &llcFactory{t: args.T, uuid: args.UUID}
}

func (p *llcFactory) Start(bck cmn.Bck) error {
	xact := newXactLLC(p.t, p.uuid, bck)
	p.xact = xact
	go xact.Run()
	return nil
}

func (*llcFactory) Kind() string        { return cmn.ActLoadLomCache }
func (p *llcFactory) Get() cluster.Xact { return p.xact }

// overriding default to keep the previous/current
func (*llcFactory) WhenPrevIsRunning(xreg.Renewable) (xreg.WPR, error) { return xreg.WprUse, nil }

/////////////
// xactLLC //
/////////////

func newXactLLC(t cluster.Target, uuid string, bck cmn.Bck) (r *xactLLC) {
	r = &xactLLC{}
	mpopts := &mpather.JoggerGroupOpts{
		T:        t,
		Bck:      bck,
		CTs:      []string{fs.ObjectType},
		VisitObj: func(*cluster.LOM, []byte) error { return nil },
		DoLoad:   mpather.Load,
	}
	r.XactBckJog.Init(uuid, cmn.ActLoadLomCache, bck, mpopts)
	return
}

func (r *xactLLC) Run() {
	r.XactBckJog.Run()
	glog.Infoln(r.String())
	err := r.XactBckJog.Wait()
	r.Finish(err)
}
