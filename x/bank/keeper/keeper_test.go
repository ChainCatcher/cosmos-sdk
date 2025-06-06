package keeper_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	cmtproto "github.com/cometbft/cometbft/api/cometbft/types/v2"
	abci "github.com/cometbft/cometbft/v2/abci/types"
	cmttime "github.com/cometbft/cometbft/v2/types/time"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/bank/exported"
	"github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktestutil "github.com/cosmos/cosmos-sdk/x/bank/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
)

const (
	fooDenom            = "foo"
	barDenom            = "bar"
	ibcPath             = "transfer/channel-0"
	ibcBaseDenom        = "farboo"
	metaDataDescription = "IBC Token from %s"
	initialPower        = int64(100)
	holder              = "holder"
	multiPerm           = "multiple permissions account"
	randomPerm          = "random permission"
)

var (
	holderAcc    = authtypes.NewEmptyModuleAccount(holder)
	burnerAcc    = authtypes.NewEmptyModuleAccount(authtypes.Burner, authtypes.Burner, authtypes.Staking)
	minterAcc    = authtypes.NewEmptyModuleAccount(authtypes.Minter, authtypes.Minter)
	mintAcc      = authtypes.NewEmptyModuleAccount(minttypes.ModuleName, authtypes.Minter)
	multiPermAcc = authtypes.NewEmptyModuleAccount(multiPerm, authtypes.Burner, authtypes.Minter, authtypes.Staking)

	baseAcc = authtypes.NewBaseAccountWithAddress(sdk.AccAddress([]byte("baseAcc")))

	accAddrs = []sdk.AccAddress{
		sdk.AccAddress([]byte("addr1_______________")),
		sdk.AccAddress([]byte("addr2_______________")),
		sdk.AccAddress([]byte("addr3_______________")),
		sdk.AccAddress([]byte("addr4_______________")),
		sdk.AccAddress([]byte("addr5_______________")),
	}

	// The default power validators are initialized to have within tests
	initTokens = sdk.TokensFromConsensusPower(initialPower, sdk.DefaultPowerReduction)
	initCoins  = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, initTokens))
)

func newFooCoin(amt int64) sdk.Coin {
	return sdk.NewInt64Coin(fooDenom, amt)
}

func newBarCoin(amt int64) sdk.Coin {
	return sdk.NewInt64Coin(barDenom, amt)
}

func newIbcCoin(amt int64) sdk.Coin {
	return sdk.NewInt64Coin(getIBCDenom(ibcPath, ibcBaseDenom), amt)
}

func getIBCDenom(path, baseDenom string) string {
	return fmt.Sprintf("%s/%s", "ibc", hex.EncodeToString(getIBCHash(path, baseDenom)))
}

func getIBCHash(path, baseDenom string) []byte {
	hash := sha256.Sum256([]byte(path + "/" + baseDenom))
	return hash[:]
}

func addIBCMetadata(ctx context.Context, k keeper.BaseKeeper) {
	metadata := banktypes.Metadata{
		Description: fmt.Sprintf(metaDataDescription, ibcPath),
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    ibcBaseDenom,
				Exponent: 0,
			},
		},
		// Setting base as IBChash Denom as SetDenomMetaData uses Base as storeKey
		// and the bank keeper will only have the IBCHash to get the denom metadata
		Base:    getIBCDenom(ibcPath, ibcBaseDenom),
		Display: ibcPath + "/" + ibcBaseDenom,
	}
	k.SetDenomMetaData(ctx, metadata)
}

type KeeperTestSuite struct {
	suite.Suite

	ctx        context.Context
	bankKeeper keeper.BaseKeeper
	authKeeper *banktestutil.MockAccountKeeper

	queryClient banktypes.QueryClient
	msgServer   banktypes.MsgServer

	encCfg moduletestutil.TestEncodingConfig
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) SetupTest() {
	key := storetypes.NewKVStoreKey(banktypes.StoreKey)
	testCtx := testutil.DefaultContextWithDB(suite.T(), key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx.WithBlockHeader(cmtproto.Header{Time: cmttime.Now()})
	encCfg := moduletestutil.MakeTestEncodingConfig()

	storeService := runtime.NewKVStoreService(key)

	// gomock initializations
	ctrl := gomock.NewController(suite.T())
	authKeeper := banktestutil.NewMockAccountKeeper(ctrl)
	authKeeper.EXPECT().AddressCodec().Return(address.NewBech32Codec("cosmos")).AnyTimes()
	suite.ctx = ctx
	suite.authKeeper = authKeeper
	suite.bankKeeper = keeper.NewBaseKeeper(
		encCfg.Codec,
		storeService,
		suite.authKeeper,
		map[string]bool{accAddrs[4].String(): true},
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		log.NewNopLogger(),
	)

	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	queryHelper := baseapp.NewQueryServerTestHelper(ctx, encCfg.InterfaceRegistry)
	banktypes.RegisterQueryServer(queryHelper, suite.bankKeeper)
	queryClient := banktypes.NewQueryClient(queryHelper)

	suite.queryClient = queryClient
	suite.msgServer = keeper.NewMsgServerImpl(suite.bankKeeper)
	suite.encCfg = encCfg
}

func (suite *KeeperTestSuite) mockQueryClient(ctx sdk.Context) banktypes.QueryClient {
	queryHelper := baseapp.NewQueryServerTestHelper(ctx, suite.encCfg.InterfaceRegistry)
	banktypes.RegisterQueryServer(queryHelper, suite.bankKeeper)
	return banktypes.NewQueryClient(queryHelper)
}

func (suite *KeeperTestSuite) mockMintCoins(moduleAcc *authtypes.ModuleAccount) {
	suite.authKeeper.EXPECT().GetModuleAccount(suite.ctx, moduleAcc.Name).Return(moduleAcc)
}

func (suite *KeeperTestSuite) mockSendCoinsFromModuleToAccount(moduleAcc *authtypes.ModuleAccount, accAddr sdk.AccAddress) {
	suite.authKeeper.EXPECT().GetModuleAddress(moduleAcc.Name).Return(moduleAcc.GetAddress())
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, moduleAcc.GetAddress()).Return(moduleAcc)
	suite.authKeeper.EXPECT().HasAccount(suite.ctx, accAddr).Return(true)
}

func (suite *KeeperTestSuite) mockBurnCoins(moduleAcc *authtypes.ModuleAccount) {
	suite.authKeeper.EXPECT().GetModuleAccount(suite.ctx, moduleAcc.Name).Return(moduleAcc)
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, moduleAcc.GetAddress()).Return(moduleAcc)
}

func (suite *KeeperTestSuite) mockSendCoinsFromModuleToModule(sender, receiver *authtypes.ModuleAccount) {
	suite.authKeeper.EXPECT().GetModuleAddress(sender.Name).Return(sender.GetAddress())
	suite.authKeeper.EXPECT().GetModuleAccount(suite.ctx, receiver.Name).Return(receiver)
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, sender.GetAddress()).Return(sender)
	suite.authKeeper.EXPECT().HasAccount(suite.ctx, receiver.GetAddress()).Return(true)
}

func (suite *KeeperTestSuite) mockSendCoinsFromAccountToModule(acc *authtypes.BaseAccount, moduleAcc *authtypes.ModuleAccount) {
	suite.authKeeper.EXPECT().GetModuleAccount(suite.ctx, moduleAcc.Name).Return(moduleAcc)
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, acc.GetAddress()).Return(acc)
	suite.authKeeper.EXPECT().HasAccount(suite.ctx, moduleAcc.GetAddress()).Return(true)
}

func (suite *KeeperTestSuite) mockSendCoins(ctx context.Context, sender sdk.AccountI, receiver sdk.AccAddress) {
	suite.authKeeper.EXPECT().GetAccount(ctx, sender.GetAddress()).Return(sender)
	suite.authKeeper.EXPECT().HasAccount(ctx, receiver).Return(true)
}

func (suite *KeeperTestSuite) mockFundAccount(receiver sdk.AccAddress) {
	suite.mockMintCoins(mintAcc)
	suite.mockSendCoinsFromModuleToAccount(mintAcc, receiver)
}

func (suite *KeeperTestSuite) mockInputOutputCoins(inputs []sdk.AccountI, outputs []sdk.AccAddress) {
	for _, input := range inputs {
		suite.authKeeper.EXPECT().GetAccount(suite.ctx, input.GetAddress()).Return(input)
	}
	for _, output := range outputs {
		suite.authKeeper.EXPECT().HasAccount(suite.ctx, output).Return(true)
	}
}

func (suite *KeeperTestSuite) mockValidateBalance(acc sdk.AccountI) {
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, acc.GetAddress()).Return(acc)
}

func (suite *KeeperTestSuite) mockSpendableCoins(ctx sdk.Context, acc sdk.AccountI) {
	suite.authKeeper.EXPECT().GetAccount(ctx, acc.GetAddress()).Return(acc)
}

func (suite *KeeperTestSuite) mockDelegateCoinsFromAccountToModule(acc *authtypes.BaseAccount, moduleAcc *authtypes.ModuleAccount) {
	suite.authKeeper.EXPECT().GetModuleAccount(suite.ctx, moduleAcc.Name).Return(moduleAcc)
	suite.mockDelegateCoins(suite.ctx, acc, moduleAcc)
}

func (suite *KeeperTestSuite) mockUndelegateCoinsFromModuleToAccount(moduleAcc *authtypes.ModuleAccount, accAddr *authtypes.BaseAccount) {
	suite.authKeeper.EXPECT().GetModuleAccount(suite.ctx, moduleAcc.Name).Return(moduleAcc)
	suite.mockUnDelegateCoins(suite.ctx, accAddr, moduleAcc)
}

func (suite *KeeperTestSuite) mockDelegateCoins(ctx context.Context, acc, mAcc sdk.AccountI) {
	vacc, ok := acc.(banktypes.VestingAccount)
	if ok {
		suite.authKeeper.EXPECT().SetAccount(ctx, vacc)
	}
	suite.authKeeper.EXPECT().GetAccount(ctx, acc.GetAddress()).Return(acc)
	suite.authKeeper.EXPECT().GetAccount(ctx, mAcc.GetAddress()).Return(mAcc)
}

func (suite *KeeperTestSuite) mockUnDelegateCoins(ctx context.Context, acc, mAcc sdk.AccountI) {
	vacc, ok := acc.(banktypes.VestingAccount)
	if ok {
		suite.authKeeper.EXPECT().SetAccount(ctx, vacc)
	}
	suite.authKeeper.EXPECT().GetAccount(ctx, acc.GetAddress()).Return(acc)
	suite.authKeeper.EXPECT().GetAccount(ctx, mAcc.GetAddress()).Return(mAcc)
	suite.authKeeper.EXPECT().GetAccount(ctx, mAcc.GetAddress()).Return(mAcc)
}

func (suite *KeeperTestSuite) TestAppendSendRestriction() {
	var calls []int
	testRestriction := func(index int) banktypes.SendRestrictionFn {
		return func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) (sdk.AccAddress, error) {
			calls = append(calls, index)
			return nil, nil
		}
	}

	bk := suite.bankKeeper

	// Initial append of the test restriction.
	bk.SetSendRestriction(nil)
	bk.AppendSendRestriction(testRestriction(1))
	_, _ = bk.GetSendRestrictionFn()(suite.ctx, nil, nil, nil)
	suite.Require().Equal([]int{1}, calls, "restriction calls after first append")

	// Append the test restriction again.
	calls = nil
	bk.AppendSendRestriction(testRestriction(2))
	_, _ = bk.GetSendRestrictionFn()(suite.ctx, nil, nil, nil)
	suite.Require().Equal([]int{1, 2}, calls, "restriction calls after second append")

	// make sure the original bank keeper has the restrictions too.
	calls = nil
	_, _ = suite.bankKeeper.GetSendRestrictionFn()(suite.ctx, nil, nil, nil)
	suite.Require().Equal([]int{1, 2}, calls, "restriction calls from original bank keeper")
}

func (suite *KeeperTestSuite) TestPrependSendRestriction() {
	var calls []int
	testRestriction := func(index int) banktypes.SendRestrictionFn {
		return func(_ context.Context, _, _ sdk.AccAddress, _ sdk.Coins) (sdk.AccAddress, error) {
			calls = append(calls, index)
			return nil, nil
		}
	}

	bk := suite.bankKeeper

	// Initial append of the test restriction.
	bk.SetSendRestriction(nil)
	bk.PrependSendRestriction(testRestriction(1))
	_, _ = bk.GetSendRestrictionFn()(suite.ctx, nil, nil, nil)
	suite.Require().Equal([]int{1}, calls, "restriction calls after first append")

	// Append the test restriction again.
	calls = nil
	bk.PrependSendRestriction(testRestriction(2))
	_, _ = bk.GetSendRestrictionFn()(suite.ctx, nil, nil, nil)
	suite.Require().Equal([]int{2, 1}, calls, "restriction calls after second append")

	// make sure the original bank keeper has the restrictions too.
	calls = nil
	_, _ = suite.bankKeeper.GetSendRestrictionFn()(suite.ctx, nil, nil, nil)
	suite.Require().Equal([]int{2, 1}, calls, "restriction calls from original bank keeper")
}

func (suite *KeeperTestSuite) TestGetAuthority() {
	storeService := runtime.NewKVStoreService(storetypes.NewKVStoreKey(banktypes.StoreKey))
	NewKeeperWithAuthority := func(authority string) keeper.BaseKeeper {
		return keeper.NewBaseKeeper(
			moduletestutil.MakeTestEncodingConfig().Codec,
			storeService,
			suite.authKeeper,
			nil,
			authority,
			log.NewNopLogger(),
		)
	}

	tests := map[string]string{
		"some random account":    "cosmos139f7kncmglres2nf3h4hc4tade85ekfr8sulz5",
		"gov module account":     authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		"another module account": authtypes.NewModuleAddress(minttypes.ModuleName).String(),
	}

	for name, expected := range tests {
		suite.T().Run(name, func(t *testing.T) {
			kpr := NewKeeperWithAuthority(expected)
			actual := kpr.GetAuthority()
			suite.Require().Equal(expected, actual)
		})
	}
}

