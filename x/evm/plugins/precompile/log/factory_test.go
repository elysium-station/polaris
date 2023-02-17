// SPDX-License-Identifier: BUSL-1.1
//
// Copyright (C) 2023, Berachain Foundation. All rights reserved.
// Use of this software is govered by the Business Source License included
// in the LICENSE file of this repository and at www.mariadb.com/bsl11.
//
// ANY USE OF THE LICENSED WORK IN VIOLATION OF THIS LICENSE WILL AUTOMATICALLY
// TERMINATE YOUR RIGHTS UNDER THIS LICENSE FOR THE CURRENT AND ALL OTHER
// VERSIONS OF THE LICENSED WORK.
//
// THIS LICENSE DOES NOT GRANT YOU ANY RIGHT IN ANY TRADEMARK OR LOGO OF
// LICENSOR OR ITS AFFILIATES (PROVIDED THAT YOU MAY USE A TRADEMARK OR LOGO OF
// LICENSOR AS EXPRESSLY REQUIRED BY THIS LICENSE).
//
// TO THE EXTENT PERMITTED BY APPLICABLE LAW, THE LICENSED WORK IS PROVIDED ON
// AN “AS IS” BASIS. LICENSOR HEREBY DISCLAIMS ALL WARRANTIES AND CONDITIONS,
// EXPRESS OR IMPLIED, INCLUDING (WITHOUT LIMITATION) WARRANTIES OF
// MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, NON-INFRINGEMENT, AND
// TITLE.

package log

import (
	"errors"
	"strconv"

	"github.com/berachain/stargazer/eth/common"
	"github.com/berachain/stargazer/eth/crypto"
	"github.com/berachain/stargazer/eth/types/abi"
	utils "github.com/berachain/stargazer/x/evm/utils"
	sdk "github.com/cosmos/cosmos-sdk/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Factory", func() {
	var f *Factory
	var valAddr sdk.ValAddress
	var delAddr sdk.AccAddress
	var amt sdk.Coin
	var creationHeight int64

	BeforeEach(func() {
		f = NewFactory()
		valAddr = sdk.ValAddress([]byte("alice"))
		delAddr = sdk.AccAddress([]byte("bob"))
		creationHeight = int64(10)
		amt = sdk.NewCoin("denom", sdk.NewInt(10))

		Expect(func() {
			f.RegisterEvent(common.BytesToAddress([]byte{0x01}), mockDefaultAbiEvent(), nil)
		}).ToNot(Panic())
		Expect(func() {
			f.RegisterEvent(common.BytesToAddress([]byte{0x02}), mockCustomAbiEvent(), cvd)
		}).ToNot(Panic())
	})

	When("building Eth logs", func() {
		It("should not build for an unregistered event", func() {
			event := sdk.NewEvent("unbonding_delegation")
			log, err := f.Build(&event)
			Expect(err.Error()).To(Equal("no Ethereum event was registered for this Cosmos event"))
			Expect(log).To(BeNil())
		})

		It("should error on invalid attributes", func() {
			event := sdk.NewEvent(
				"cancel_unbonding_delegation",
				sdk.NewAttribute("validator", valAddr.String()),
			)
			log, err := f.Build(&event)
			Expect(err.Error()).To(Equal("not enough event attributes provided"))
			Expect(log).To(BeNil())
		})

		It("should correctly build a log for valid event", func() {
			event := sdk.NewEvent(
				"cancel_unbonding_delegation",
				sdk.NewAttribute("validator", valAddr.String()),
				sdk.NewAttribute("amount", amt.String()),
				sdk.NewAttribute("creation_height", strconv.FormatInt(creationHeight, 10)),
				sdk.NewAttribute("delegator", delAddr.String()),
			)
			log, err := f.Build(&event)
			Expect(err).To(BeNil())
			Expect(log).ToNot(BeNil())
			Expect(log.Address).To(Equal(common.BytesToAddress([]byte{0x01})))
			Expect(log.Topics).To(HaveLen(3))
			Expect(log.Topics[0]).To(Equal(
				crypto.Keccak256Hash(
					[]byte("CancelUnbondingDelegation(address,address,uint256,int64)"),
				),
			))
			Expect(log.Topics[1]).To(Equal(common.BytesToHash(valAddr.Bytes())))
			Expect(log.Topics[2]).To(Equal(common.BytesToHash(delAddr.Bytes())))
			packedData, err := mockDefaultAbiEvent().Inputs.NonIndexed().Pack(
				amt.Amount.BigInt(), creationHeight,
			)
			Expect(err).To(BeNil())
			Expect(log.Data).To(Equal(packedData))
		})

		It("should correctly build a log for valid event with custom decoder", func() {
			event := sdk.NewEvent(
				"custom_unbonding_delegation",
				sdk.NewAttribute("custom_validator", valAddr.String()),
				sdk.NewAttribute("custom_amount", amt.String()),
			)
			log, err := f.Build(&event)
			Expect(err).To(BeNil())
			Expect(log).ToNot(BeNil())
			Expect(log.Address).To(Equal(common.BytesToAddress([]byte{0x02})))
			Expect(log.Topics).To(HaveLen(2))
			Expect(log.Topics[0]).To(Equal(
				crypto.Keccak256Hash(
					[]byte("CustomUnbondingDelegation(address,uint256)"),
				),
			))
			Expect(log.Topics[1]).To(Equal(common.BytesToHash(valAddr.Bytes())))
			packedData, err := mockCustomAbiEvent().Inputs.NonIndexed().Pack(
				amt.Amount.BigInt(),
			)
			Expect(err).To(BeNil())
			Expect(log.Data).To(Equal(packedData))
		})
	})

	When("building invalid Cosmos events", func() {
		It("should not find the custom value decoder", func() {
			f.RegisterEvent(common.BytesToAddress([]byte{0x03}), mockBadAbiEvent(), cvd)
			event := sdk.NewEvent(
				"custom_unbonding_delegation",
				sdk.NewAttribute("custom_validator", valAddr.String()),
				sdk.NewAttribute("custom_amount", amt.String()),
				sdk.NewAttribute("invalid_arg", amt.String()),
			)
			log, err := f.Build(&event)
			Expect(log).To(BeNil())
			Expect(err.Error()).To(Equal("no value decoder function is found for event attribute key: invalid_arg"))
		})

		It("should error on decoders returning errors", func() {
			badCvd := make(ValueDecoders)
			badCvd["custom_amount"] = func(val string) (any, error) {
				coin, err := sdk.ParseCoinNormalized(val)
				if err != nil {
					return nil, err
				}
				return coin.Amount.BigInt(), nil
			}
			badCvd["custom_validator"] = func(val string) (any, error) {
				return nil, errors.New("invalid validator address")
			}
			f.RegisterEvent(common.BytesToAddress([]byte{0x03}), mockCustomAbiEvent(), badCvd)
			event := sdk.NewEvent(
				"custom_unbonding_delegation",
				sdk.NewAttribute("custom_validator", valAddr.String()),
				sdk.NewAttribute("custom_amount", amt.String()),
			)
			log, err := f.Build(&event)
			Expect(log).To(BeNil())
			Expect(err.Error()).To(Equal("invalid validator address"))

			badCvd = make(ValueDecoders)
			badCvd["custom_validator"] = func(val string) (any, error) {
				valAddress, err2 := sdk.ValAddressFromBech32(val)
				if err2 != nil {
					return nil, err
				}
				return utils.ValAddressToEthAddress(valAddress), nil
			}
			badCvd["custom_amount"] = func(val string) (any, error) {
				return nil, errors.New("invalid amount")
			}
			f.RegisterEvent(common.BytesToAddress([]byte{0x03}), mockCustomAbiEvent(), badCvd)
			log, err = f.Build(&event)
			Expect(log).To(BeNil())
			Expect(err.Error()).To(Equal("invalid amount"))
		})

		It("should not find attribute key", func() {
			f.RegisterEvent(common.BytesToAddress([]byte{0x03}), mockCustomAbiEvent(), cvd)
			event := sdk.NewEvent(
				"custom_unbonding_delegation",
				sdk.NewAttribute("custom_validator", valAddr.String()),
				sdk.NewAttribute("custom_amount_bad", amt.String()),
			)
			log, err := f.Build(&event)
			Expect(log).To(BeNil())
			Expect(err.Error()).To(Equal("this Ethereum event argument has no matching Cosmos attribute key: customAmount"))

			event = sdk.NewEvent(
				"custom_unbonding_delegation",
				sdk.NewAttribute("custom_validator_bad", valAddr.String()),
				sdk.NewAttribute("custom_amount", amt.String()),
			)
			log, err = f.Build(&event)
			Expect(log).To(BeNil())
			Expect(err.Error()).To(Equal("this Ethereum event argument has no matching Cosmos attribute key: customValidator"))
		})
	})
})

