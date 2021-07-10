// Package xs contains eXtended actions (xactions) except storage services
// (mirror, ec) and extensions (downloader, lru).
/*
 * Copyright (c) 2021, NVIDIA CORPORATION. All rights reserved.
 */
package xs

import (
	"archive/tar"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/NVIDIA/aistore/3rdparty/glog"
	"github.com/NVIDIA/aistore/cluster"
	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/cmn/cos"
	"github.com/NVIDIA/aistore/cmn/debug"
	"github.com/NVIDIA/aistore/fs"
	"github.com/NVIDIA/aistore/transport"
	"github.com/NVIDIA/aistore/transport/bundle"
	"github.com/NVIDIA/aistore/xaction"
	"github.com/NVIDIA/aistore/xaction/xreg"
)

type (
	archFactory struct {
		xreg.BaseBckEntry
		xact *XactPutArchive
		t    cluster.Target
		uuid string
	}
	archwi struct { // archival work item; implements lrwi
		r   *XactPutArchive
		msg *cmn.ArchiveMsg
		lom *cluster.LOM // of the archive
		fqn string       // workFQN --/--
		fh  *os.File     // --/--
		tw  *tar.Writer
		mu  sync.Mutex
		tsi *cluster.Snode
	}
	XactPutArchive struct {
		xaction.XactDemandBase
		t       cluster.Target
		bckFrom cmn.Bck
		dm      *bundle.DataMover
		workCh  chan *cmn.ArchiveMsg
		pending struct {
			sync.RWMutex
			m map[string]*archwi
		}
	}
)

const (
	maxNumInParallel = 64
)

// interface guard
var (
	_ cluster.Xact    = (*XactPutArchive)(nil)
	_ xreg.BckFactory = (*archFactory)(nil)
	_ lrwi            = (*archwi)(nil)
)

////////////////
// archFactory //
////////////////

func (*archFactory) New(args *xreg.Args) xreg.BucketEntry {
	return &archFactory{t: args.T, uuid: args.UUID}
}

func (*archFactory) Kind() string        { return cmn.ActArchive }
func (p *archFactory) Get() cluster.Xact { return p.xact }

func (p *archFactory) Start(bckFrom cmn.Bck) error {
	var (
		config      = cmn.GCO.Get()
		totallyIdle = config.Timeout.SendFile.D()
		likelyIdle  = config.Timeout.MaxKeepalive.D()
	)
	r := &XactPutArchive{
		XactDemandBase: *xaction.NewXDB(p.uuid, cmn.ActArchive, &bckFrom, totallyIdle, likelyIdle),
		t:              p.t,
		bckFrom:        bckFrom,
		workCh:         make(chan *cmn.ArchiveMsg, maxNumInParallel),
	}
	r.pending.m = make(map[string]*archwi, maxNumInParallel)
	p.xact = r
	r.InitIdle()
	if err := p.newDM(bckFrom, r); err != nil {
		return err
	}
	r.dm.SetXact(r)
	r.dm.Open()

	go r.Run()
	return nil
}

func (p *archFactory) newDM(bckFrom cmn.Bck, r *XactPutArchive) error {
	// NOTE: transport stream name
	trname := "arch-" + bckFrom.Provider + "-" + bckFrom.Name
	dm, err := bundle.NewDataMover(p.t, trname, r.recvObjDM, cluster.RegularPut, bundle.Extra{Multiplier: 1})
	if err != nil {
		return err
	}
	if err := dm.RegRecv(); err != nil {
		return err
	}
	r.dm = dm
	return nil
}

////////////////////
// XactPutArchive //
////////////////////

func (r *XactPutArchive) Begin(msg *cmn.ArchiveMsg) (err error) {
	debug.Assert(strings.HasSuffix(msg.ArchName, cos.ExtTar)) // TODO: NIY
	lom := cluster.AllocLOM(msg.ArchName)
	if err = lom.Init(msg.ToBck); err != nil {
		return
	}
	debug.Assert(lom.FullName() == msg.FullName()) // relying on it

	wi := &archwi{r: r, msg: msg, lom: lom}
	wi.fqn = fs.CSM.GenContentFQN(wi.lom, fs.WorkfileType, fs.WorkfileAppend)

	smap := r.t.Sowner().Get()
	wi.tsi, err = cluster.HrwTarget(msg.ToBck.MakeUname(msg.ArchName), smap)
	if err != nil {
		return
	}

	// NOTE: creating archive at BEGIN time; TODO: cleanup upon ABORT
	if r.t.Snode().ID() == wi.tsi.ID() {
		wi.fh, err = wi.lom.CreateFile(wi.fqn)
		if err != nil {
			return
		}
		wi.tw = tar.NewWriter(wi.fh)
	}
	r.pending.Lock()
	r.pending.m[msg.FullName()] = wi
	r.pending.Unlock()
	return
}

func (r *XactPutArchive) Do(msg *cmn.ArchiveMsg) {
	r.IncPending()
	r.workCh <- msg
}

func (r *XactPutArchive) Run() {
	var err error
	glog.Infoln(r.String())
	for {
		select {
		case msg := <-r.workCh:
			fullname := msg.FullName()
			r.pending.RLock()
			wi := r.pending.m[fullname]
			r.pending.RUnlock()
			var (
				smap             = r.t.Sowner().Get()
				lrit             = &lriterator{}
				ignoreBackendErr = !msg.IsList() // list defaults to aborting on errors other than non-existence
				freeLOM          = false         // not delegating the responsibility - doing it
			)
			lrit.init(r, r.t, &msg.ListRangeMsg, ignoreBackendErr, freeLOM)
			if msg.IsList() {
				err = lrit.iterateList(wi, smap)
			} else {
				err = lrit.iterateRange(wi, smap)
			}
			if r.Aborted() || err != nil {
				goto fin
			}
			if r.t.Snode().ID() == wi.tsi.ID() {
				go r.finalize(wi, fullname)
			} else {
				r.DecPending()
			}
		case <-r.IdleTimer():
			goto fin
		case <-r.ChanAbort():
			goto fin
		}
	}
fin:
	r.XactDemandBase.Stop()
	config := cmn.GCO.Get()
	if q := r.dm.Quiesce(config.Rebalance.Quiesce.D()); q == cluster.QuiAborted && err == nil {
		err = cmn.NewAbortedError(r.String())
	}
	r.dm.Close(err)
	r.dm.UnregRecv()
	r.Finish(err)
}