func (suite *KeeperTestSuite) TestSupply() {
	ctx := suite.ctx
	require := suite.Require()
	keeper := suite.bankKeeper

	// add module accounts to supply keeper
	genesisSupply, _, err := keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)

	initialPower := int64(100)
	initTokens := sdk.TokensFromConsensusPower(initialPower, sdk.DefaultPowerReduction)
	initCoins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, initTokens))

	// set burnerAcc balance
	suite.mockMintCoins(minterAcc)
	require.NoError(keeper.MintCoins(ctx, authtypes.Minter, initCoins))

	suite.mockSendCoinsFromModuleToAccount(minterAcc, burnerAcc.GetAddress())
	require.NoError(keeper.SendCoinsFromModuleToAccount(ctx, authtypes.Minter, burnerAcc.GetAddress(), initCoins))

	total, _, err := keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)

	expTotalSupply := initCoins.Add(genesisSupply...)
	require.Equal(expTotalSupply, total)

	// burning all supplied tokens
	suite.mockBurnCoins(burnerAcc)
	require.NoError(keeper.BurnCoins(ctx, authtypes.Burner, initCoins))

	total, _, err = keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)
	require.Equal(total, genesisSupply)
}

func (suite *KeeperTestSuite) TestSendCoinsFromModuleToAccount_Blocklist() {
	ctx := suite.ctx
	require := suite.Require()
	keeper := suite.bankKeeper

	suite.mockMintCoins(mintAcc)
	require.NoError(keeper.MintCoins(ctx, minttypes.ModuleName, initCoins))

	suite.authKeeper.EXPECT().GetModuleAddress(mintAcc.Name).Return(mintAcc.GetAddress())
	require.Error(keeper.SendCoinsFromModuleToAccount(
		ctx, minttypes.ModuleName, accAddrs[4], initCoins,
	))
}

func (suite *KeeperTestSuite) TestSupply_DelegateUndelegateCoins() {
	ctx := suite.ctx
	require := suite.Require()
	authKeeper, keeper := suite.authKeeper, suite.bankKeeper

	// set initial balances
	suite.mockMintCoins(mintAcc)
	require.NoError(keeper.MintCoins(ctx, minttypes.ModuleName, initCoins))

	suite.mockSendCoinsFromModuleToAccount(mintAcc, holderAcc.GetAddress())
	require.NoError(keeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, holderAcc.GetAddress(), initCoins))

	authKeeper.EXPECT().GetModuleAddress("").Return(nil)
	require.Panics(func() {
		_ = keeper.SendCoinsFromModuleToAccount(ctx, "", holderAcc.GetAddress(), initCoins)
	})

	authKeeper.EXPECT().GetModuleAddress(burnerAcc.Name).Return(burnerAcc.GetAddress())
	authKeeper.EXPECT().GetModuleAccount(ctx, "").Return(nil)
	require.Panics(func() {
		_ = keeper.SendCoinsFromModuleToModule(ctx, authtypes.Burner, "", initCoins)
	})

	authKeeper.EXPECT().GetModuleAddress("").Return(nil)
	require.Panics(func() {
		_ = keeper.SendCoinsFromModuleToAccount(ctx, "", baseAcc.GetAddress(), initCoins)
	})

	authKeeper.EXPECT().GetModuleAddress(holderAcc.Name).Return(holderAcc.GetAddress())
	authKeeper.EXPECT().GetAccount(suite.ctx, holderAcc.GetAddress()).Return(holderAcc)
	require.Error(
		keeper.SendCoinsFromModuleToAccount(ctx, holderAcc.GetName(), baseAcc.GetAddress(), initCoins.Add(initCoins...)),
	)
	suite.mockSendCoinsFromModuleToModule(holderAcc, burnerAcc)
	require.NoError(
		keeper.SendCoinsFromModuleToModule(ctx, holderAcc.GetName(), authtypes.Burner, initCoins),
	)

	require.Equal(sdk.NewCoins(), keeper.GetAllBalances(ctx, holderAcc.GetAddress()))
	require.Equal(initCoins, keeper.GetAllBalances(ctx, burnerAcc.GetAddress()))

	suite.mockSendCoinsFromModuleToAccount(burnerAcc, baseAcc.GetAddress())
	require.NoError(
		keeper.SendCoinsFromModuleToAccount(ctx, authtypes.Burner, baseAcc.GetAddress(), initCoins),
	)
	require.Equal(sdk.NewCoins(), keeper.GetAllBalances(ctx, burnerAcc.GetAddress()))
	require.Equal(initCoins, keeper.GetAllBalances(ctx, baseAcc.GetAddress()))

	suite.mockDelegateCoinsFromAccountToModule(baseAcc, burnerAcc)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	require.NoError(keeper.DelegateCoinsFromAccountToModule(sdkCtx, baseAcc.GetAddress(), authtypes.Burner, initCoins))
	require.Equal(sdk.NewCoins(), keeper.GetAllBalances(ctx, baseAcc.GetAddress()))
	require.Equal(initCoins, keeper.GetAllBalances(ctx, burnerAcc.GetAddress()))

	suite.mockUndelegateCoinsFromModuleToAccount(burnerAcc, baseAcc)
	require.NoError(keeper.UndelegateCoinsFromModuleToAccount(sdkCtx, authtypes.Burner, baseAcc.GetAddress(), initCoins))
	require.Equal(sdk.NewCoins(), keeper.GetAllBalances(ctx, burnerAcc.GetAddress()))
	require.Equal(initCoins, keeper.GetAllBalances(ctx, baseAcc.GetAddress()))
}

func (suite *KeeperTestSuite) TestSupply_SendCoins() {
	ctx := suite.ctx
	require := suite.Require()
	authKeeper, keeper := suite.authKeeper, suite.bankKeeper

	// set initial balances
	suite.mockMintCoins(mintAcc)
	require.NoError(keeper.MintCoins(ctx, minttypes.ModuleName, initCoins))

	suite.mockSendCoinsFromModuleToAccount(mintAcc, holderAcc.GetAddress())
	require.NoError(keeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, holderAcc.GetAddress(), initCoins))

	authKeeper.EXPECT().GetModuleAddress("").Return(nil)
	require.Panics(func() {
		_ = keeper.SendCoinsFromModuleToModule(ctx, "", holderAcc.GetName(), initCoins)
	})

	authKeeper.EXPECT().GetModuleAddress(burnerAcc.Name).Return(burnerAcc.GetAddress())
	authKeeper.EXPECT().GetModuleAccount(ctx, "").Return(nil)
	require.Panics(func() {
		_ = keeper.SendCoinsFromModuleToModule(ctx, authtypes.Burner, "", initCoins)
	})

	authKeeper.EXPECT().GetModuleAddress("").Return(nil)
	require.Panics(func() {
		_ = keeper.SendCoinsFromModuleToAccount(ctx, "", baseAcc.GetAddress(), initCoins)
	})

	authKeeper.EXPECT().GetModuleAddress(holderAcc.Name).Return(holderAcc.GetAddress())
	authKeeper.EXPECT().GetAccount(suite.ctx, holderAcc.GetAddress()).Return(holderAcc)
	require.Error(
		keeper.SendCoinsFromModuleToAccount(ctx, holderAcc.GetName(), baseAcc.GetAddress(), initCoins.Add(initCoins...)),
	)

	suite.mockSendCoinsFromModuleToModule(holderAcc, burnerAcc)
	require.NoError(
		keeper.SendCoinsFromModuleToModule(ctx, holderAcc.GetName(), authtypes.Burner, initCoins),
	)

	require.Equal(sdk.NewCoins(), keeper.GetAllBalances(ctx, holderAcc.GetAddress()))
	require.Equal(initCoins, keeper.GetAllBalances(ctx, burnerAcc.GetAddress()))

	suite.mockSendCoinsFromModuleToAccount(burnerAcc, baseAcc.GetAddress())
	require.NoError(
		keeper.SendCoinsFromModuleToAccount(ctx, authtypes.Burner, baseAcc.GetAddress(), initCoins),
	)
	require.Equal(sdk.NewCoins(), keeper.GetAllBalances(ctx, burnerAcc.GetAddress()))
	require.Equal(initCoins, keeper.GetAllBalances(ctx, baseAcc.GetAddress()))

	suite.mockSendCoinsFromAccountToModule(baseAcc, burnerAcc)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	require.NoError(keeper.SendCoinsFromAccountToModule(sdkCtx, baseAcc.GetAddress(), authtypes.Burner, initCoins))
	require.Equal(sdk.NewCoins(), keeper.GetAllBalances(ctx, baseAcc.GetAddress()))
	require.Equal(initCoins, keeper.GetAllBalances(ctx, burnerAcc.GetAddress()))
}

func (suite *KeeperTestSuite) TestSupply_MintCoins() {
	ctx := suite.ctx
	require := suite.Require()
	authKeeper, keeper := suite.authKeeper, suite.bankKeeper

	initialSupply, _, err := keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)

	authKeeper.EXPECT().GetModuleAccount(ctx, "").Return(nil)
	require.Panics(func() { _ = keeper.MintCoins(ctx, "", initCoins) }, "no module account")

	suite.mockMintCoins(burnerAcc)
	require.Panics(func() { _ = keeper.MintCoins(ctx, authtypes.Burner, initCoins) }, "invalid permission")

	suite.mockMintCoins(minterAcc)
	require.Error(keeper.MintCoins(ctx, authtypes.Minter, sdk.Coins{sdk.Coin{Denom: "denom", Amount: math.NewInt(-10)}}), "insufficient coins")

	authKeeper.EXPECT().GetModuleAccount(ctx, randomPerm).Return(nil)
	require.Panics(func() { _ = keeper.MintCoins(ctx, randomPerm, initCoins) })

	suite.mockMintCoins(minterAcc)
	require.NoError(keeper.MintCoins(ctx, authtypes.Minter, initCoins))

	require.Equal(initCoins, keeper.GetAllBalances(ctx, minterAcc.GetAddress()))
	totalSupply, _, err := keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)

	require.Equal(initialSupply.Add(initCoins...), totalSupply)

	// test same functionality on module account with multiple permissions
	initialSupply, _, err = keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)

	suite.mockMintCoins(multiPermAcc)
	require.NoError(keeper.MintCoins(ctx, multiPermAcc.GetName(), initCoins))

	totalSupply, _, err = keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)
	require.Equal(initCoins, keeper.GetAllBalances(ctx, multiPermAcc.GetAddress()))
	require.Equal(initialSupply.Add(initCoins...), totalSupply)
}

func (suite *KeeperTestSuite) TestSupply_BurnCoins() {
	ctx := suite.ctx
	require := suite.Require()
	authKeeper, keeper := suite.authKeeper, suite.bankKeeper

	// set burnerAcc balance
	suite.mockMintCoins(minterAcc)
	require.NoError(keeper.MintCoins(ctx, authtypes.Minter, initCoins))

	suite.mockSendCoinsFromModuleToAccount(minterAcc, burnerAcc.GetAddress())
	require.NoError(keeper.SendCoinsFromModuleToAccount(ctx, authtypes.Minter, burnerAcc.GetAddress(), initCoins))

	// inflate supply
	suite.mockMintCoins(minterAcc)
	require.NoError(keeper.MintCoins(ctx, authtypes.Minter, initCoins))

	supplyAfterInflation, _, err := keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)

	authKeeper.EXPECT().GetModuleAccount(ctx, "").Return(nil)
	require.Panics(func() { _ = keeper.BurnCoins(ctx, "", initCoins) }, "no module account")

	authKeeper.EXPECT().GetModuleAccount(ctx, minterAcc.Name).Return(nil)
	require.Panics(func() { _ = keeper.BurnCoins(ctx, authtypes.Minter, initCoins) }, "invalid permission")

	authKeeper.EXPECT().GetModuleAccount(ctx, randomPerm).Return(nil)
	require.Panics(func() { _ = keeper.BurnCoins(ctx, randomPerm, supplyAfterInflation) }, "random permission")

	suite.mockBurnCoins(burnerAcc)
	require.Error(keeper.BurnCoins(ctx, authtypes.Burner, supplyAfterInflation), "insufficient coins")

	suite.mockBurnCoins(burnerAcc)
	require.NoError(keeper.BurnCoins(ctx, authtypes.Burner, initCoins))

	supplyAfterBurn, _, err := keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)
	require.Equal(sdk.NewCoins(), keeper.GetAllBalances(ctx, burnerAcc.GetAddress()))
	require.Equal(supplyAfterInflation.Sub(initCoins...), supplyAfterBurn)

	// test same functionality on module account with multiple permissions
	suite.mockMintCoins(minterAcc)
	require.NoError(keeper.MintCoins(ctx, authtypes.Minter, initCoins))

	supplyAfterInflation, _, err = keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)

	suite.mockSendCoins(ctx, minterAcc, multiPermAcc.GetAddress())
	require.NoError(keeper.SendCoins(ctx, minterAcc.GetAddress(), multiPermAcc.GetAddress(), initCoins))

	suite.mockBurnCoins(multiPermAcc)
	require.NoError(keeper.BurnCoins(ctx, multiPermAcc.GetName(), initCoins))

	supplyAfterBurn, _, err = keeper.GetPaginatedTotalSupply(ctx, &query.PageRequest{})
	require.NoError(err)
	require.Equal(sdk.NewCoins(), keeper.GetAllBalances(ctx, multiPermAcc.GetAddress()))
	require.Equal(supplyAfterInflation.Sub(initCoins...), supplyAfterBurn)
}

func (suite *KeeperTestSuite) TestSendCoinsNewAccount() {
	ctx := suite.ctx
	require := suite.Require()
	balances := sdk.NewCoins(newFooCoin(100), newBarCoin(50))

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], balances))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	acc1Balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[0])
	require.Equal(balances, acc1Balances)

	suite.bankKeeper.GetAllBalances(ctx, accAddrs[1])
	require.Empty(suite.bankKeeper.GetAllBalances(ctx, accAddrs[1]))

	sendAmt := sdk.NewCoins(newFooCoin(50), newBarCoin(50))
	suite.mockSendCoins(ctx, acc0, accAddrs[1])
	require.NoError(suite.bankKeeper.SendCoins(ctx, accAddrs[0], accAddrs[1], sendAmt))

	acc2Balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[1])
	acc1Balances = suite.bankKeeper.GetAllBalances(ctx, accAddrs[0])
	require.Equal(sendAmt, acc2Balances)
	updatedAcc1Bal := balances.Sub(sendAmt...)
	require.Len(acc1Balances, len(updatedAcc1Bal))
	require.Equal(acc1Balances, updatedAcc1Bal)
}

