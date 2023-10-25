// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
)

func TestEthTxGossip(t *testing.T) {
	tests := []struct {
		name             string
		sender1, sender2 func(vm1, vm2 *VM) *commonEng.SenderTest
	}{
		{
			name: "push gossip",
			sender1: func(vm1, vm2 *VM) *commonEng.SenderTest {
				return &commonEng.SenderTest{
					SendAppGossipF: func(ctx context.Context, bytes []byte) error {
						return vm2.AppGossip(ctx, vm1.ctx.NodeID, bytes)
					},
				}
			},
			sender2: func(vm1, vm2 *VM) *commonEng.SenderTest {
				return &commonEng.SenderTest{}
			},
		},
		{
			name: "pull gossip",
			sender1: func(vm1, vm2 *VM) *commonEng.SenderTest {
				return &commonEng.SenderTest{
					SendAppResponseF: func(ctx context.Context, _ ids.NodeID, requestID uint32, bytes []byte) error {
						return vm2.AppResponse(ctx, vm1.ctx.NodeID, requestID,
							bytes)
					},
				}
			},
			sender2: func(vm1, vm2 *VM) *commonEng.SenderTest {
				return &commonEng.SenderTest{
					SendAppRequestF: func(ctx context.Context, _ set.Set[ids.NodeID], requestID uint32, bytes []byte) error {
						go vm1.AppRequest(ctx, vm2.ctx.NodeID, requestID, time.Time{}, bytes)
						return nil
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			vm1 := &VM{}
			vm1SnowCtx := snow.DefaultContextTest()

			vm2 := &VM{}
			vm2SnowCtx := snow.DefaultContextTest()

			keyFactory := secp256k1.Factory{}
			pk, err := keyFactory.NewPrivateKey()
			require.NoError(err)
			address := GetEthAddress(pk)
			genesis := newPrefundedGenesis(params.Ether, address)
			genesisBytes, err := json.Marshal(genesis)
			require.NoError(err)

			validatorState := &validators.TestState{
				GetCurrentHeightF: func(ctx context.Context) (uint64, error) {
					return 0, nil
				},
				GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
					return map[ids.NodeID]*validators.GetValidatorOutput{
						vm1SnowCtx.NodeID: nil,
						vm2SnowCtx.NodeID: nil,
					}, nil
				},
			}

			vm1SnowCtx.ValidatorState = validatorState
			vm2SnowCtx.ValidatorState = validatorState

			require.NoError(vm1.Initialize(
				context.Background(),
				vm1SnowCtx,
				manager.NewMemDB(&version.Semantic{}),
				genesisBytes,
				nil,
				nil,
				make(chan commonEng.Message),
				nil,
				tt.sender1(vm1, vm2),
			))
			require.NoError(vm1.SetState(context.Background(), snow.NormalOp))

			require.NoError(vm2.Initialize(
				context.Background(),
				vm2SnowCtx,
				manager.NewMemDB(&version.Semantic{}),
				genesisBytes,
				nil,
				nil,
				make(chan commonEng.Message),
				nil,
				tt.sender2(vm1, vm2),
			))
			require.NoError(vm2.SetState(context.Background(), snow.NormalOp))

			tx := types.NewTransaction(0, address, big.NewInt(10), 100_000, big.NewInt(params.LaunchMinGasPrice), nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm1.chainID), pk.ToECDSA())
			require.NoError(err)

			require.False(vm1.txPool.Has(signedTx.Hash()))
			require.False(vm2.txPool.Has(signedTx.Hash()))

			require.NoError(vm1.txPool.AddRemote(signedTx))
			require.True(vm1.txPool.Has(signedTx.Hash()))

			update := make(chan core.NewTxsEvent)
			vm2.txPool.SubscribeNewTxsEvent(update)
			<-update

			require.True(vm2.txPool.Has(signedTx.Hash()))
		})
	}
}