func (r *XactPutArchive) doSend(lom *cluster.LOM, wi *archwi, fh cos.ReadOpenCloser) {
	o := transport.AllocSend()
	hdr := &o.Hdr
	{
		hdr.Bck = wi.msg.ToBck
		hdr.ObjName = lom.ObjName
		hdr.ObjAttrs.CopyFrom(lom.ObjAttrs())
		hdr.Opaque = []byte(wi.msg.FullName()) // NOTE
	}
	o.Callback = func(_ transport.ObjHdr, _ io.ReadCloser, _ interface{}, _ error) {
		cluster.FreeLOM(lom)
	}
	r.dm.Send(o, fh, wi.tsi)
}

func (r *XactPutArchive) recvObjDM(hdr transport.ObjHdr, objReader io.Reader, err error) {
	defer transport.FreeRecv(objReader)
	if err != nil && !cos.IsEOF(err) {
		glog.Error(err)
		return
	}
	defer cos.DrainReader(objReader)

	r.pending.RLock()
	wi, ok := r.pending.m[string(hdr.Opaque)] // NOTE: fullname
	r.pending.RUnlock()
	debug.Assert(ok)
	debug.Assert(wi.tsi.ID() == r.t.Snode().ID())

	wi.addToArch(nil, &hdr, objReader)
}

func (r *XactPutArchive) finalize(wi *archwi, fullname string) {
	if wi == nil || wi.tw == nil {
		r.DecPending()
		return // nothing to do
	}
	time.Sleep(3 * time.Second) // TODO -- FIXME: via [target => last] accounting

	errCode, err := r._fini(wi)
	if err != nil {
		glog.Errorf("%s: %v(%d)", r.t.Snode(), err, errCode) // TODO
	}
	r.pending.Lock()
	delete(r.pending.m, fullname)
	r.pending.Unlock()
	r.DecPending()
}

func (r *XactPutArchive) _fini(wi *archwi) (errCode int, err error) {
	wi.tw.Close()

	size, err := wi.fh.Seek(0, io.SeekCurrent)
	if err != nil {
		debug.AssertNoErr(err)
		return
	}
	wi.lom.SetSize(size)
	wi.lom.SetCksum(cos.NoneCksum)
	cos.Close(wi.fh)

	errCode, err = r.t.FinalizeObj(wi.lom, wi.fqn, size)
	cluster.FreeLOM(wi.lom)
	return
}

func (r *XactPutArchive) Stats() cluster.XactStats {
	baseStats := r.XactDemandBase.Stats().(*xaction.BaseXactStatsExt)
	baseStats.Ext = &xaction.BaseXactDemandStatsExt{IsIdle: r.Pending() == 0}
	return baseStats
}

////////////
// archwi //
////////////
func (wi *archwi) do(lom *cluster.LOM, lrit *lriterator) (err error) {
	var (
		t       = lrit.t
		coldGet bool
	)
	debug.Assert(t == wi.r.t)
	debug.Assert(wi.r.bckFrom.Equal(lom.Bucket()))
	debug.Assert(lom.Bprops() != nil) // must be init-ed
	if err = lom.Load(false /*cache it*/, false /*locked*/); err != nil {
		if !cmn.IsObjNotExist(err) {
			return err
		}
		coldGet = lom.Bck().IsRemote()
		if !coldGet {
			return err
		}
	}

	if coldGet {
		if errCode, err := t.Backend(lom.Bck()).GetObj(lrit.ctx, lom); err != nil {
			if errCode == http.StatusNotFound || cmn.IsObjNotExist(err) {
				return nil
			}
			if lrit.ignoreBackendErr {
				glog.Warning(err)
				err = nil
			}
			return err
		}
	}

	fh, err := cos.NewFileHandle(lom.FQN)
	debug.AssertNoErr(err)
	if err != nil {
		return err
	}
	if t.Snode().ID() != wi.tsi.ID() {
		wi.r.doSend(lom, wi, fh)
		return
	}
	debug.Assert(wi.fh != nil) // see Begin
	wi.addToArch(lom, nil, fh)
	cluster.FreeLOM(lom)
	cos.Close(fh)
	return
}

func (wi *archwi) addToArch(lom *cluster.LOM, hdr *transport.ObjHdr, reader io.Reader) {
	tarhdr := new(tar.Header)
	tarhdr.Typeflag = tar.TypeReg

	if lom != nil { // local
		tarhdr.Size = lom.SizeBytes()
		tarhdr.ModTime = lom.Atime()
		tarhdr.Name = lom.FullName()
	} else { // recv
		tarhdr.Size = hdr.ObjAttrs.Size
		tarhdr.ModTime = time.Unix(0, hdr.ObjAttrs.Atime)
		tarhdr.Name = hdr.FullName()
	}

	// one at a time
	wi.mu.Lock()
	err := wi.tw.WriteHeader(tarhdr)
	debug.AssertNoErr(err)

	_, err = io.Copy(wi.tw, reader)
	wi.mu.Unlock()
	debug.AssertNoErr(err)
}