func (suite *KeeperTestSuite) TestInputOutputNewAccount() {
	ctx := suite.ctx
	require := suite.Require()

	balances := sdk.NewCoins(newFooCoin(100), newBarCoin(50))

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], balances))

	acc1Balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[0])
	require.Equal(balances, acc1Balances)

	require.Empty(suite.bankKeeper.GetAllBalances(ctx, accAddrs[1]))

	suite.mockInputOutputCoins([]sdk.AccountI{authtypes.NewBaseAccountWithAddress(accAddrs[0])}, []sdk.AccAddress{accAddrs[1]})
	input := banktypes.Input{
		Address: accAddrs[0].String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10)),
	}
	outputs := []banktypes.Output{
		{Address: accAddrs[1].String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10))},
	}

	require.NoError(suite.bankKeeper.InputOutputCoins(ctx, input, outputs))

	expected := sdk.NewCoins(newFooCoin(30), newBarCoin(10))
	acc2Balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[1])
	require.Equal(expected, acc2Balances)
}

func (suite *KeeperTestSuite) TestInputOutputCoins() {
	ctx := suite.ctx
	require := suite.Require()
	balances := sdk.NewCoins(newFooCoin(90), newBarCoin(30))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	input := banktypes.Input{
		Address: accAddrs[0].String(), Coins: sdk.NewCoins(newFooCoin(60), newBarCoin(20)),
	}
	outputs := []banktypes.Output{
		{Address: accAddrs[1].String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10))},
		{Address: accAddrs[2].String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10))},
	}

	// test that inputs with no outputs fails
	require.ErrorContains(suite.bankKeeper.InputOutputCoins(ctx, input, []banktypes.Output{}), banktypes.ErrInputOutputMismatch.Error())

	// accounts has no funds, should error.
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, accAddrs[0]).Return(acc0)
	suite.authKeeper.EXPECT().HasAccount(suite.ctx, gomock.Any()).Return(true).Times(len(outputs))
	err := suite.bankKeeper.InputOutputCoins(ctx, input, outputs)
	require.ErrorContains(err, "insufficient funds")

	// fund account now.
	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], balances))

	insufficientInput := banktypes.Input{
		Address: accAddrs[0].String(),
		Coins:   sdk.NewCoins(newFooCoin(300), newBarCoin(100)),
	}
	insufficientOutputs := []banktypes.Output{
		{Address: accAddrs[1].String(), Coins: sdk.NewCoins(newFooCoin(300), newBarCoin(100))},
		{Address: accAddrs[2].String(), Coins: sdk.NewCoins(newFooCoin(300), newBarCoin(100))},
	}

	// input: 300foo,100bar ==> output: 600foo,200bar. should fail
	err = suite.bankKeeper.InputOutputCoins(ctx, insufficientInput, insufficientOutputs)
	require.ErrorContains(err, banktypes.ErrInputOutputMismatch.Error())

	// should work with valid input/outputs.
	suite.mockInputOutputCoins([]sdk.AccountI{acc0}, accAddrs[1:3])
	require.NoError(suite.bankKeeper.InputOutputCoins(ctx, input, outputs))

	acc1Balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[0])
	expected := sdk.NewCoins(newFooCoin(30), newBarCoin(10))
	require.Equal(expected, acc1Balances)
	acc2Balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[1])
	require.Equal(expected, acc2Balances)

	acc3Balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[2])
	require.Equal(expected, acc3Balances)
}

func (suite *KeeperTestSuite) TestInputOutputCoins_AccountCreated() {
	ctx := suite.ctx
	require := suite.Require()
	balances := sdk.NewCoins(newFooCoin(90), newBarCoin(30))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	input := banktypes.Input{
		Address: accAddrs[0].String(), Coins: sdk.NewCoins(newFooCoin(60), newBarCoin(20)),
	}
	outputs := []banktypes.Output{
		{Address: accAddrs[1].String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10))},
		{Address: accAddrs[2].String(), Coins: sdk.NewCoins(newFooCoin(30), newBarCoin(10))},
	}

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], balances))

	// the input account should be retrieved.
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, accAddrs[0]).Return(acc0)

	// creates output account 1
	suite.authKeeper.EXPECT().HasAccount(suite.ctx, accAddrs[1].Bytes()).Return(false)
	suite.authKeeper.EXPECT().NewAccountWithAddress(suite.ctx, accAddrs[1].Bytes())
	suite.authKeeper.EXPECT().SetAccount(suite.ctx, gomock.Any())

	// creates output account 2
	suite.authKeeper.EXPECT().HasAccount(suite.ctx, accAddrs[2].Bytes()).Return(false)
	suite.authKeeper.EXPECT().NewAccountWithAddress(suite.ctx, accAddrs[2].Bytes())
	suite.authKeeper.EXPECT().SetAccount(suite.ctx, gomock.Any())

	require.NoError(suite.bankKeeper.InputOutputCoins(ctx, input, outputs))
}

func (suite *KeeperTestSuite) TestInputOutputCoinsWithRestrictions() {
	type restrictionArgs struct {
		ctx      context.Context
		fromAddr sdk.AccAddress
		toAddr   sdk.AccAddress
		amt      sdk.Coins
	}
	var actualRestrictionArgs []*restrictionArgs
	restrictionError := func(messages ...string) banktypes.SendRestrictionFn {
		i := -1
		return func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
			actualRestrictionArgs = append(actualRestrictionArgs, &restrictionArgs{
				ctx:      ctx,
				fromAddr: fromAddr,
				toAddr:   toAddr,
				amt:      amt,
			})
			i++
			if i < len(messages) {
				if len(messages[i]) > 0 {
					return nil, errors.New(messages[i])
				}
			}
			return toAddr, nil
		}
	}
	restrictionPassthrough := func() banktypes.SendRestrictionFn {
		return func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
			actualRestrictionArgs = append(actualRestrictionArgs, &restrictionArgs{
				ctx:      ctx,
				fromAddr: fromAddr,
				toAddr:   toAddr,
				amt:      amt,
			})
			return toAddr, nil
		}
	}
	restrictionNewTo := func(newToAddrs ...sdk.AccAddress) banktypes.SendRestrictionFn {
		i := -1
		return func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
			actualRestrictionArgs = append(actualRestrictionArgs, &restrictionArgs{
				ctx:      ctx,
				fromAddr: fromAddr,
				toAddr:   toAddr,
				amt:      amt,
			})
			i++
			if i < len(newToAddrs) {
				if len(newToAddrs[i]) > 0 {
					return newToAddrs[i], nil
				}
			}
			return toAddr, nil
		}
	}
	type expBals struct {
		from sdk.Coins
		to1  sdk.Coins
		to2  sdk.Coins
	}

	setupCtx := suite.ctx
	balances := sdk.NewCoins(newFooCoin(1000), newBarCoin(500))
	fromAddr := accAddrs[0]
	fromAcc := authtypes.NewBaseAccountWithAddress(fromAddr)
	inputAccs := []sdk.AccountI{fromAcc}
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, inputAccs[0].GetAddress()).Return(inputAccs[0]).AnyTimes()
	toAddr1 := accAddrs[1]
	toAddr2 := accAddrs[2]

	suite.mockFundAccount(accAddrs[0])
	suite.Require().NoError(banktestutil.FundAccount(setupCtx, suite.bankKeeper, accAddrs[0], balances))

	tests := []struct {
		name        string
		fn          banktypes.SendRestrictionFn
		inputCoins  sdk.Coins
		outputs     []banktypes.Output
		outputAddrs []sdk.AccAddress
		expArgs     []*restrictionArgs
		expErr      string
		expBals     expBals
	}{
		{
			name:        "nil restriction",
			fn:          nil,
			inputCoins:  sdk.NewCoins(newFooCoin(5)),
			outputs:     []banktypes.Output{{Address: toAddr1.String(), Coins: sdk.NewCoins(newFooCoin(5))}},
			outputAddrs: []sdk.AccAddress{toAddr1},
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(995), newBarCoin(500)),
				to1:  sdk.NewCoins(newFooCoin(5)),
				to2:  sdk.Coins{},
			},
		},
		{
			name:        "passthrough restriction single output",
			fn:          restrictionPassthrough(),
			inputCoins:  sdk.NewCoins(newFooCoin(10)),
			outputs:     []banktypes.Output{{Address: toAddr1.String(), Coins: sdk.NewCoins(newFooCoin(10))}},
			outputAddrs: []sdk.AccAddress{toAddr1},
			expArgs: []*restrictionArgs{
				{
					ctx:      suite.ctx,
					fromAddr: fromAddr,
					toAddr:   toAddr1,
					amt:      sdk.NewCoins(newFooCoin(10)),
				},
			},
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(985), newBarCoin(500)),
				to1:  sdk.NewCoins(newFooCoin(15)),
				to2:  sdk.Coins{},
			},
		},
		{
			name:        "new to restriction single output",
			fn:          restrictionNewTo(toAddr2),
			inputCoins:  sdk.NewCoins(newFooCoin(26)),
			outputs:     []banktypes.Output{{Address: toAddr1.String(), Coins: sdk.NewCoins(newFooCoin(26))}},
			outputAddrs: []sdk.AccAddress{toAddr2},
			expArgs: []*restrictionArgs{
				{
					ctx:      suite.ctx,
					fromAddr: fromAddr,
					toAddr:   toAddr1,
					amt:      sdk.NewCoins(newFooCoin(26)),
				},
			},
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(959), newBarCoin(500)),
				to1:  sdk.NewCoins(newFooCoin(15)),
				to2:  sdk.NewCoins(newFooCoin(26)),
			},
		},
		{
			name:        "error restriction single output",
			fn:          restrictionError("restriction test error"),
			inputCoins:  sdk.NewCoins(newBarCoin(88)),
			outputs:     []banktypes.Output{{Address: toAddr1.String(), Coins: sdk.NewCoins(newBarCoin(88))}},
			outputAddrs: []sdk.AccAddress{},
			expArgs: []*restrictionArgs{
				{
					ctx:      suite.ctx,
					fromAddr: fromAddr,
					toAddr:   toAddr1,
					amt:      sdk.NewCoins(newBarCoin(88)),
				},
			},
			expErr: "restriction test error",
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(959), newBarCoin(500)),
				to1:  sdk.NewCoins(newFooCoin(15)),
				to2:  sdk.NewCoins(newFooCoin(26)),
			},
		},
		{
			name:       "passthrough restriction two outputs",
			fn:         restrictionPassthrough(),
			inputCoins: sdk.NewCoins(newFooCoin(11), newBarCoin(12)),
			outputs: []banktypes.Output{
				{Address: toAddr1.String(), Coins: sdk.NewCoins(newFooCoin(11))},
				{Address: toAddr2.String(), Coins: sdk.NewCoins(newBarCoin(12))},
			},
			outputAddrs: []sdk.AccAddress{toAddr1, toAddr2},
			expArgs: []*restrictionArgs{
				{
					ctx:      suite.ctx,
					fromAddr: fromAddr,
					toAddr:   toAddr1,
					amt:      sdk.NewCoins(newFooCoin(11)),
				},
				{
					ctx:      suite.ctx,
					fromAddr: fromAddr,
					toAddr:   toAddr2,
					amt:      sdk.NewCoins(newBarCoin(12)),
				},
			},
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(948), newBarCoin(488)),
				to1:  sdk.NewCoins(newFooCoin(26)),
				to2:  sdk.NewCoins(newFooCoin(26), newBarCoin(12)),
			},
		},
		{
			name:       "error restriction two outputs error on second",
			fn:         restrictionError("", "second restriction error"),
			inputCoins: sdk.NewCoins(newFooCoin(44)),
			outputs: []banktypes.Output{
				{Address: toAddr1.String(), Coins: sdk.NewCoins(newFooCoin(12))},
				{Address: toAddr2.String(), Coins: sdk.NewCoins(newFooCoin(32))},
			},
			outputAddrs: []sdk.AccAddress{toAddr1},
			expArgs: []*restrictionArgs{
				{
					ctx:      suite.ctx,
					fromAddr: fromAddr,
					toAddr:   toAddr1,
					amt:      sdk.NewCoins(newFooCoin(12)),
				},
				{
					ctx:      suite.ctx,
					fromAddr: fromAddr,
					toAddr:   toAddr2,
					amt:      sdk.NewCoins(newFooCoin(32)),
				},
			},
			expErr: "second restriction error",
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(948), newBarCoin(488)),
				to1:  sdk.NewCoins(newFooCoin(26)),
				to2:  sdk.NewCoins(newFooCoin(26), newBarCoin(12)),
			},
		},
		{
			name:       "new to restriction two outputs",
			fn:         restrictionNewTo(toAddr2, toAddr1),
			inputCoins: sdk.NewCoins(newBarCoin(35)),
			outputs: []banktypes.Output{
				{Address: toAddr1.String(), Coins: sdk.NewCoins(newBarCoin(10))},
				{Address: toAddr2.String(), Coins: sdk.NewCoins(newBarCoin(25))},
			},
			outputAddrs: []sdk.AccAddress{toAddr1, toAddr2},
			expArgs: []*restrictionArgs{
				{
					ctx:      suite.ctx,
					fromAddr: fromAddr,
					toAddr:   toAddr1,
					amt:      sdk.NewCoins(newBarCoin(10)),
				},
				{
					ctx:      suite.ctx,
					fromAddr: fromAddr,
					toAddr:   toAddr2,
					amt:      sdk.NewCoins(newBarCoin(25)),
				},
			},
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(948), newBarCoin(453)),
				to1:  sdk.NewCoins(newFooCoin(26), newBarCoin(25)),
				to2:  sdk.NewCoins(newFooCoin(26), newBarCoin(22)),
			},
		},
	}

	for _, tc := range tests {
		suite.Run(tc.name, func() {
			existingSendRestrictionFn := suite.bankKeeper.GetSendRestrictionFn()
			defer suite.bankKeeper.SetSendRestriction(existingSendRestrictionFn)
			actualRestrictionArgs = nil
			suite.bankKeeper.SetSendRestriction(tc.fn)
			ctx := suite.ctx
			input := banktypes.Input{
				Address: fromAddr.String(),
				Coins:   tc.inputCoins,
			}

			var err error
			testFunc := func() {
				err = suite.bankKeeper.InputOutputCoins(ctx, input, tc.outputs)
			}
			suite.authKeeper.EXPECT().HasAccount(gomock.Any(), gomock.Any()).Return(true).Times(len(tc.outputAddrs))
			suite.Require().NotPanics(testFunc, "InputOutputCoins")
			if len(tc.expErr) > 0 {
				suite.Assert().EqualError(err, tc.expErr, "InputOutputCoins error")
			} else {
				suite.Assert().NoError(err, "InputOutputCoins error")
			}
			if len(tc.expArgs) > 0 {
				for i, expArgs := range tc.expArgs {
					suite.Assert().Equal(expArgs.ctx, actualRestrictionArgs[i].ctx, "[%d] ctx provided to restriction", i)
					suite.Assert().Equal(expArgs.fromAddr, actualRestrictionArgs[i].fromAddr, "[%d] fromAddr provided to restriction", i)
					suite.Assert().Equal(expArgs.toAddr, actualRestrictionArgs[i].toAddr, "[%d] toAddr provided to restriction", i)
					suite.Assert().Equal(expArgs.amt.String(), actualRestrictionArgs[i].amt.String(), "[%d] amt provided to restriction", i)
				}
			} else {
				suite.Assert().Nil(actualRestrictionArgs, "args provided to a restriction")
			}
			fromBal := suite.bankKeeper.GetAllBalances(ctx, fromAddr)
			suite.Assert().Equal(tc.expBals.from.String(), fromBal.String(), "fromAddr balance")
			to1Bal := suite.bankKeeper.GetAllBalances(ctx, toAddr1)
			suite.Assert().Equal(tc.expBals.to1.String(), to1Bal.String(), "toAddr1 balance")
			to2Bal := suite.bankKeeper.GetAllBalances(ctx, toAddr2)
			suite.Assert().Equal(tc.expBals.to2.String(), to2Bal.String(), "toAddr2 balance")
		})
	}
}

