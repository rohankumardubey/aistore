// Package xact provides core functionality for the AIStore eXtended Actions (xactions).
/*
 * Copyright (c) 2018-2021, NVIDIA CORPORATION. All rights reserved.
 */
package xact

import (
	"fmt"
	"time"

	"github.com/NVIDIA/aistore/cluster"
	"github.com/NVIDIA/aistore/cmn"
)

type (
	Snap struct {
		ID        string    `json:"id"`
		Kind      string    `json:"kind"`
		Bck       cmn.Bck   `json:"bck"`
		StartTime time.Time `json:"start-time"`
		EndTime   time.Time `json:"end-time"`
		Stats     Stats     `json:"stats"` // common stats counters (see below)
		AbortedX  bool      `json:"aborted"`
	}

	Stats struct {
		Objs     int64 `json:"loc-objs,string"`  // locally processed
		Bytes    int64 `json:"loc-bytes,string"` //
		OutObjs  int64 `json:"out-objs,string"`  // transmit
		OutBytes int64 `json:"out-bytes,string"` //
		InObjs   int64 `json:"in-objs,string"`   // receive
		InBytes  int64 `json:"in-bytes,string"`
	}

	SnapExt struct {
		Snap
		Ext interface{} `json:"ext"`
	}
	BaseDemandStatsExt struct {
		IsIdle bool `json:"is_idle"`
	}

	// NOTE: see closely related `api.XactReqArgs` and comments
	// TODO: apc package, here and elsewhere
	QueryMsg struct {
		ID          string      `json:"id"`
		Kind        string      `json:"kind"`
		Bck         cmn.Bck     `json:"bck"`
		OnlyRunning *bool       `json:"show_active"`
		Buckets     []cmn.Bck   `json:"buckets,omitempty"`
		DaemonID    string      `json:"node,omitempty"`
		Ext         interface{} `json:"ext"`
	}

	QueryMsgLRU struct {
		Force bool `json:"force"`
	}
)

// interface guard
var _ cluster.XactSnap = (*Snap)(nil)

///////////////
// Snap //
///////////////

func (b *Snap) IsAborted() bool { return b.AbortedX }
func (b *Snap) Running() bool   { return b.EndTime.IsZero() }
func (b *Snap) Finished() bool  { return !b.EndTime.IsZero() }

// Idle is:
// - stat.IsIdle for on-demand xactions
// - !stat.Running() for the rest of xactions
// MorphMarshal cannot be used to read any stats as BaseDemandStatsExt because
// upcasting is unsupported (uknown fields are forbidden).
func (b *SnapExt) Idle() bool {
	if b.Ext == nil {
		return !b.Running()
	}
	if vals, ok := b.Ext.(map[string]interface{}); ok {
		if idle, ok := vals["is_idle"].(bool); ok {
			return idle
		}
	}
	return !b.Running()
}

//////////////
// QueryMsg //
//////////////
func (msg *QueryMsg) String() (s string) {
	if msg.ID == "" {
		s = fmt.Sprintf("xmsg-%s", msg.Kind)
	} else {
		s = fmt.Sprintf("xmsg-%s[%s]", msg.Kind, msg.ID)
	}
	if msg.Bck.IsEmpty() {
		return
	}
	return fmt.Sprintf("%s, bucket %s", s, msg.Bck)
}
