// Package mirror provides local mirroring and replica management
/*
 * Copyright (c) 2018-2020, NVIDIA CORPORATION. All rights reserved.
 */
package mirror

import (
	"fmt"
	"time"

	"github.com/NVIDIA/aistore/3rdparty/glog"
	"github.com/NVIDIA/aistore/cluster"
	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/fs"
	"github.com/NVIDIA/aistore/memsys"
	"github.com/NVIDIA/aistore/transport"
)

// XactBckCopy copies a bucket locally within the same cluster

type (
	XactBckCopy struct {
		xactBckBase
		slab    *memsys.Slab
		bckFrom *cluster.Bck
		bckTo   *cluster.Bck
		dm      *transport.DataMover
	}
	bccJogger struct { // one per mountpath
		joggerBckBase
		parent *XactBckCopy
		buf    []byte
	}
)

//
// public methods
//

func NewXactBCC(id string, bckFrom, bckTo *cluster.Bck, t cluster.Target, slab *memsys.Slab,
	dm *transport.DataMover) *XactBckCopy {
	return &XactBckCopy{
		xactBckBase: *newXactBckBase(id, cmn.ActCopyBucket, bckTo.Bck, t),
		slab:        slab,
		bckFrom:     bckFrom,
		bckTo:       bckTo,
		dm:          dm,
	}
}

func (r *XactBckCopy) Run() (err error) {
	r.dm.Open()

	mpathCount := r.runJoggers()

	glog.Infoln(r.String(), r.bckFrom.Bck, "=>", r.bckTo.Bck)
	err = r.xactBckBase.waitDone(mpathCount)

	time.Sleep(2 * time.Second) // TODO -- FIXME: quiesce
	r.dm.Close()
	r.dm.UnregRecv()

	r.Finish(err)
	return
}

func (r *XactBckCopy) String() string { return fmt.Sprintf("%s <= %s", r.XactBase.String(), r.bckFrom) }

//
// private methods
//

func (r *XactBckCopy) runJoggers() (mpathCount int) {
	var (
		availablePaths, _ = fs.Get()
		config            = cmn.GCO.Get()
	)
	mpathCount = len(availablePaths)
	r.xactBckBase.init(mpathCount)
	for _, mpathInfo := range availablePaths {
		bccJogger := newBCCJogger(r, mpathInfo, config)
		mpathLC := mpathInfo.MakePathCT(r.bckFrom.Bck, fs.ObjectType)
		r.mpathers[mpathLC] = bccJogger
		go bccJogger.jog()
	}
	return
}

//
// mpath bccJogger - main
//

func newBCCJogger(parent *XactBckCopy, mpathInfo *fs.MountpathInfo, config *cmn.Config) *bccJogger {
	j := &bccJogger{
		joggerBckBase: joggerBckBase{
			parent:    &parent.xactBckBase,
			bck:       parent.bckFrom.Bck,
			mpathInfo: mpathInfo,
			config:    config,
			skipLoad:  true,
			stopCh:    cmn.NewStopCh(),
		},
		parent: parent,
	}
	j.joggerBckBase.callback = j.copyObject
	return j
}

func (j *bccJogger) jog() {
	glog.Infof("jogger[%s/%s] started", j.mpathInfo, j.parent.bckFrom.Bck)
	j.buf = j.parent.slab.Alloc()
	j.joggerBckBase.jog()
	j.parent.slab.Free(j.buf)
}

func (j *bccJogger) copyObject(lom *cluster.LOM) error {
	var (
		params      = cluster.CopyObjectParams{BckTo: j.parent.bckTo, Buf: j.buf, DM: j.parent.dm}
		copied, err = j.parent.Target().CopyObject(lom, params)
	)
	if copied {
		j.parent.ObjectsInc()
		j.parent.BytesAdd(lom.Size() + lom.Size()) // FIXME: depends on whether local <-> remote
		j.num++
		if (j.num % throttleNumObjects) == 0 {
			if cs := fs.GetCapStatus(); cs.Err != nil {
				what := fmt.Sprintf("%s(%q)", j.parent.Kind(), j.parent.ID())
				return cmn.NewAbortedErrorDetails(what, cs.Err.Error())
			}
		}
	}
	if cmn.IsErrOOS(err) {
		what := fmt.Sprintf("%s(%q)", j.parent.Kind(), j.parent.ID())
		return cmn.NewAbortedErrorDetails(what, err.Error())
	}
	return err
}