func (suite *KeeperTestSuite) TestSendCoins() {
	ctx := suite.ctx
	require := suite.Require()
	balances := sdk.NewCoins(newFooCoin(100), newBarCoin(50))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])

	suite.mockFundAccount(accAddrs[1])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[1], balances))

	sendAmt := sdk.NewCoins(newFooCoin(50), newBarCoin(25))
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, accAddrs[0]).Return(acc0)
	require.Error(suite.bankKeeper.SendCoins(ctx, accAddrs[0], accAddrs[1], sendAmt))

	// invalid denom rejected
	invalidDenomAmounts := []sdk.Coin{newFooCoin(50), {Denom: "123fox", Amount: math.OneInt()}}
	gotErr := suite.bankKeeper.SendCoins(ctx, accAddrs[0], accAddrs[1], invalidDenomAmounts)
	require.ErrorIs(gotErr, sdkerrors.ErrInvalidCoins)

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], balances))
	suite.mockSendCoins(ctx, acc0, accAddrs[1])
	require.NoError(suite.bankKeeper.SendCoins(ctx, accAddrs[0], accAddrs[1], sendAmt))

	acc1Balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[0])
	expected := sdk.NewCoins(newFooCoin(50), newBarCoin(25))
	require.Equal(expected, acc1Balances)

	acc2Balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[1])
	expected = sdk.NewCoins(newFooCoin(150), newBarCoin(75))
	require.Equal(expected, acc2Balances)

	// we sent all foo coins to acc2, so foo balance should be deleted for acc1 and bar should be still there
	var coins []sdk.Coin
	suite.bankKeeper.IterateAccountBalances(ctx, accAddrs[0], func(c sdk.Coin) (stop bool) {
		coins = append(coins, c)
		return true
	})
	require.Len(coins, 1)
	require.Equal(newBarCoin(25), coins[0], "expected only bar coins in the account balance, got: %v", coins)
}

func (suite *KeeperTestSuite) TestSendCoinsWithRestrictions() {
	type restrictionArgs struct {
		ctx      context.Context
		fromAddr sdk.AccAddress
		toAddr   sdk.AccAddress
		amt      sdk.Coins
	}
	var actualRestrictionArgs *restrictionArgs
	restrictionError := func(message string) banktypes.SendRestrictionFn {
		return func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
			actualRestrictionArgs = &restrictionArgs{
				ctx:      ctx,
				fromAddr: fromAddr,
				toAddr:   toAddr,
				amt:      amt,
			}
			return nil, errors.New(message)
		}
	}
	restrictionPassthrough := func() banktypes.SendRestrictionFn {
		return func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
			actualRestrictionArgs = &restrictionArgs{
				ctx:      ctx,
				fromAddr: fromAddr,
				toAddr:   toAddr,
				amt:      amt,
			}
			return toAddr, nil
		}
	}
	restrictionNewTo := func(newToAddr sdk.AccAddress) banktypes.SendRestrictionFn {
		return func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
			actualRestrictionArgs = &restrictionArgs{
				ctx:      ctx,
				fromAddr: fromAddr,
				toAddr:   toAddr,
				amt:      amt,
			}
			return newToAddr, nil
		}
	}
	type expBals struct {
		from sdk.Coins
		to1  sdk.Coins
		to2  sdk.Coins
	}

	setupCtx := suite.ctx
	balances := sdk.NewCoins(newFooCoin(1000), newBarCoin(500))
	fromAddr := accAddrs[0]
	fromAcc := authtypes.NewBaseAccountWithAddress(fromAddr)
	toAddr1 := accAddrs[1]
	toAddr2 := accAddrs[2]

	suite.mockFundAccount(accAddrs[0])
	suite.Require().NoError(banktestutil.FundAccount(setupCtx, suite.bankKeeper, accAddrs[0], balances))

	tests := []struct {
		name      string
		fn        banktypes.SendRestrictionFn
		toAddr    sdk.AccAddress
		finalAddr sdk.AccAddress
		amt       sdk.Coins
		expArgs   *restrictionArgs
		expErr    string
		expBals   expBals
	}{
		{
			name:      "nil restriction",
			fn:        nil,
			toAddr:    toAddr1,
			finalAddr: toAddr1,
			amt:       sdk.NewCoins(newFooCoin(5)),
			expArgs:   nil,
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(995), newBarCoin(500)),
				to1:  sdk.NewCoins(newFooCoin(5)),
				to2:  sdk.Coins{},
			},
		},
		{
			name:      "passthrough restriction",
			fn:        restrictionPassthrough(),
			toAddr:    toAddr1,
			finalAddr: toAddr1,
			amt:       sdk.NewCoins(newFooCoin(10)),
			expArgs: &restrictionArgs{
				ctx:      suite.ctx,
				fromAddr: fromAddr,
				toAddr:   toAddr1,
				amt:      sdk.NewCoins(newFooCoin(10)),
			},
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(985), newBarCoin(500)),
				to1:  sdk.NewCoins(newFooCoin(15)),
				to2:  sdk.Coins{},
			},
		},
		{
			name:      "new to addr restriction",
			fn:        restrictionNewTo(toAddr2),
			toAddr:    toAddr1,
			finalAddr: toAddr2,
			amt:       sdk.NewCoins(newBarCoin(27)),
			expArgs: &restrictionArgs{
				ctx:      suite.ctx,
				fromAddr: fromAddr,
				toAddr:   toAddr1,
				amt:      sdk.NewCoins(newBarCoin(27)),
			},
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(985), newBarCoin(473)),
				to1:  sdk.NewCoins(newFooCoin(15)),
				to2:  sdk.NewCoins(newBarCoin(27)),
			},
		},
		{
			name:      "restriction returns error",
			fn:        restrictionError("test restriction error"),
			toAddr:    toAddr1,
			finalAddr: toAddr1,
			amt:       sdk.NewCoins(newFooCoin(100), newBarCoin(200)),
			expArgs: &restrictionArgs{
				ctx:      suite.ctx,
				fromAddr: fromAddr,
				toAddr:   toAddr1,
				amt:      sdk.NewCoins(newFooCoin(100), newBarCoin(200)),
			},
			expErr: "test restriction error",
			expBals: expBals{
				from: sdk.NewCoins(newFooCoin(985), newBarCoin(473)),
				to1:  sdk.NewCoins(newFooCoin(15)),
				to2:  sdk.NewCoins(newBarCoin(27)),
			},
		},
	}

	for _, tc := range tests {
		suite.Run(tc.name, func() {
			existingSendRestrictionFn := suite.bankKeeper.GetSendRestrictionFn()
			defer suite.bankKeeper.SetSendRestriction(existingSendRestrictionFn)
			actualRestrictionArgs = nil
			suite.bankKeeper.SetSendRestriction(tc.fn)
			ctx := suite.ctx
			if len(tc.expErr) == 0 {
				suite.mockSendCoins(ctx, fromAcc, tc.finalAddr)
			}
			var err error
			testFunc := func() {
				err = suite.bankKeeper.SendCoins(ctx, fromAddr, tc.toAddr, tc.amt)
			}
			suite.Require().NotPanics(testFunc, "SendCoins")
			if len(tc.expErr) > 0 {
				suite.Assert().EqualError(err, tc.expErr, "SendCoins error")
			} else {
				suite.Assert().NoError(err, "SendCoins error")
			}
			if tc.expArgs != nil {
				suite.Assert().Equal(tc.expArgs.ctx, actualRestrictionArgs.ctx, "ctx provided to restriction")
				suite.Assert().Equal(tc.expArgs.fromAddr, actualRestrictionArgs.fromAddr, "fromAddr provided to restriction")
				suite.Assert().Equal(tc.expArgs.toAddr, actualRestrictionArgs.toAddr, "toAddr provided to restriction")
				suite.Assert().Equal(tc.expArgs.amt.String(), actualRestrictionArgs.amt.String(), "amt provided to restriction")
			} else {
				suite.Assert().Nil(actualRestrictionArgs, "args provided to a restriction")
			}
			fromBal := suite.bankKeeper.GetAllBalances(ctx, fromAddr)
			suite.Assert().Equal(tc.expBals.from.String(), fromBal.String(), "fromAddr balance")
			to1Bal := suite.bankKeeper.GetAllBalances(ctx, toAddr1)
			suite.Assert().Equal(tc.expBals.to1.String(), to1Bal.String(), "toAddr1 balance")
			to2Bal := suite.bankKeeper.GetAllBalances(ctx, toAddr2)
			suite.Assert().Equal(tc.expBals.to2.String(), to2Bal.String(), "toAddr2 balance")
		})
	}
}

func (suite *KeeperTestSuite) TestSendCoins_Invalid_SendLockedCoins() {
	balances := sdk.NewCoins(newFooCoin(50))

	now := cmttime.Now()
	endTime := now.Add(24 * time.Hour)

	origCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 100))
	sendCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 50))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	vacc, err := vesting.NewContinuousVestingAccount(acc0, origCoins, now.Unix(), endTime.Unix())
	suite.Require().NoError(err)

	suite.mockFundAccount(accAddrs[1])
	suite.Require().NoError(banktestutil.FundAccount(suite.ctx, suite.bankKeeper, accAddrs[1], balances))

	suite.authKeeper.EXPECT().GetAccount(suite.ctx, accAddrs[0]).Return(vacc)
	suite.Require().Error(suite.bankKeeper.SendCoins(suite.ctx, accAddrs[0], accAddrs[1], sendCoins))
}

func (suite *KeeperTestSuite) TestSendCoins_Invalid_NoSpendableCoins() {
	coin := sdk.NewInt64Coin("stake", 10)
	coins := sdk.NewCoins(coin)
	balances := coins

	now := cmttime.Now()
	endTime := now.Add(24 * time.Hour)

	origCoins := coins
	sendCoins := coins

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	suite.mockFundAccount(accAddrs[0])
	suite.Require().NoError(banktestutil.FundAccount(suite.ctx, suite.bankKeeper, accAddrs[0], balances))
	vacc, err := vesting.NewContinuousVestingAccount(acc0, origCoins, now.Unix(), endTime.Unix())
	suite.Require().NoError(err)

	suite.authKeeper.EXPECT().GetAccount(suite.ctx, accAddrs[0]).Return(vacc)
	e := errorsmod.Wrapf(
		sdkerrors.ErrInsufficientFunds,
		"spendable balance 0stake is smaller than %s",
		coin,
	)
	suite.Require().EqualError(suite.bankKeeper.SendCoins(suite.ctx, accAddrs[0], accAddrs[1], sendCoins), e.Error())
}

func (suite *KeeperTestSuite) TestValidateBalance() {
	ctx := suite.ctx
	require := suite.Require()
	now := cmttime.Now()
	endTime := now.Add(24 * time.Hour)

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	suite.authKeeper.EXPECT().GetAccount(suite.ctx, accAddrs[0]).Return(nil)
	require.Error(suite.bankKeeper.ValidateBalance(ctx, accAddrs[0]))

	balances := sdk.NewCoins(newFooCoin(100))
	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], balances))

	suite.mockValidateBalance(acc0)
	require.NoError(suite.bankKeeper.ValidateBalance(ctx, accAddrs[0]))

	acc1 := authtypes.NewBaseAccountWithAddress(accAddrs[1])
	vacc, err := vesting.NewContinuousVestingAccount(acc1, balances.Add(balances...), now.Unix(), endTime.Unix())
	suite.Require().NoError(err)

	suite.mockFundAccount(accAddrs[1])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[1], balances))

	suite.mockValidateBalance(vacc)
	require.Error(suite.bankKeeper.ValidateBalance(ctx, accAddrs[1]))
}

func (suite *KeeperTestSuite) TestSendEnabled() {
	ctx := suite.ctx
	require := suite.Require()
	enabled := true
	params := banktypes.DefaultParams()
	require.Equal(enabled, params.DefaultSendEnabled)

	require.NoError(suite.bankKeeper.SetParams(ctx, params))

	bondCoin := sdk.NewCoin(sdk.DefaultBondDenom, math.OneInt())
	fooCoin := sdk.NewCoin("foocoin", math.OneInt())
	barCoin := sdk.NewCoin("barcoin", math.OneInt())

	// assert with default (all denom) send enabled both Bar and Bond Denom are enabled
	require.Equal(enabled, suite.bankKeeper.IsSendEnabledCoin(ctx, barCoin))
	require.Equal(enabled, suite.bankKeeper.IsSendEnabledCoin(ctx, bondCoin))

	// Both coins should be send enabled.
	err := suite.bankKeeper.IsSendEnabledCoins(ctx, fooCoin, bondCoin)
	require.NoError(err)

	// Set default send_enabled to !enabled, add a foodenom that overrides default as enabled
	params.DefaultSendEnabled = !enabled
	require.NoError(suite.bankKeeper.SetParams(ctx, params))
	suite.bankKeeper.SetSendEnabled(ctx, fooCoin.Denom, enabled)

	// Expect our specific override to be enabled, others to be !enabled.
	require.Equal(enabled, suite.bankKeeper.IsSendEnabledCoin(ctx, fooCoin))
	require.Equal(!enabled, suite.bankKeeper.IsSendEnabledCoin(ctx, barCoin))
	require.Equal(!enabled, suite.bankKeeper.IsSendEnabledCoin(ctx, bondCoin))

	// Foo coin should be send enabled.
	err = suite.bankKeeper.IsSendEnabledCoins(ctx, fooCoin)
	require.NoError(err)

	// Expect an error when one coin is not send enabled.
	err = suite.bankKeeper.IsSendEnabledCoins(ctx, fooCoin, bondCoin)
	require.Error(err)

	// Expect an error when all coins are not send enabled.
	err = suite.bankKeeper.IsSendEnabledCoins(ctx, bondCoin, barCoin)
	require.Error(err)
}

