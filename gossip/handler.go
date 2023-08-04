// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ethereum/go-ethereum/log"
)

var _ p2p.Handler = (*Handler[Gossipable])(nil)

func NewHandler[T Gossipable](set Set[T], codec codec.Manager, codecVersion uint16) *Handler[T] {
	return &Handler[T]{
		set:          set,
		codec:        codec,
		codecVersion: codecVersion,
	}
}

type Handler[T Gossipable] struct {
	set          Set[T]
	codec        codec.Manager
	codecVersion uint16
}

func (h Handler[T]) AppRequest(_ context.Context, nodeID ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, error) {
	request := PullGossipRequest{}
	if _, err := h.codec.Unmarshal(requestBytes, &request); err != nil {
		log.Info("failed to unmarshal gossip request", "nodeID", nodeID, "err", err)
		return nil, nil
	}
	var peerFilter Filter
	if _, err := h.codec.Unmarshal(request.Filter, &peerFilter); err != nil {
		log.Debug("failed to unmarshal bloom filter", "nodeID", nodeID, "err", err)
		return nil, nil
	}

	// filter out what the requesting peer already knows about
	unknown := h.set.Get(func(gossipable T) bool {
		return !peerFilter.Has(gossipable)
	})

	gossipBytes := make([][]byte, 0, len(unknown))
	for _, gossipable := range unknown {
		bytes, err := h.codec.Marshal(h.codecVersion, gossipable)
		if err != nil {
			return nil, err
		}
		gossipBytes = append(gossipBytes, bytes)
	}

	response := PullGossipResponse{
		GossipBytes: gossipBytes,
	}
	responseBytes, err := h.codec.Marshal(h.codecVersion, response)
	if err != nil {
		return nil, err
	}

	return responseBytes, nil
}

func (h Handler[T]) AppGossip(context.Context, ids.NodeID, []byte) error {
	return nil
}

func (h Handler[T]) CrossChainAppRequest(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
	return nil, nil
}
