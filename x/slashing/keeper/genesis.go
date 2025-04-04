package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// InitGenesis initializes default parameters and the keeper's address to
// pubkey map.
func (keeper Keeper) InitGenesis(ctx sdk.Context, stakingKeeper types.StakingKeeper, data *types.GenesisState) {
	err := stakingKeeper.IterateValidators(ctx,
		func(index int64, validator stakingtypes.ValidatorI) bool {
			consPk, err := validator.ConsPubKey()
			if err != nil {
				panic(err)
			}

			err = keeper.AddPubkey(ctx, consPk)
			if err != nil {
				panic(err)
			}
			return false
		},
	)
	if err != nil {
		panic(err)
	}

	for _, info := range data.SigningInfos {
		address, err := keeper.sk.ConsensusAddressCodec().StringToBytes(info.Address)
		if err != nil {
			panic(err)
		}
		err = keeper.SetValidatorSigningInfo(ctx, address, info.ValidatorSigningInfo)
		if err != nil {
			panic(err)
		}
	}

	for _, array := range data.MissedBlocks {
		address, err := keeper.sk.ConsensusAddressCodec().StringToBytes(array.Address)
		if err != nil {
			panic(err)
		}

		for _, missed := range array.MissedBlocks {
			if err := keeper.SetMissedBlockBitmapValue(ctx, address, missed.Index, missed.Missed); err != nil {
				panic(err)
			}
		}
	}

	if err := keeper.SetParams(ctx, data.Params); err != nil {
		panic(err)
	}
}

// ExportGenesis writes the current store values
// to a genesis file, which can be imported again
// with InitGenesis
func (keeper Keeper) ExportGenesis(ctx sdk.Context) (data *types.GenesisState) {
	params, err := keeper.GetParams(ctx)
	if err != nil {
		panic(err)
	}
	signingInfos := make([]types.SigningInfo, 0)
	missedBlocks := make([]types.ValidatorMissedBlocks, 0)
	err = keeper.IterateValidatorSigningInfos(ctx, func(address sdk.ConsAddress, info types.ValidatorSigningInfo) (stop bool) {
		bechAddr := address.String()
		signingInfos = append(signingInfos, types.SigningInfo{
			Address:              bechAddr,
			ValidatorSigningInfo: info,
		})

		localMissedBlocks, err := keeper.GetValidatorMissedBlocks(ctx, address)
		if err != nil {
			panic(err)
		}

		missedBlocks = append(missedBlocks, types.ValidatorMissedBlocks{
			Address:      bechAddr,
			MissedBlocks: localMissedBlocks,
		})

		return false
	})
	if err != nil {
		panic(err)
	}

	return types.NewGenesisState(params, signingInfos, missedBlocks)
}