func TestAtomicTxGossip(t *testing.T) {
	tests := []struct {
		name    string
		sender1 func(vm1, vm2 *VM, done chan struct{}) *commonEng.SenderTest
		sender2 func(vm1, vm2 *VM) *commonEng.SenderTest
	}{
		{
			name: "push gossip",
			sender1: func(vm1, vm2 *VM, done chan struct{}) *commonEng.
				SenderTest {
				return &commonEng.SenderTest{
					SendAppGossipF: func(ctx context.Context, bytes []byte) error {
						_ = vm2.AppGossip(ctx, vm1.ctx.NodeID, bytes)
						done <- struct{}{}
						return nil
					},
				}
			},
			sender2: func(vm1, vm2 *VM) *commonEng.SenderTest {
				return &commonEng.SenderTest{}
			},
		},
		{
			name: "pull gossip",
			sender1: func(vm1, vm2 *VM, done chan struct{}) *commonEng.
				SenderTest {
				return &commonEng.SenderTest{
					SendAppResponseF: func(ctx context.Context, _ ids.NodeID, requestID uint32, bytes []byte) error {
						_ = vm2.AppResponse(ctx, vm1.ctx.NodeID, requestID, bytes)
						done <- struct{}{}
						return nil
					},
				}
			},
			sender2: func(vm1, vm2 *VM) *commonEng.
				SenderTest {
				return &commonEng.SenderTest{
					SendAppRequestF: func(ctx context.Context, _ set.Set[ids.NodeID], requestID uint32, bytes []byte) error {
						go vm1.AppRequest(ctx, vm2.ctx.NodeID, requestID, time.Time{}, bytes)
						return nil
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			vm1 := &VM{}
			vm1SnowCtx := snow.DefaultContextTest()

			vm2 := &VM{}
			vm2SnowCtx := snow.DefaultContextTest()

			keyFactory := secp256k1.Factory{}
			pk, err := keyFactory.NewPrivateKey()
			require.NoError(err)
			address := GetEthAddress(pk)
			genesis := newPrefundedGenesis(params.Ether, address)
			genesisBytes, err := json.Marshal(genesis)
			require.NoError(err)

			validatorState := &validators.TestState{
				GetCurrentHeightF: func(ctx context.Context) (uint64, error) {
					return 0, nil
				},
				GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
					return map[ids.NodeID]*validators.GetValidatorOutput{
						vm1SnowCtx.NodeID: nil,
						vm2SnowCtx.NodeID: nil,
					}, nil
				},
				GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
					return ids.Empty, nil
				},
			}

			vm1SnowCtx.ValidatorState = validatorState
			vm2SnowCtx.ValidatorState = validatorState

			avaxID := ids.GenerateTestID()
			vm1SnowCtx.AVAXAssetID = avaxID
			vm2SnowCtx.AVAXAssetID = avaxID

			from := ids.GenerateTestID()
			to := ids.GenerateTestID()

			vm1SnowCtx.ChainID = from
			vm2SnowCtx.ChainID = from

			vm1SnowCtx.AVAXAssetID = avaxID
			vm2SnowCtx.AVAXAssetID = avaxID

			done := make(chan struct{})
			require.NoError(vm1.Initialize(
				context.Background(),
				vm1SnowCtx,
				manager.NewMemDB(&version.Semantic{}),
				genesisBytes,
				nil,
				nil,
				make(chan commonEng.Message),
				nil,
				tt.sender1(vm1, vm2, done),
			))
			require.NoError(vm1.SetState(context.Background(), snow.NormalOp))

			require.NoError(vm2.Initialize(
				context.Background(),
				vm2SnowCtx,
				manager.NewMemDB(&version.Semantic{}),
				genesisBytes,
				nil,
				nil,
				make(chan commonEng.Message),
				nil,
				tt.sender2(vm1, vm2),
			))
			require.NoError(vm2.SetState(context.Background(), snow.NormalOp))

			tx := &Tx{
				UnsignedAtomicTx: &UnsignedExportTx{
					BlockchainID:     from,
					DestinationChain: to,

					Ins: []EVMInput{
						{
							Address: address,
							Amount:  1_000_000_000,
							AssetID: avaxID,
						},
					},
					ExportedOutputs: []*avax.TransferableOutput{
						{
							Asset: avax.Asset{ID: avaxID},
							Out: &secp256k1fx.TransferOutput{
								Amt: 1,
								OutputOwners: secp256k1fx.OutputOwners{
									Locktime:  0,
									Threshold: 1,
									Addrs:     []ids.ShortID{pk.Address()},
								},
							},
						},
					},
				},
			}

			require.NoError(tx.Sign(vm1.codec, [][]*secp256k1.PrivateKey{{pk}}))
			require.NoError(vm1.mempool.AddLocalTx(tx))

			for {
				// It's possible we aren't served the tx on a given gossip
				// cycle, so keep polling until we see it.
				<-done

				if vm2.mempool.has(tx.ID()) {
					break
				}
			}

		})
	}
}