func (suite *KeeperTestSuite) TestHasBalance() {
	ctx := suite.ctx
	require := suite.Require()

	balances := sdk.NewCoins(newFooCoin(100))
	require.False(suite.bankKeeper.HasBalance(ctx, accAddrs[0], newFooCoin(99)))

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], balances))
	require.False(suite.bankKeeper.HasBalance(ctx, accAddrs[0], newFooCoin(101)))
	require.True(suite.bankKeeper.HasBalance(ctx, accAddrs[0], newFooCoin(100)))
	require.True(suite.bankKeeper.HasBalance(ctx, accAddrs[0], newFooCoin(1)))
}

func (suite *KeeperTestSuite) TestMsgSendEvents() {
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	newCoins := sdk.NewCoins(sdk.NewInt64Coin(fooDenom, 50))
	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], newCoins))

	suite.mockSendCoins(ctx, acc0, accAddrs[1])
	require.NoError(suite.bankKeeper.SendCoins(ctx, accAddrs[0], accAddrs[1], newCoins))
	event1 := sdk.Event{
		Type:       banktypes.EventTypeTransfer,
		Attributes: []abci.EventAttribute{},
	}
	event1.Attributes = append(
		event1.Attributes,
		abci.EventAttribute{Key: banktypes.AttributeKeyRecipient, Value: accAddrs[1].String()},
	)
	event1.Attributes = append(
		event1.Attributes,
		abci.EventAttribute{Key: banktypes.AttributeKeySender, Value: accAddrs[0].String()},
	)
	event1.Attributes = append(
		event1.Attributes,
		abci.EventAttribute{Key: sdk.AttributeKeyAmount, Value: newCoins.String()},
	)

	event2 := sdk.Event{
		Type:       sdk.EventTypeMessage,
		Attributes: []abci.EventAttribute{},
	}
	event2.Attributes = append(
		event2.Attributes,
		abci.EventAttribute{Key: banktypes.AttributeKeySender, Value: accAddrs[0].String()},
	)

	// events are shifted due to the funding account events
	events := ctx.EventManager().ABCIEvents()
	require.Equal(10, len(events))
	require.Equal(abci.Event(event1), events[8])
	require.Equal(abci.Event(event2), events[9])
}

func (suite *KeeperTestSuite) TestMsgMultiSendEvents() {
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()
	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])

	require.NoError(suite.bankKeeper.SetParams(ctx, banktypes.DefaultParams()))

	coins := sdk.NewCoins(sdk.NewInt64Coin(fooDenom, 50), sdk.NewInt64Coin(barDenom, 100))
	newCoins := sdk.NewCoins(sdk.NewInt64Coin(fooDenom, 50))
	newCoins2 := sdk.NewCoins(sdk.NewInt64Coin(barDenom, 100))
	input := banktypes.Input{
		Address: accAddrs[0].String(),
		Coins:   coins,
	}
	outputs := []banktypes.Output{
		{Address: accAddrs[2].String(), Coins: newCoins},
		{Address: accAddrs[3].String(), Coins: newCoins2},
	}

	suite.authKeeper.EXPECT().GetAccount(suite.ctx, accAddrs[0]).Return(acc0)
	suite.authKeeper.EXPECT().HasAccount(gomock.Any(), gomock.Any()).Return(true).Times(len(outputs))

	require.Error(suite.bankKeeper.InputOutputCoins(ctx, input, outputs))

	events := ctx.EventManager().ABCIEvents()
	require.Equal(1, len(events))

	// Set addr's coins but not accAddrs[1]'s coins
	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], sdk.NewCoins(sdk.NewInt64Coin(fooDenom, 50), sdk.NewInt64Coin(barDenom, 100))))

	suite.mockInputOutputCoins([]sdk.AccountI{acc0}, accAddrs[2:4])
	require.NoError(suite.bankKeeper.InputOutputCoins(ctx, input, outputs))

	events = ctx.EventManager().ABCIEvents()
	require.Equal(13, len(events)) // 13 events because account funding causes extra minting + coin_spent + coin_recv events

	event1 := sdk.Event{
		Type:       sdk.EventTypeMessage,
		Attributes: []abci.EventAttribute{},
	}
	event1.Attributes = append(
		event1.Attributes,
		abci.EventAttribute{Key: banktypes.AttributeKeySender, Value: accAddrs[0].String()},
	)
	require.Equal(abci.Event(event1), events[7])

	// Set addr's coins and accAddrs[1]'s coins
	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], sdk.NewCoins(sdk.NewInt64Coin(fooDenom, 50))))
	newCoins = sdk.NewCoins(sdk.NewInt64Coin(fooDenom, 50))

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], sdk.NewCoins(sdk.NewInt64Coin(barDenom, 100))))
	newCoins2 = sdk.NewCoins(sdk.NewInt64Coin(barDenom, 100))

	suite.mockInputOutputCoins([]sdk.AccountI{acc0}, accAddrs[2:4])
	require.NoError(suite.bankKeeper.InputOutputCoins(ctx, input, outputs))

	events = ctx.EventManager().ABCIEvents()
	require.Equal(31, len(events)) // 31 due to account funding + coin_spent + coin_recv events

	event2 := sdk.Event{
		Type:       banktypes.EventTypeTransfer,
		Attributes: []abci.EventAttribute{},
	}
	event2.Attributes = append(
		event2.Attributes,
		abci.EventAttribute{Key: banktypes.AttributeKeyRecipient, Value: accAddrs[2].String()},
		abci.EventAttribute{Key: banktypes.AttributeKeySender, Value: accAddrs[0].String()},
		abci.EventAttribute{Key: sdk.AttributeKeyAmount, Value: newCoins.String()},
	)
	event3 := sdk.Event{
		Type:       banktypes.EventTypeTransfer,
		Attributes: []abci.EventAttribute{},
	}
	event3.Attributes = append(
		event3.Attributes,
		abci.EventAttribute{Key: banktypes.AttributeKeyRecipient, Value: accAddrs[3].String()},
		abci.EventAttribute{Key: banktypes.AttributeKeySender, Value: accAddrs[0].String()},
		abci.EventAttribute{Key: sdk.AttributeKeyAmount, Value: newCoins2.String()},
	)
	require.Contains(events, abci.Event(event1))
	require.Contains(events, abci.Event(event2))
	require.Contains(events, abci.Event(event3))
}

func (suite *KeeperTestSuite) TestSpendableCoins() {
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()
	now := cmttime.Now()
	endTime := now.Add(24 * time.Hour)

	origCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 100))
	lockedCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 50))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	acc1 := authtypes.NewBaseAccountWithAddress(accAddrs[1])
	vacc, err := vesting.NewContinuousVestingAccount(acc0, origCoins, ctx.BlockHeader().Time.Unix(), endTime.Unix())
	suite.Require().NoError(err)

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], origCoins))

	suite.mockFundAccount(accAddrs[1])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[1], origCoins))

	suite.mockSpendableCoins(ctx, acc1)
	require.Equal(origCoins, suite.bankKeeper.SpendableCoins(ctx, accAddrs[1]))

	suite.mockSpendableCoins(ctx, acc1)
	require.Equal(origCoins[0], suite.bankKeeper.SpendableCoin(ctx, accAddrs[1], "stake"))

	ctx = ctx.WithBlockTime(now.Add(12 * time.Hour))
	suite.mockSpendableCoins(ctx, vacc)
	require.Equal(origCoins.Sub(lockedCoins...), suite.bankKeeper.SpendableCoins(ctx, accAddrs[0]))

	suite.mockSpendableCoins(ctx, vacc)
	require.Equal(origCoins.Sub(lockedCoins...)[0], suite.bankKeeper.SpendableCoin(ctx, accAddrs[0], "stake"))
}

func (suite *KeeperTestSuite) TestVestingAccountSend() {
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()
	now := cmttime.Now()
	endTime := now.Add(24 * time.Hour)

	origCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 100))
	sendCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 50))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	vacc, err := vesting.NewContinuousVestingAccount(acc0, origCoins, now.Unix(), endTime.Unix())
	suite.Require().NoError(err)

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], origCoins))

	// require that no coins be sendable at the beginning of the vesting schedule
	suite.authKeeper.EXPECT().GetAccount(ctx, accAddrs[0]).Return(vacc)
	require.Error(suite.bankKeeper.SendCoins(ctx, accAddrs[0], accAddrs[1], sendCoins))

	// receive some coins
	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], sendCoins))
	// require that all vested coins are spendable plus any received
	ctx = ctx.WithBlockTime(now.Add(12 * time.Hour))
	suite.mockSendCoins(ctx, vacc, accAddrs[1])
	require.NoError(suite.bankKeeper.SendCoins(ctx, accAddrs[0], accAddrs[1], sendCoins))
	require.Equal(origCoins, suite.bankKeeper.GetAllBalances(ctx, accAddrs[0]))
}

func (suite *KeeperTestSuite) TestPeriodicVestingAccountSend() {
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()
	now := cmttime.Now()
	origCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 100))
	sendCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 50))

	periods := vesting.Periods{
		vesting.Period{Length: int64(12 * 60 * 60), Amount: sdk.Coins{sdk.NewInt64Coin("stake", 50)}},
		vesting.Period{Length: int64(6 * 60 * 60), Amount: sdk.Coins{sdk.NewInt64Coin("stake", 25)}},
		vesting.Period{Length: int64(6 * 60 * 60), Amount: sdk.Coins{sdk.NewInt64Coin("stake", 25)}},
	}

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	vacc, err := vesting.NewPeriodicVestingAccount(acc0, origCoins, ctx.BlockHeader().Time.Unix(), periods)
	suite.Require().NoError(err)

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], origCoins))

	// require that no coins be sendable at the beginning of the vesting schedule
	suite.authKeeper.EXPECT().GetAccount(ctx, accAddrs[0]).Return(vacc)
	require.Error(suite.bankKeeper.SendCoins(ctx, accAddrs[0], accAddrs[1], sendCoins))

	// receive some coins
	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], sendCoins))

	// require that all vested coins are spendable plus any received
	ctx = ctx.WithBlockTime(now.Add(12 * time.Hour))
	suite.mockSendCoins(ctx, vacc, accAddrs[1])
	require.NoError(suite.bankKeeper.SendCoins(ctx, accAddrs[0], accAddrs[1], sendCoins))
	require.Equal(origCoins, suite.bankKeeper.GetAllBalances(ctx, accAddrs[0]))
}

func (suite *KeeperTestSuite) TestVestingAccountReceive() {
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()
	now := cmttime.Now()
	endTime := now.Add(24 * time.Hour)

	origCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 100))
	sendCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 50))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	acc1 := authtypes.NewBaseAccountWithAddress(accAddrs[1])
	vacc, err := vesting.NewContinuousVestingAccount(acc0, origCoins, ctx.BlockHeader().Time.Unix(), endTime.Unix())
	suite.Require().NoError(err)

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], origCoins))

	suite.mockFundAccount(accAddrs[1])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[1], origCoins))

	// send some coins to the vesting account
	suite.mockSendCoins(ctx, acc1, accAddrs[0])
	require.NoError(suite.bankKeeper.SendCoins(ctx, accAddrs[1], accAddrs[0], sendCoins))

	// require the coins are spendable
	balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[0])
	require.Equal(origCoins.Add(sendCoins...), balances)
	require.Equal(balances.Sub(vacc.LockedCoins(now)...), sendCoins)

	// require coins are spendable plus any that have vested
	require.Equal(balances.Sub(vacc.LockedCoins(now.Add(12*time.Hour))...), origCoins)
}

func (suite *KeeperTestSuite) TestPeriodicVestingAccountReceive() {
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()
	now := cmttime.Now()

	origCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 100))
	sendCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 50))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	acc1 := authtypes.NewBaseAccountWithAddress(accAddrs[1])
	periods := vesting.Periods{
		vesting.Period{Length: int64(12 * 60 * 60), Amount: sdk.Coins{sdk.NewInt64Coin("stake", 50)}},
		vesting.Period{Length: int64(6 * 60 * 60), Amount: sdk.Coins{sdk.NewInt64Coin("stake", 25)}},
		vesting.Period{Length: int64(6 * 60 * 60), Amount: sdk.Coins{sdk.NewInt64Coin("stake", 25)}},
	}

	vacc, err := vesting.NewPeriodicVestingAccount(acc0, origCoins, ctx.BlockHeader().Time.Unix(), periods)
	suite.Require().NoError(err)

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], origCoins))

	suite.mockFundAccount(accAddrs[1])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[1], origCoins))

	// send some coins to the vesting account
	suite.mockSendCoins(ctx, acc1, accAddrs[0])
	require.NoError(suite.bankKeeper.SendCoins(ctx, accAddrs[1], accAddrs[0], sendCoins))

	// require the coins are spendable
	balances := suite.bankKeeper.GetAllBalances(ctx, accAddrs[0])
	require.Equal(origCoins.Add(sendCoins...), balances)
	require.Equal(balances.Sub(vacc.LockedCoins(now)...), sendCoins)

	// require coins are spendable plus any that have vested
	require.Equal(balances.Sub(vacc.LockedCoins(now.Add(12*time.Hour))...), origCoins)
}

