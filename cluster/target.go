// Package cluster provides local access to cluster-level metadata
/*
 * Copyright (c) 2018-2020, NVIDIA CORPORATION. All rights reserved.
 */
package cluster

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/dbdriver"
	"github.com/NVIDIA/aistore/fs"
	"github.com/NVIDIA/aistore/memsys"
)

type RecvType int

const (
	ColdGet RecvType = iota
	WarmGet
	Migrated
)

type (
	GFNType int
	GFN     interface {
		Activate() bool
		Deactivate()
	}
)

const (
	GFNGlobal GFNType = iota
	GFNLocal
)

type CloudProvider interface {
	Provider() string
	MaxPageSize() uint
	GetObj(ctx context.Context, fqn string, lom *LOM) (err error, errCode int)
	PutObj(ctx context.Context, r io.Reader, lom *LOM) (version string, err error, errCode int)
	DeleteObj(ctx context.Context, lom *LOM) (error, int)
	HeadObj(ctx context.Context, lom *LOM) (objMeta cmn.SimpleKVs, err error, errCode int)

	HeadBucket(ctx context.Context, bck *Bck) (bucketProps cmn.SimpleKVs, err error, errCode int)
	ListObjects(ctx context.Context, bck *Bck, msg *cmn.SelectMsg) (bckList *cmn.BucketList, err error, errCode int)
	ListBuckets(ctx context.Context, query cmn.QueryBcks) (buckets cmn.BucketNames, err error, errCode int)
}

// a callback called by EC PUT jogger after the object is processed and
// all its slices/replicas are sent to other targets
type OnFinishObj = func(lom *LOM, err error)

type (
	DataMover interface {
		RegRecv() error
		Open()
		Close()
		UnregRecv()
	}
	PutObjectParams struct {
		Reader       io.ReadCloser
		WorkFQN      string
		RecvType     RecvType
		Cksum        *cmn.Cksum // checksum to check
		Started      time.Time
		WithFinalize bool // determines if we should also finalize the object
		SkipEncode   bool // Do not run EC encode after finalizing
	}
	CopyObjectParams struct {
		BckTo *Bck
		Buf   []byte
		DM    DataMover
	}
	SendToParams struct {
		Reader    cmn.ReadOpenCloser
		BckTo     *Bck
		ObjNameTo string
		Tsi       *Snode
		DM        DataMover
		Locked    bool
	}
	PromoteFileParams struct {
		SrcFQN    string
		Bck       *Bck
		ObjName   string
		Cksum     *cmn.Cksum
		Overwrite bool
		KeepOrig  bool
		Verbose   bool
	}
)

// NOTE: For implementations, please refer to ais/tgtifimpl.go and ais/httpcommon.go
type Target interface {
	Node
	FSHC(err error, path string)
	GetMMSA() *memsys.MMSA
	GetSmallMMSA() *memsys.MMSA
	GetFSPRG() fs.PathRunGroup
	GetDB() dbdriver.Driver
	Cloud(*Bck) CloudProvider
	RunLRU(id string, force bool, bcks ...cmn.Bck)

	GetObject(w io.Writer, lom *LOM, started time.Time) error
	PutObject(lom *LOM, params PutObjectParams) error
	EvictObject(lom *LOM) error
	CopyObject(lom *LOM, params CopyObjectParams, localOnly ...bool) (bool, error)
	GetCold(ctx context.Context, lom *LOM, prefetch bool) (error, int)
	PromoteFile(params PromoteFileParams) (lom *LOM, err error)
	LookupRemoteSingle(lom *LOM, si *Snode) bool
	SendTo(lom *LOM, params SendToParams) error

	CheckCloudVersion(ctx context.Context, lom *LOM) (vchanged bool, err error, errCode int)
	GetGFN(gfnType GFNType) GFN
	GetXactRegistry() XactRegistry
	Health(si *Snode, timeout time.Duration, query url.Values) ([]byte, error, int)
	RebalanceNamespace(si *Snode) ([]byte, int, error)
	BMDVersionFixup(r *http.Request, bck cmn.Bck, sleep bool)
	K8sNodeName() string
}

type RebManager interface {
	RunResilver(id string, skipMisplaced bool)
	RunRebalance(smap *Smap, rebID int64)
}
