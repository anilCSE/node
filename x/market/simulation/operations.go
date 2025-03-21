package simulation

import (
	"errors"
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/simapp/helpers"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	simappparams "github.com/cosmos/cosmos-sdk/simapp/params"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	types "github.com/akash-network/akash-api/go/node/market/v1beta3"

	appparams "github.com/akash-network/node/app/params"
	testsim "github.com/akash-network/node/testutil/sim"
	keepers "github.com/akash-network/node/x/market/handler"
)

// Simulation operation weights constants
const (
	OpWeightMsgCreateBid  = "op_weight_msg_create_bid"  // nolint gosec
	OpWeightMsgCloseBid   = "op_weight_msg_close_bid"   // nolint gosec
	OpWeightMsgCloseLease = "op_weight_msg_close_lease" // nolint gosec
)

// WeightedOperations returns all the operations from the module with their respective weights
func WeightedOperations(
	appParams simtypes.AppParams, cdc codec.JSONCodec, ak govtypes.AccountKeeper,
	ks keepers.Keepers) simulation.WeightedOperations {
	var (
		weightMsgCreateBid  int
		weightMsgCloseBid   int
		weightMsgCloseLease int
	)

	appParams.GetOrGenerate(
		cdc, OpWeightMsgCreateBid, &weightMsgCreateBid, nil, func(r *rand.Rand) {
			weightMsgCreateBid = appparams.DefaultWeightMsgCreateBid
		},
	)

	appParams.GetOrGenerate(
		cdc, OpWeightMsgCloseBid, &weightMsgCloseBid, nil, func(r *rand.Rand) {
			weightMsgCloseBid = appparams.DefaultWeightMsgCloseBid
		},
	)

	appParams.GetOrGenerate(
		cdc, OpWeightMsgCloseLease, &weightMsgCloseLease, nil, func(r *rand.Rand) {
			weightMsgCloseLease = appparams.DefaultWeightMsgCloseLease
		},
	)

	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(
			weightMsgCreateBid,
			SimulateMsgCreateBid(ak, ks),
		),
		simulation.NewWeightedOperation(
			weightMsgCloseBid,
			SimulateMsgCloseBid(ak, ks),
		),
		simulation.NewWeightedOperation(
			weightMsgCloseLease,
			SimulateMsgCloseLease(ak, ks),
		),
	}
}

// SimulateMsgCreateBid generates a MsgCreateBid with random values
func SimulateMsgCreateBid(ak govtypes.AccountKeeper, ks keepers.Keepers) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accounts []simtypes.Account,
		chainID string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		orders := getOrdersWithState(ctx, ks, types.OrderOpen)
		if len(orders) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCreateBid, "no open orders found"), nil, nil
		}

		// Get random order
		order := orders[testsim.RandIdx(r, len(orders)-1)]

		providers := getProviders(ctx, ks)

		if len(providers) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCreateBid, "no providers found"), nil, nil
		}

		// Get random deployment
		provider := providers[testsim.RandIdx(r, len(providers)-1)]

		ownerAddr, convertErr := sdk.AccAddressFromBech32(provider.Owner)
		if convertErr != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCreateBid, "error while converting address"), nil, convertErr
		}

		simAccount, found := simtypes.FindAccount(accounts, ownerAddr)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCreateBid, "unable to find provider"),
				nil, fmt.Errorf("provider with %s not found", provider.Owner)
		}

		if provider.Owner == order.ID().Owner {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCreateBid, "provider and order owner cannot be same"),
				nil, nil
		}

		depositAmount := minDeposit
		account := ak.GetAccount(ctx, simAccount.Address)
		spendable := ks.Bank.SpendableCoins(ctx, account.GetAddress())

		if spendable.AmountOf(depositAmount.Denom).LT(depositAmount.Amount.MulRaw(2)) {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCreateBid, "out of money"), nil, nil
		}
		spendable = spendable.Sub(sdk.NewCoins(depositAmount))

		fees, err := simtypes.RandomFees(r, ctx, spendable)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCreateBid, "unable to generate fees"), nil, err
		}

		msg := types.NewMsgCreateBid(order.OrderID, simAccount.Address, order.Price(), depositAmount)

		txGen := simappparams.MakeTestEncodingConfig().TxConfig
		tx, err := helpers.GenTx(
			txGen,
			[]sdk.Msg{msg},
			fees,
			helpers.DefaultGenTxGas,
			chainID,
			[]uint64{account.GetAccountNumber()},
			[]uint64{account.GetSequence()},
			simAccount.PrivKey,
		)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to generate mock tx"), nil, err
		}

		_, _, err = app.Deliver(txGen.TxEncoder(), tx)
		switch {
		case err == nil:
			return simtypes.NewOperationMsg(msg, true, "", nil), nil, nil
		case errors.Is(err, types.ErrBidExists):
			return simtypes.NewOperationMsg(msg, false, "", nil), nil, nil
		default:
			return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to deliver mock tx"), nil, err
		}

	}
}