func (suite *KeeperTestSuite) TestDelegateCoins() {
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()
	now := cmttime.Now()
	endTime := now.Add(24 * time.Hour)

	origCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 100))
	delCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 50))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	acc1 := authtypes.NewBaseAccountWithAddress(accAddrs[1])
	vacc, err := vesting.NewContinuousVestingAccount(acc0, origCoins, ctx.BlockHeader().Time.Unix(), endTime.Unix())
	suite.Require().NoError(err)

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], origCoins))

	suite.mockFundAccount(accAddrs[1])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[1], origCoins))

	ctx = ctx.WithBlockTime(now.Add(12 * time.Hour))

	// require the ability for a non-vesting account to delegate
	suite.mockDelegateCoins(ctx, acc1, holderAcc)
	require.NoError(suite.bankKeeper.DelegateCoins(ctx, accAddrs[1], holderAcc.GetAddress(), delCoins))
	require.Equal(origCoins.Sub(delCoins...), suite.bankKeeper.GetAllBalances(ctx, accAddrs[1]))
	require.Equal(delCoins, suite.bankKeeper.GetAllBalances(ctx, holderAcc.GetAddress()))

	// require the ability for a vesting account to delegate
	suite.mockDelegateCoins(ctx, vacc, holderAcc)
	require.NoError(suite.bankKeeper.DelegateCoins(ctx, accAddrs[0], holderAcc.GetAddress(), delCoins))
	require.Equal(delCoins, suite.bankKeeper.GetAllBalances(ctx, accAddrs[0]))

	// require that delegated vesting amount is equal to what was delegated with DelegateCoins
	require.Equal(delCoins, vacc.GetDelegatedVesting())
}

func (suite *KeeperTestSuite) TestDelegateCoins_Invalid() {
	ctx := suite.ctx
	require := suite.Require()

	origCoins := sdk.NewCoins(newFooCoin(100))
	delCoins := sdk.NewCoins(newFooCoin(50))

	suite.authKeeper.EXPECT().GetAccount(ctx, holderAcc.GetAddress()).Return(nil)
	require.Error(suite.bankKeeper.DelegateCoins(ctx, accAddrs[0], holderAcc.GetAddress(), delCoins))

	suite.authKeeper.EXPECT().GetAccount(ctx, holderAcc.GetAddress()).Return(holderAcc)
	invalidCoins := sdk.Coins{sdk.Coin{Denom: "fooDenom", Amount: math.NewInt(-50)}}
	require.Error(suite.bankKeeper.DelegateCoins(ctx, accAddrs[0], holderAcc.GetAddress(), invalidCoins))

	suite.authKeeper.EXPECT().GetAccount(ctx, holderAcc.GetAddress()).Return(holderAcc)
	require.Error(suite.bankKeeper.DelegateCoins(ctx, accAddrs[0], holderAcc.GetAddress(), delCoins))

	suite.authKeeper.EXPECT().GetAccount(ctx, holderAcc.GetAddress()).Return(holderAcc)
	require.Error(suite.bankKeeper.DelegateCoins(ctx, accAddrs[0], holderAcc.GetAddress(), origCoins.Add(origCoins...)))
}

func (suite *KeeperTestSuite) TestUndelegateCoins() {
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()
	now := cmttime.Now()
	endTime := now.Add(24 * time.Hour)

	origCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 100))
	delCoins := sdk.NewCoins(sdk.NewInt64Coin("stake", 50))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])
	acc1 := authtypes.NewBaseAccountWithAddress(accAddrs[1])
	vacc, err := vesting.NewContinuousVestingAccount(acc0, origCoins, ctx.BlockHeader().Time.Unix(), endTime.Unix())
	suite.Require().NoError(err)

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], origCoins))

	suite.mockFundAccount(accAddrs[1])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[1], origCoins))

	ctx = ctx.WithBlockTime(now.Add(12 * time.Hour))

	// require the ability for a non-vesting account to delegate
	suite.mockDelegateCoins(ctx, acc1, holderAcc)
	require.NoError(suite.bankKeeper.DelegateCoins(ctx, accAddrs[1], holderAcc.GetAddress(), delCoins))

	require.Equal(origCoins.Sub(delCoins...), suite.bankKeeper.GetAllBalances(ctx, accAddrs[1]))
	require.Equal(delCoins, suite.bankKeeper.GetAllBalances(ctx, holderAcc.GetAddress()))

	// require the ability for a non-vesting account to undelegate
	suite.mockUnDelegateCoins(ctx, acc1, holderAcc)
	require.NoError(suite.bankKeeper.UndelegateCoins(ctx, holderAcc.GetAddress(), accAddrs[1], delCoins))

	require.Equal(origCoins, suite.bankKeeper.GetAllBalances(ctx, accAddrs[1]))
	require.True(suite.bankKeeper.GetAllBalances(ctx, holderAcc.GetAddress()).Empty())

	// require the ability for a vesting account to delegate
	suite.mockDelegateCoins(ctx, acc0, holderAcc)
	require.NoError(suite.bankKeeper.DelegateCoins(ctx, accAddrs[0], holderAcc.GetAddress(), delCoins))

	require.Equal(origCoins.Sub(delCoins...), suite.bankKeeper.GetAllBalances(ctx, accAddrs[0]))
	require.Equal(delCoins, suite.bankKeeper.GetAllBalances(ctx, holderAcc.GetAddress()))

	// require the ability for a vesting account to undelegate
	suite.mockUnDelegateCoins(ctx, vacc, holderAcc)
	require.NoError(suite.bankKeeper.UndelegateCoins(ctx, holderAcc.GetAddress(), accAddrs[0], delCoins))

	require.Equal(origCoins, suite.bankKeeper.GetAllBalances(ctx, accAddrs[0]))
	require.True(suite.bankKeeper.GetAllBalances(ctx, holderAcc.GetAddress()).Empty())

	// require that delegated vesting amount is completely empty, since they were completely undelegated
	require.Empty(vacc.GetDelegatedVesting())
}

func (suite *KeeperTestSuite) TestUndelegateCoins_Invalid() {
	ctx := suite.ctx
	require := suite.Require()

	origCoins := sdk.NewCoins(newFooCoin(100))
	delCoins := sdk.NewCoins(newFooCoin(50))

	acc0 := authtypes.NewBaseAccountWithAddress(accAddrs[0])

	suite.authKeeper.EXPECT().GetAccount(ctx, holderAcc.GetAddress()).Return(nil)
	require.Error(suite.bankKeeper.UndelegateCoins(ctx, holderAcc.GetAddress(), accAddrs[0], delCoins))

	suite.mockFundAccount(accAddrs[0])
	require.NoError(banktestutil.FundAccount(ctx, suite.bankKeeper, accAddrs[0], origCoins))

	suite.mockDelegateCoins(ctx, acc0, holderAcc)
	require.NoError(suite.bankKeeper.DelegateCoins(ctx, accAddrs[0], holderAcc.GetAddress(), delCoins))

	suite.authKeeper.EXPECT().GetAccount(ctx, holderAcc.GetAddress()).Return(holderAcc)
	suite.authKeeper.EXPECT().GetAccount(ctx, holderAcc.GetAddress()).Return(holderAcc)
	suite.authKeeper.EXPECT().GetAccount(ctx, acc0.GetAddress()).Return(nil)
	require.Error(suite.bankKeeper.UndelegateCoins(ctx, holderAcc.GetAddress(), accAddrs[0], delCoins))

	suite.authKeeper.EXPECT().GetAccount(ctx, holderAcc.GetAddress()).Return(holderAcc)
	suite.authKeeper.EXPECT().GetAccount(ctx, holderAcc.GetAddress()).Return(holderAcc)
	require.Error(suite.bankKeeper.UndelegateCoins(ctx, holderAcc.GetAddress(), accAddrs[0], origCoins))
}

func (suite *KeeperTestSuite) TestSetDenomMetaData() {
	ctx := suite.ctx
	require := suite.Require()

	metadata := suite.getTestMetadata()

	for i := range []int{1, 2} {
		suite.bankKeeper.SetDenomMetaData(ctx, metadata[i])
	}

	actualMetadata, found := suite.bankKeeper.GetDenomMetaData(ctx, metadata[1].Base)
	require.True(found)
	found = suite.bankKeeper.HasDenomMetaData(ctx, metadata[1].Base)
	require.True(found)
	require.Equal(metadata[1].GetBase(), actualMetadata.GetBase())
	require.Equal(metadata[1].GetDisplay(), actualMetadata.GetDisplay())
	require.Equal(metadata[1].GetDescription(), actualMetadata.GetDescription())
	require.Equal(metadata[1].GetDenomUnits()[1].GetDenom(), actualMetadata.GetDenomUnits()[1].GetDenom())
	require.Equal(metadata[1].GetDenomUnits()[1].GetExponent(), actualMetadata.GetDenomUnits()[1].GetExponent())
	require.Equal(metadata[1].GetDenomUnits()[1].GetAliases(), actualMetadata.GetDenomUnits()[1].GetAliases())
}

func (suite *KeeperTestSuite) TestIterateAllDenomMetaData() {
	ctx := suite.ctx
	require := suite.Require()

	expectedMetadata := suite.getTestMetadata()
	// set metadata
	for i := range []int{1, 2} {
		suite.bankKeeper.SetDenomMetaData(ctx, expectedMetadata[i])
	}
	// retrieve metadata
	actualMetadata := make([]banktypes.Metadata, 0)
	suite.bankKeeper.IterateAllDenomMetaData(ctx, func(metadata banktypes.Metadata) bool {
		actualMetadata = append(actualMetadata, metadata)
		return false
	})
	// execute checks
	for i := range []int{1, 2} {
		require.Equal(expectedMetadata[i].GetBase(), actualMetadata[i].GetBase())
		require.Equal(expectedMetadata[i].GetDisplay(), actualMetadata[i].GetDisplay())
		require.Equal(expectedMetadata[i].GetDescription(), actualMetadata[i].GetDescription())
		require.Equal(expectedMetadata[i].GetDenomUnits()[1].GetDenom(), actualMetadata[i].GetDenomUnits()[1].GetDenom())
		require.Equal(expectedMetadata[i].GetDenomUnits()[1].GetExponent(), actualMetadata[i].GetDenomUnits()[1].GetExponent())
		require.Equal(expectedMetadata[i].GetDenomUnits()[1].GetAliases(), actualMetadata[i].GetDenomUnits()[1].GetAliases())
	}
}

func (suite *KeeperTestSuite) TestBalanceTrackingEvents() {
	require := suite.Require()

	// mint coins
	suite.mockMintCoins(multiPermAcc)
	require.NoError(
		suite.bankKeeper.MintCoins(
			suite.ctx,
			multiPermAcc.Name,
			sdk.NewCoins(sdk.NewCoin("utxo", math.NewInt(100000)))),
	)
	// send coins to address
	suite.mockSendCoinsFromModuleToAccount(multiPermAcc, accAddrs[0])
	require.NoError(
		suite.bankKeeper.SendCoinsFromModuleToAccount(
			suite.ctx,
			multiPermAcc.Name,
			accAddrs[0],
			sdk.NewCoins(sdk.NewCoin("utxo", math.NewInt(50000))),
		),
	)

	// burn coins from module account
	suite.mockBurnCoins(multiPermAcc)
	require.NoError(
		suite.bankKeeper.BurnCoins(
			suite.ctx,
			multiPermAcc.Name,
			sdk.NewCoins(sdk.NewInt64Coin("utxo", 1000)),
		),
	)

	// process balances and supply from events
	supply := sdk.NewCoins()

	balances := make(map[string]sdk.Coins)

	ctx := sdk.UnwrapSDKContext(suite.ctx)

	for _, e := range ctx.EventManager().ABCIEvents() {
		switch e.Type {
		case banktypes.EventTypeCoinBurn:
			burnedCoins, err := sdk.ParseCoinsNormalized(e.Attributes[1].Value)
			require.NoError(err)
			supply = supply.Sub(burnedCoins...)

		case banktypes.EventTypeCoinMint:
			mintedCoins, err := sdk.ParseCoinsNormalized(e.Attributes[1].Value)
			require.NoError(err)
			supply = supply.Add(mintedCoins...)

		case banktypes.EventTypeCoinSpent:
			coinsSpent, err := sdk.ParseCoinsNormalized(e.Attributes[1].Value)
			require.NoError(err)
			spender, err := sdk.AccAddressFromBech32(e.Attributes[0].Value)
			require.NoError(err)
			balances[spender.String()] = balances[spender.String()].Sub(coinsSpent...)

		case banktypes.EventTypeCoinReceived:
			coinsRecv, err := sdk.ParseCoinsNormalized(e.Attributes[1].Value)
			require.NoError(err)
			receiver, err := sdk.AccAddressFromBech32(e.Attributes[0].Value)
			require.NoError(err)
			balances[receiver.String()] = balances[receiver.String()].Add(coinsRecv...)
		}
	}

	// check balance and supply tracking
	require.True(suite.bankKeeper.HasSupply(suite.ctx, "utxo"))
	savedSupply := suite.bankKeeper.GetSupply(suite.ctx, "utxo")
	utxoSupply := savedSupply
	require.Equal(utxoSupply.Amount, supply.AmountOf("utxo"))
	// iterate accounts and check balances
	suite.bankKeeper.IterateAllBalances(suite.ctx, func(address sdk.AccAddress, coin sdk.Coin) (stop bool) {
		// if it's not utxo coin then skip
		if coin.Denom != "utxo" {
			return false
		}

		balance, exists := balances[address.String()]
		require.True(exists)

		expectedUtxo := sdk.NewCoin("utxo", balance.AmountOf(coin.Denom))
		require.Equal(expectedUtxo.String(), coin.String())
		return false
	})
}

func (suite *KeeperTestSuite) getTestMetadata() []banktypes.Metadata {
	return []banktypes.Metadata{
		{
			Name:        "Cosmos Hub Atom",
			Symbol:      "ATOM",
			Description: "The native staking token of the Cosmos Hub.",
			DenomUnits: []*banktypes.DenomUnit{
				{Denom: "uatom", Exponent: uint32(0), Aliases: []string{"microatom"}},
				{Denom: "matom", Exponent: uint32(3), Aliases: []string{"milliatom"}},
				{Denom: "atom", Exponent: uint32(6), Aliases: nil},
			},
			Base:    "uatom",
			Display: "atom",
		},
		{
			Name:        "Token",
			Symbol:      "TOKEN",
			Description: "The native staking token of the Token Hub.",
			DenomUnits: []*banktypes.DenomUnit{
				{Denom: "1token", Exponent: uint32(5), Aliases: []string{"decitoken"}},
				{Denom: "2token", Exponent: uint32(4), Aliases: []string{"centitoken"}},
				{Denom: "3token", Exponent: uint32(7), Aliases: []string{"dekatoken"}},
			},
			Base:    "utoken",
			Display: "token",
		},
	}
}