// MOCKS BELOW.

func mockCustomAbiEvent() abi.Event {
	addrType, _ := abi.NewType("address", "address", nil)
	uint256Type, _ := abi.NewType("uint256", "uint256", nil)
	return abi.NewEvent(
		"CustomUnbondingDelegation",
		"CustomUnbondingDelegation",
		false,
		abi.Arguments{
			{
				Name:    "customValidator",
				Type:    addrType,
				Indexed: true,
			},
			{
				Name:    "customAmount",
				Type:    uint256Type,
				Indexed: false,
			},
		},
	)
}

var cvd = ValueDecoders{
	"custom_validator": func(val string) (any, error) {
		valAddress, err := sdk.ValAddressFromBech32(val)
		if err != nil {
			return nil, err
		}
		return utils.ValAddressToEthAddress(valAddress), nil
	},
	"custom_amount": func(val string) (any, error) {
		coin, err := sdk.ParseCoinNormalized(val)
		if err != nil {
			return nil, err
		}
		return coin.Amount.BigInt(), nil
	},
}

func mockBadAbiEvent() abi.Event {
	addrType, _ := abi.NewType("address", "address", nil)
	uint256Type, _ := abi.NewType("uint256", "uint256", nil)
	return abi.NewEvent(
		"CustomUnbondingDelegation",
		"CustomUnbondingDelegation",
		false,
		abi.Arguments{
			{
				Name:    "customValidator",
				Type:    addrType,
				Indexed: true,
			},
			{
				Name:    "customAmount",
				Type:    uint256Type,
				Indexed: false,
			},
			{
				Name:    "invalidArg",
				Type:    uint256Type,
				Indexed: false,
			},
		},
	)
}