// SimulateMsgCloseBid generates a MsgCloseBid with random values
func SimulateMsgCloseBid(ak govtypes.AccountKeeper, ks keepers.Keepers) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accounts []simtypes.Account,
		chainID string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var bids []types.Bid

		ks.Market.WithBids(ctx, func(bid types.Bid) bool {
			if bid.State == types.BidActive {
				lease, ok := ks.Market.GetLease(ctx, types.LeaseID(bid.BidID))
				if ok && lease.State == types.LeaseActive {
					bids = append(bids, bid)
				}
			}

			return false
		})

		if len(bids) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCloseBid, "no matched bids found"), nil, nil
		}

		// Get random bid
		bid := bids[testsim.RandIdx(r, len(bids)-1)]

		providerAddr, convertErr := sdk.AccAddressFromBech32(bid.ID().Provider)
		if convertErr != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCloseBid, "error while converting address"), nil, convertErr
		}

		simAccount, found := simtypes.FindAccount(accounts, providerAddr)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCloseBid, "unable to find bid with provider"),
				nil, fmt.Errorf("bid with %s not found", bid.ID().Provider)
		}

		account := ak.GetAccount(ctx, simAccount.Address)
		spendable := ks.Bank.SpendableCoins(ctx, account.GetAddress())

		fees, err := simtypes.RandomFees(r, ctx, spendable)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCloseBid, "unable to generate fees"), nil, err
		}

		msg := types.NewMsgCloseBid(bid.BidID)

		txGen := simappparams.MakeTestEncodingConfig().TxConfig
		tx, err := helpers.GenTx(
			txGen,
			[]sdk.Msg{msg},
			fees,
			helpers.DefaultGenTxGas,
			chainID,
			[]uint64{account.GetAccountNumber()},
			[]uint64{account.GetSequence()},
			simAccount.PrivKey,
		)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to generate mock tx"), nil, err
		}

		_, _, err = app.Deliver(txGen.TxEncoder(), tx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to deliver tx"), nil, err
		}

		return simtypes.NewOperationMsg(msg, true, "", nil), nil, nil
	}
}

// SimulateMsgCloseLease generates a MsgCloseLease with random values
func SimulateMsgCloseLease(ak govtypes.AccountKeeper, ks keepers.Keepers) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accounts []simtypes.Account,
		chainID string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// orders := getOrdersWithState(ctx, ks, types.OrderActive)
		// if len(orders) == 0 {
		// 	return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCloseLease, "no orders with state matched found"), nil, nil
		// }

		// // Get random order
		// order := orders[testsim.RandIdx(r, len(orders) - 1)]

		// ownerAddr, convertErr := sdk.AccAddressFromBech32(order.ID().Owner)
		// if convertErr != nil {
		// 	return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCloseLease, "error while converting address"), nil, convertErr
		// }

		// simAccount, found := simtypes.FindAccount(accounts, ownerAddr)
		// if !found {
		// 	return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCloseLease, "unable to find order"),
		// 		nil, errors.Errorf("order with %s not found", order.ID().Owner)
		// }

		// account := ak.GetAccount(ctx, simAccount.Address)
		// spendable := ks.Bank.SpendableCoins(ctx, account.GetAddress())

		// fees, err := simtypes.RandomFees(r, ctx, spendable)
		// if err != nil {
		// 	return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCloseBid, "unable to generate fees"), nil, err
		// }

		// msg := types.NewMsgCloseLease(order.OrderID)

		// txGen := simappparams.MakeTestEncodingConfig().TxConfig
		// tx, err := helpers.GenTx(
		// 	txGen,
		// 	[]sdk.Msg{msg},
		// 	fees,
		// 	helpers.DefaultGenTxGas,
		// 	chainID,
		// 	[]uint64{account.GetAccountNumber()},
		// 	[]uint64{account.GetSequence()},
		// 	simAccount.PrivKey,
		// )
		// if err != nil {
		// 	return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to generate mock tx"), nil, err
		// }

		// _, _, err = app.Deliver(txGen.TxEncoder(), tx)
		// if err != nil {
		// 	return simtypes.NoOpMsg(types.ModuleName, msg.Type(), "unable to deliver tx"), nil, err
		// }

		return simtypes.NoOpMsg(types.ModuleName, types.MsgTypeCloseLease, "skipping"), nil, nil
	}
}