func (suite *KeeperTestSuite) TestMintCoinDenomGuard() {
	specs := map[string]struct {
		amounts sdk.Coins
		expErr  error
	}{
		"valid": {
			amounts: sdk.NewCoins(sdk.Coin{Denom: "stake", Amount: math.OneInt()}),
		},
		"invalid denom": {
			amounts: []sdk.Coin{{Denom: "11stake", Amount: math.OneInt()}},
			expErr:  sdkerrors.ErrInvalidCoins,
		},
		"invalid denom - multiple": {
			amounts: []sdk.Coin{newFooCoin(50), {Denom: "11stake", Amount: math.OneInt()}},
			expErr:  sdkerrors.ErrInvalidCoins,
		},
	}
	for name, spec := range specs {
		suite.T().Run(name, func(t *testing.T) {
			suite.mockMintCoins(multiPermAcc)
			gotErr := suite.bankKeeper.MintCoins(suite.ctx, multiPermAcc.Name, spec.amounts)
			if spec.expErr != nil {
				suite.Require().ErrorIs(gotErr, spec.expErr)
				return
			}
			suite.Require().NoError(gotErr)
		})
	}
}

func (suite *KeeperTestSuite) TestMintCoinRestrictions() {
	type BankMintingRestrictionFn func(ctx context.Context, coins sdk.Coins) error
	require := suite.Require()

	type testCase struct {
		coinsToTry sdk.Coin
		expectPass bool
	}

	tests := []struct {
		name          string
		restrictionFn BankMintingRestrictionFn
		testCases     []testCase
	}{
		{
			"restriction",
			func(_ context.Context, coins sdk.Coins) error {
				for _, coin := range coins {
					if coin.Denom != fooDenom {
						return fmt.Errorf("Module %s only has perms for minting %s coins, tried minting %s coins", banktypes.ModuleName, fooDenom, coin.Denom)
					}
				}
				return nil
			},
			[]testCase{
				{
					coinsToTry: newFooCoin(100),
					expectPass: true,
				},
				{
					coinsToTry: newBarCoin(100),
					expectPass: false,
				},
			},
		},
	}

	for _, test := range tests {
		keeper := suite.bankKeeper.WithMintCoinsRestriction(banktypes.MintingRestrictionFn(test.restrictionFn))
		for _, testCase := range test.testCases {
			if testCase.expectPass {
				suite.mockMintCoins(multiPermAcc)
				require.NoError(
					keeper.MintCoins(
						suite.ctx,
						multiPermAcc.Name,
						sdk.NewCoins(testCase.coinsToTry),
					),
				)
			} else {
				require.Error(
					keeper.MintCoins(
						suite.ctx,
						multiPermAcc.Name,
						sdk.NewCoins(testCase.coinsToTry),
					),
				)
			}
		}
	}
}

func (suite *KeeperTestSuite) TestBurnCoinDenomGuard() {
	suite.mockMintCoins(multiPermAcc)
	myCoins := sdk.NewCoins(sdk.NewCoin("stake", math.OneInt()))
	suite.Require().NoError(suite.bankKeeper.MintCoins(suite.ctx, multiPermAcc.Name, myCoins))

	specs := map[string]struct {
		amounts sdk.Coins
		expErr  error
	}{
		"valid": {
			amounts: sdk.NewCoins(sdk.Coin{Denom: "stake", Amount: math.OneInt()}),
		},
		"invalid denom": {
			amounts: []sdk.Coin{{Denom: "11stake", Amount: math.OneInt()}},
			expErr:  sdkerrors.ErrInvalidCoins,
		},
		"invalid denom - multiple": {
			amounts: []sdk.Coin{newFooCoin(50), {Denom: "11stake", Amount: math.OneInt()}},
			expErr:  sdkerrors.ErrInvalidCoins,
		},
	}
	for name, spec := range specs {
		suite.T().Run(name, func(t *testing.T) {
			suite.authKeeper.EXPECT().GetModuleAccount(suite.ctx, multiPermAcc.Name).Return(multiPermAcc)
			if spec.expErr == nil {
				suite.authKeeper.EXPECT().GetAccount(suite.ctx, multiPermAcc.GetAddress()).Return(multiPermAcc)
			}
			// when
			gotErr := suite.bankKeeper.BurnCoins(suite.ctx, multiPermAcc.Name, spec.amounts)
			if spec.expErr != nil {
				suite.Require().ErrorIs(gotErr, spec.expErr)
				return
			}
			suite.Require().NoError(gotErr)
		})
	}
}

func (suite *KeeperTestSuite) TestIsSendEnabledDenom() {
	ctx, bankKeeper := suite.ctx, suite.bankKeeper
	require := suite.Require()

	defaultCoin := "defaultCoin"
	enabledCoin := "enabledCoin"
	disabledCoin := "disabledCoin"
	bankKeeper.DeleteSendEnabled(ctx, defaultCoin)
	bankKeeper.SetSendEnabled(ctx, enabledCoin, true)
	bankKeeper.SetSendEnabled(ctx, disabledCoin, false)

	tests := []struct {
		denom  string
		exp    bool
		expDef bool
	}{
		{
			denom:  "defaultCoin",
			expDef: true,
		},
		{
			denom: enabledCoin,
			exp:   true,
		},
		{
			denom: disabledCoin,
			exp:   false,
		},
	}

	for _, def := range []bool{true, false} {
		params := banktypes.Params{DefaultSendEnabled: def}
		require.NoError(bankKeeper.SetParams(ctx, params))

		for _, tc := range tests {
			suite.T().Run(fmt.Sprintf("%s default %t", tc.denom, def), func(t *testing.T) {
				actual := suite.bankKeeper.IsSendEnabledDenom(suite.ctx, tc.denom)
				exp := tc.exp
				if tc.expDef {
					exp = def
				}

				require.Equal(exp, actual)
			})
		}
	}
}

func (suite *KeeperTestSuite) TestGetSendEnabledEntry() {
	ctx, bankKeeper := suite.ctx, suite.bankKeeper
	require := suite.Require()

	bankKeeper.SetAllSendEnabled(ctx, []*banktypes.SendEnabled{
		{Denom: "gettruecoin", Enabled: true},
		{Denom: "getfalsecoin", Enabled: false},
	})

	tests := []struct {
		denom string
		expSE banktypes.SendEnabled
		expF  bool
	}{
		{
			denom: "missing",
			expSE: banktypes.SendEnabled{},
			expF:  false,
		},
		{
			denom: "gettruecoin",
			expSE: banktypes.SendEnabled{Denom: "gettruecoin", Enabled: true},
			expF:  true,
		},
		{
			denom: "getfalsecoin",
			expSE: banktypes.SendEnabled{Denom: "getfalsecoin", Enabled: false},
			expF:  true,
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.denom, func(t *testing.T) {
			actualSE, actualF := bankKeeper.GetSendEnabledEntry(ctx, tc.denom)
			require.Equal(tc.expF, actualF, "found")
			require.Equal(tc.expSE, actualSE, "SendEnabled")
		})
	}
}

func (suite *KeeperTestSuite) TestSetSendEnabled() {
	ctx, bankKeeper := suite.ctx, suite.bankKeeper
	require := suite.Require()

	tests := []struct {
		name  string
		denom string
		value bool
	}{
		{
			name:  "very short denom true",
			denom: "f",
			value: true,
		},
		{
			name:  "very short denom false",
			denom: "f",
			value: true,
		},
		{
			name:  "falseFirstCoin false",
			denom: "falseFirstCoin",
			value: false,
		},
		{
			name:  "falseFirstCoin true",
			denom: "falseFirstCoin",
			value: true,
		},
		{
			name:  "falseFirstCoin true again",
			denom: "falseFirstCoin",
			value: true,
		},
		{
			name:  "trueFirstCoin true",
			denom: "falseFirstCoin",
			value: false,
		},
		{
			name:  "trueFirstCoin false",
			denom: "falseFirstCoin",
			value: false,
		},
		{
			name:  "trueFirstCoin false again",
			denom: "falseFirstCoin",
			value: false,
		},
	}

	for _, def := range []bool{true, false} {
		params := banktypes.Params{DefaultSendEnabled: def}
		require.NoError(bankKeeper.SetParams(ctx, params))

		for _, tc := range tests {
			suite.T().Run(fmt.Sprintf("%s default %t", tc.name, def), func(t *testing.T) {
				bankKeeper.SetSendEnabled(ctx, tc.denom, tc.value)
				actual := bankKeeper.IsSendEnabledDenom(ctx, tc.denom)
				require.Equal(tc.value, actual)
			})
		}
	}
}

func (suite *KeeperTestSuite) TestSetAllSendEnabled() {
	ctx, bankKeeper := suite.ctx, suite.bankKeeper
	require := suite.Require()

	tests := []struct {
		name         string
		sendEnableds []*banktypes.SendEnabled
	}{
		{
			name:         "nil",
			sendEnableds: nil,
		},
		{
			name:         "empty",
			sendEnableds: []*banktypes.SendEnabled{},
		},
		{
			name: "one true",
			sendEnableds: []*banktypes.SendEnabled{
				{Denom: "aonecoin", Enabled: true},
			},
		},
		{
			name: "one false",
			sendEnableds: []*banktypes.SendEnabled{
				{Denom: "bonecoin", Enabled: false},
			},
		},
		{
			name: "two true",
			sendEnableds: []*banktypes.SendEnabled{
				{Denom: "conecoin", Enabled: true},
				{Denom: "ctwocoin", Enabled: true},
			},
		},
		{
			name: "two true false",
			sendEnableds: []*banktypes.SendEnabled{
				{Denom: "donecoin", Enabled: true},
				{Denom: "dtwocoin", Enabled: false},
			},
		},
		{
			name: "two false true",
			sendEnableds: []*banktypes.SendEnabled{
				{Denom: "eonecoin", Enabled: false},
				{Denom: "etwocoin", Enabled: true},
			},
		},
		{
			name: "two false",
			sendEnableds: []*banktypes.SendEnabled{
				{Denom: "fonecoin", Enabled: false},
				{Denom: "ftwocoin", Enabled: false},
			},
		},
	}

	for _, def := range []bool{true, false} {
		params := banktypes.Params{DefaultSendEnabled: def}
		require.NoError(bankKeeper.SetParams(ctx, params))

		for _, tc := range tests {
			suite.T().Run(fmt.Sprintf("%s default %t", tc.name, def), func(t *testing.T) {
				bankKeeper.SetAllSendEnabled(ctx, tc.sendEnableds)

				for _, se := range tc.sendEnableds {
					actual := bankKeeper.IsSendEnabledDenom(ctx, se.Denom)
					require.Equal(se.Enabled, actual, se.Denom)
				}
			})
		}
	}
}

func (suite *KeeperTestSuite) TestDeleteSendEnabled() {
	ctx, bankKeeper := suite.ctx, suite.bankKeeper
	require := suite.Require()

	for _, def := range []bool{true, false} {
		params := banktypes.Params{DefaultSendEnabled: def}
		require.NoError(bankKeeper.SetParams(ctx, params))
		suite.T().Run(fmt.Sprintf("default %t", def), func(t *testing.T) {
			denom := fmt.Sprintf("somerand%tcoin", !def)
			bankKeeper.SetSendEnabled(ctx, denom, !def)
			require.Equal(!def, bankKeeper.IsSendEnabledDenom(ctx, denom))
			bankKeeper.DeleteSendEnabled(ctx, denom)
			require.Equal(def, bankKeeper.IsSendEnabledDenom(ctx, denom))
		})
	}
}

func (suite *KeeperTestSuite) TestIterateSendEnabledEntries() {
	ctx, bankKeeper := suite.ctx, suite.bankKeeper
	require := suite.Require()

	suite.T().Run("no entries to iterate", func(t *testing.T) {
		count := 0
		bankKeeper.IterateSendEnabledEntries(ctx, func(_ string, _ bool) (stop bool) {
			count++
			return false
		})

		require.Equal(0, count)
	})

	alpha := strings.Split("abcdefghijklmnopqrstuvwxyz", "")
	denoms := make([]string, len(alpha)*2)
	for i, l := range alpha {
		denoms[i*2] = fmt.Sprintf("%sitercointrue", l)
		denoms[i*2+1] = fmt.Sprintf("%sitercoinfalse", l)
		bankKeeper.SetSendEnabled(ctx, denoms[i*2], true)
		bankKeeper.SetSendEnabled(ctx, denoms[i*2+1], false)
	}

	for _, def := range []bool{true, false} {
		params := banktypes.Params{DefaultSendEnabled: def}
		require.NoError(bankKeeper.SetParams(ctx, params))

		var seen []string
		suite.T().Run(fmt.Sprintf("all denoms have expected values default %t", def), func(t *testing.T) {
			bankKeeper.IterateSendEnabledEntries(ctx, func(denom string, sendEnabled bool) (stop bool) {
				seen = append(seen, denom)
				exp := !strings.HasSuffix(denom, "false")

				require.Equal(exp, sendEnabled, denom)
				return false
			})
		})

		suite.T().Run(fmt.Sprintf("all denoms were seen default %t", def), func(t *testing.T) {
			require.ElementsMatch(denoms, seen)
		})
	}

	bankKeeper.DeleteSendEnabled(ctx, denoms...)

	suite.T().Run("no entries to iterate again after deleting all of them", func(t *testing.T) {
		count := 0
		bankKeeper.IterateSendEnabledEntries(ctx, func(_ string, _ bool) (stop bool) {
			count++
			return false
		})

		require.Equal(0, count)
	})
}

func (suite *KeeperTestSuite) TestGetAllSendEnabledEntries() {
	ctx, bankKeeper := suite.ctx, suite.bankKeeper
	require := suite.Require()

	suite.T().Run("no entries", func(t *testing.T) {
		actual := bankKeeper.GetAllSendEnabledEntries(ctx)
		require.Len(actual, 0)
	})

	alpha := strings.Split("abcdefghijklmnopqrstuvwxyz", "")
	denoms := make([]string, len(alpha)*2)
	for i, l := range alpha {
		denoms[i*2] = fmt.Sprintf("%sitercointrue", l)
		denoms[i*2+1] = fmt.Sprintf("%sitercoinfalse", l)
		bankKeeper.SetSendEnabled(ctx, denoms[i*2], true)
		bankKeeper.SetSendEnabled(ctx, denoms[i*2+1], false)
	}

	for _, def := range []bool{true, false} {
		params := banktypes.Params{DefaultSendEnabled: def}
		require.NoError(bankKeeper.SetParams(ctx, params))

		var seen []string
		suite.T().Run(fmt.Sprintf("all denoms have expected values default %t", def), func(t *testing.T) {
			actual := bankKeeper.GetAllSendEnabledEntries(ctx)
			for _, se := range actual {
				seen = append(seen, se.Denom)
				exp := true
				if strings.HasSuffix(se.Denom, "false") {
					exp = false
				}

				require.Equal(exp, se.Enabled, se.Denom)
			}
		})

		suite.T().Run(fmt.Sprintf("all denoms were seen default %t", def), func(t *testing.T) {
			require.ElementsMatch(denoms, seen)
		})
	}

	for _, denom := range denoms {
		bankKeeper.DeleteSendEnabled(ctx, denom)
	}

	suite.T().Run("no entries again after deleting all of them", func(t *testing.T) {
		actual := bankKeeper.GetAllSendEnabledEntries(ctx)
		require.Len(actual, 0)
	})
}

func (suite *KeeperTestSuite) TestGetAllBalances() {
	addr0 := sdk.AccAddress("addr_0______________")
	addr1 := sdk.AccAddress("addr_1______________")
	addr2 := sdk.AccAddress("addr_2______________")
	addr5 := sdk.AccAddress("addr_5______________")
	addr20 := sdk.AccAddress("addr_20_____________")

	bal0 := sdk.Coins(nil)
	bal1 := sdk.NewCoins(sdk.NewInt64Coin("one", 111))
	bal2 := sdk.NewCoins(sdk.NewInt64Coin("one", 222), sdk.NewInt64Coin("two", 234))
	bal5 := sdk.NewCoins(
		sdk.NewInt64Coin("one", 37), sdk.NewInt64Coin("two", 3), sdk.NewInt64Coin("three", 399),
		sdk.NewInt64Coin("four", 3456), sdk.NewInt64Coin("five", 321),
	)
	bal20 := sdk.NewCoins(
		sdk.NewInt64Coin("one", 4987), sdk.NewInt64Coin("two", 444), sdk.NewInt64Coin("three", 41),
		sdk.NewInt64Coin("four", 442), sdk.NewInt64Coin("five", 443), sdk.NewInt64Coin("six", 46622),
		sdk.NewInt64Coin("seven", 487), sdk.NewInt64Coin("eight", 4), sdk.NewInt64Coin("nine", 4991),
		sdk.NewInt64Coin("ten", 40000000), sdk.NewInt64Coin("eleven", 4545), sdk.NewInt64Coin("twelve", 41212),
		sdk.NewInt64Coin("thirteen", 41333), sdk.NewInt64Coin("fourteen", 401414), sdk.NewInt64Coin("fifteen", 47),
		sdk.NewInt64Coin("sixteen", 400016), sdk.NewInt64Coin("seventeen", 404), sdk.NewInt64Coin("eighteen", 4454),
		sdk.NewInt64Coin("ninteen", 41298), sdk.NewInt64Coin("twenty", 456789),
	)

	bals := []struct {
		Addr  sdk.AccAddress
		Coins sdk.Coins
	}{
		{Addr: addr0, Coins: bal0},
		{Addr: addr1, Coins: bal1},
		{Addr: addr2, Coins: bal2},
		{Addr: addr5, Coins: bal5},
		{Addr: addr20, Coins: bal20},
	}

	for _, bal := range bals {
		if bal.Coins.IsZero() {
			continue
		}

		suite.mockMintCoins(minterAcc)
		suite.Require().NoError(suite.bankKeeper.MintCoins(suite.ctx, authtypes.Minter, bal.Coins),
			"minting %s for %s", bal.Coins, string(bal.Addr))
		suite.mockSendCoinsFromModuleToAccount(minterAcc, bal.Addr)
		suite.Require().NoError(
			suite.bankKeeper.SendCoinsFromModuleToAccount(suite.ctx, authtypes.Minter, bal.Addr, bal.Coins),
			"sending freshly minted %s to %s", bal.Coins, string(bal.Addr),
		)
	}

	tests := []struct {
		name string
		addr sdk.AccAddress
		exp  sdk.Coins
	}{
		{name: "nil addr", addr: nil, exp: nil},
		{name: "empty addr", addr: sdk.AccAddress{}, exp: nil},
		{name: "addr without anything", addr: addr0, exp: bal0},
		{name: "addr with one denom", addr: addr1, exp: bal1},
		{name: "addr with two denom", addr: addr2, exp: bal2},
		{name: "addr with five denom", addr: addr5, exp: bal5},
		{name: "addr with twenty denom", addr: addr20, exp: bal20},
	}

	count := 100
	for _, tc := range tests {
		suite.Run(tc.name, func() {
			var act sdk.Coins
			testFunc := func() {
				act = suite.bankKeeper.GetAllBalances(suite.ctx, tc.addr)
			}
			for i := 1; i <= count; i++ {
				suite.Require().NotPanics(testFunc, "[%d/%d]: GetAllBalances", i, count)
				if !suite.Assert().Equal(tc.exp.String(), act.String(), "[%d/%d]: GetAllBalances result", i, count) && i == 1 {
					// If it fails on the first one, stop now since that probably means they'll all fail.
					// If it's not the first, keep going so it's easier to see that it's not deterministic.
					break
				}
				actSorted := copyAndSortCoins(act)
				suite.Assert().Equal(actSorted, act,
					"[%d/%d]: GetAllBalances result is not sorted.\nexpected: %s\nactual  : %s",
					i, count, actSorted, act)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestGetAccountsBalances() {
	addrNames := make(map[string]string)
	balsToStrings := func(bals []banktypes.Balance) []string {
		if bals == nil {
			return nil
		}
		rv := make([]string, len(bals))
		for i, bal := range bals {
			rv[i] = fmt.Sprintf("%s (%s) = %s", bal.Address, addrNames[bal.Address], bal.Coins)
		}
		return rv
	}

	addrA := sdk.AccAddress("addr_a______________")
	addrB := sdk.AccAddress("addr_b______________")
	addrC := sdk.AccAddress("addr_c______________")
	addrD := sdk.AccAddress("addr_d______________")
	addrE := sdk.AccAddress("addr_e______________")
	addrF := sdk.AccAddress("addr_f______________")
	addrG := sdk.AccAddress("addr_g______________")

	balA := sdk.NewCoins(sdk.NewInt64Coin("one", 111))
	balB := sdk.NewCoins(
		sdk.NewInt64Coin("one", 4987), sdk.NewInt64Coin("two", 444), sdk.NewInt64Coin("three", 41),
		sdk.NewInt64Coin("four", 442), sdk.NewInt64Coin("five", 443), sdk.NewInt64Coin("six", 46622),
		sdk.NewInt64Coin("seven", 487), sdk.NewInt64Coin("eight", 4), sdk.NewInt64Coin("nine", 4991),
		sdk.NewInt64Coin("ten", 40000000), sdk.NewInt64Coin("eleven", 4545), sdk.NewInt64Coin("twelve", 41212),
		sdk.NewInt64Coin("thirteen", 41333), sdk.NewInt64Coin("fourteen", 401414), sdk.NewInt64Coin("fifteen", 47),
		sdk.NewInt64Coin("sixteen", 400016), sdk.NewInt64Coin("seventeen", 404), sdk.NewInt64Coin("eighteen", 4454),
		sdk.NewInt64Coin("ninteen", 41298), sdk.NewInt64Coin("twenty", 456789),
	)
	balC := sdk.NewCoins(sdk.NewInt64Coin("one", 222), sdk.NewInt64Coin("two", 234))
	balD := sdk.Coins(nil)
	balE := sdk.NewCoins(sdk.NewInt64Coin("banana", 99), sdk.NewInt64Coin("six", 789))
	balF := sdk.NewCoins(
		sdk.NewInt64Coin("one", 37), sdk.NewInt64Coin("two", 3), sdk.NewInt64Coin("three", 399),
		sdk.NewInt64Coin("four", 3456), sdk.NewInt64Coin("five", 321),
	)
	balG := sdk.NewCoins(sdk.NewInt64Coin("one", 543))

	type addrCoins struct {
		Addr  sdk.AccAddress
		Coins sdk.Coins
	}

	bals := []addrCoins{
		{Addr: addrA, Coins: balA},
		{Addr: addrB, Coins: balB},
		{Addr: addrC, Coins: balC},
		{Addr: addrD, Coins: balD},
		{Addr: addrE, Coins: balE},
		{Addr: addrF, Coins: balF},
		{Addr: addrG, Coins: balG},
	}
	slices.SortFunc(bals, func(a, b addrCoins) int {
		return bytes.Compare(a.Addr, b.Addr)
	})

	expBals := make([]banktypes.Balance, 0, len(bals))

	for _, bal := range bals {
		addrNames[bal.Addr.String()] = string(bal.Addr)
		if bal.Coins.IsZero() {
			continue
		}

		suite.mockMintCoins(minterAcc)
		suite.Require().NoError(suite.bankKeeper.MintCoins(suite.ctx, authtypes.Minter, bal.Coins),
			"minting %s for %s", bal.Coins, string(bal.Addr))
		suite.mockSendCoinsFromModuleToAccount(minterAcc, bal.Addr)
		suite.Require().NoError(
			suite.bankKeeper.SendCoinsFromModuleToAccount(suite.ctx, authtypes.Minter, bal.Addr, bal.Coins),
			"sending freshly minted %s to %s", bal.Coins, string(bal.Addr),
		)

		expBals = append(expBals, banktypes.Balance{Address: bal.Addr.String(), Coins: bal.Coins})
	}

	expStrs := balsToStrings(expBals)
	suite.Require().NotEmpty(expStrs, "The expected balance strings should not be empty after setup.")

	var actBals []banktypes.Balance
	testFunc := func() {
		actBals = suite.bankKeeper.GetAccountsBalances(suite.ctx)
	}

	count := 1000
	for i := 1; i <= count; i++ {
		suite.Require().NotPanics(testFunc, "[%d/%d]: GetAccountsBalances", i, count)
		actStrs := balsToStrings(actBals)
		if !suite.Assert().Equal(expStrs, actStrs, "[%d/%d]: GetAccountsBalances result", i, count) && i == 1 {
			// If it fails on the first one, stop now since that probably means it'll always fail.
			// If it's not the first, keep going so it's easier to see that it's not deterministic.
			break
		}
		for b, actBal := range actBals {
			actBalSorted := copyAndSortCoins(actBal.Coins)
			suite.Assert().Equal(actBalSorted, actBal.Coins,
				"[%d/%d]: Balance[%d] is not sorted.\nexpected: %s\nactual  : %s",
				i, count, b, actBalSorted, actBal.Coins)
		}
	}
}

// copyAndSortCoins returns a copy of the provided coins slice and sorts it.
func copyAndSortCoins(coins sdk.Coins) sdk.Coins {
	if coins == nil {
		return nil
	}
	rv := make(sdk.Coins, len(coins))
	copy(rv, coins)
	return rv.Sort()
}

type mockSubspace struct {
	ps banktypes.Params
}

func (ms mockSubspace) GetParamSet(ctx sdk.Context, ps exported.ParamSet) {
	*ps.(*banktypes.Params) = ms.ps
}

func (ms mockSubspace) Get(ctx sdk.Context, key []byte, ptr any) {}

func (suite *KeeperTestSuite) TestMigrator_Migrate3to4() {
	bankKeeper := suite.bankKeeper
	ctx := sdk.UnwrapSDKContext(suite.ctx)
	require := suite.Require()

	for _, def := range []bool{true, false} {
		params := banktypes.Params{DefaultSendEnabled: def}
		require.NoError(bankKeeper.SetParams(ctx, params))

		suite.T().Run(fmt.Sprintf("default %t does not change", def), func(t *testing.T) {
			legacySubspace := func(ps banktypes.Params) mockSubspace {
				return mockSubspace{ps: ps}
			}(banktypes.NewParams(def))

			migrator := keeper.NewMigrator(bankKeeper, legacySubspace)
			require.NoError(migrator.Migrate3to4(ctx))

			actual := bankKeeper.GetParams(ctx)
			require.Equal(params.DefaultSendEnabled, actual.DefaultSendEnabled)
		})
	}

	for _, def := range []bool{true, false} {
		params := banktypes.Params{
			SendEnabled: []*banktypes.SendEnabled{
				{Denom: fmt.Sprintf("truecoin%t", def), Enabled: true},
				{Denom: fmt.Sprintf("falsecoin%t", def), Enabled: false},
			},
		}

		require.NoError(bankKeeper.SetParams(ctx, params))

		suite.T().Run(fmt.Sprintf("default %t send enabled info moved to store", def), func(t *testing.T) {
			legacySubspace := func(ps banktypes.Params) mockSubspace {
				return mockSubspace{ps: ps}
			}(banktypes.NewParams(def))

			migrator := keeper.NewMigrator(bankKeeper, legacySubspace)
			require.NoError(migrator.Migrate3to4(ctx))

			newParams := bankKeeper.GetParams(ctx)
			require.Len(newParams.SendEnabled, 0) //nolint // just using this for testing when migrating legacy
			require.Equal(def, newParams.DefaultSendEnabled)

			for _, se := range params.SendEnabled {
				actual := bankKeeper.IsSendEnabledDenom(ctx, se.Denom)
				require.Equal(se.Enabled, actual, se.Denom)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestSetParams() {
	ctx, bankKeeper := suite.ctx, suite.bankKeeper
	require := suite.Require()

	params := banktypes.NewParams(true)
	params.SendEnabled = []*banktypes.SendEnabled{
		{Denom: "paramscointrue", Enabled: true},
		{Denom: "paramscoinfalse", Enabled: false},
	}
	require.NoError(bankKeeper.SetParams(ctx, params))

	suite.Run("stored params are as expected", func() {
		actual := bankKeeper.GetParams(ctx)
		require.True(actual.DefaultSendEnabled, "DefaultSendEnabled")
		require.Len(actual.SendEnabled, 0, "SendEnabled") //nolint:staticcheck // test legacy deprecated param
	})

	suite.Run("send enabled params converted to store", func() {
		actual := bankKeeper.GetAllSendEnabledEntries(ctx)
		if suite.Assert().Len(actual, 2) {
			require.Equal("paramscoinfalse", actual[0].Denom, "actual[0].Denom")
			require.False(actual[0].Enabled, "actual[0].Enabled")
			require.Equal("paramscointrue", actual[1].Denom, "actual[1].Denom")
			require.True(actual[1].Enabled, "actual[1].Enabled")
		}
	})
}
