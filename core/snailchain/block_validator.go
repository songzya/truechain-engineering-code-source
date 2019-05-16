// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package snailchain

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/truechain/truechain-engineering-code/core"

	"github.com/truechain/truechain-engineering-code/consensus"
	"github.com/truechain/truechain-engineering-code/core/types"
	"github.com/truechain/truechain-engineering-code/params"
	"math/big"
	time2 "time"
)

var (
	// ErrInvalidSignHash is returned if the fruit contains an invalid signatures hash.
	ErrInvalidSignHash = errors.New("invalid sign")

	//ErrInvalidFast is returned if the fastchain not have the hash
	ErrInvalidFast = errors.New("invalid fast hash")

	//ErrNoFruits is returned if the block not contains the exact fruit count
	ErrNoFruits = errors.New("invalid fruits count")

	//ErrInvalidFruits is returned if the fruits in block not continuity
	ErrInvalidFruits = errors.New("invalid fruits number")

	//ErrGapFruits is returned if the fruits's fastblock time gap less than 360s
	ErrGapFruits = errors.New("invalid fruits time gap")

	//ErrFruitTime is returned if the fruit's time less than fastblock's time
	ErrFruitTime = errors.New("invalid fruit time")

	//ErrBlockTime is returned if the block's time less than the last fruit's time
	ErrBlockTime = errors.New("invalid block time")
)

// BlockValidator is responsible for validating block headers, uncles and
// processed state.
//
// BlockValidator implements Validator.
type BlockValidator struct {
	config *params.ChainConfig // Chain configuration options
	bc     *SnailBlockChain    // Canonical block chain

	engine consensus.Engine // Consensus engine used for validating
	//election  consensus.CommitteeElection
	fastchain *core.BlockChain
}

// NewBlockValidator returns a new block validator which is safe for re-use
func NewBlockValidator(config *params.ChainConfig, fc *core.BlockChain, sc *SnailBlockChain, engine consensus.Engine) *BlockValidator {
	validator := &BlockValidator{
		config:    config,
		engine:    engine,
		fastchain: fc,
		bc:        sc,
	}
	return validator
}

//ValidateRewarded verify whether the block has been rewarded.
func (v *BlockValidator) ValidateRewarded(number uint64) error {
	if br := v.fastchain.GetBlockReward(number); br != nil {
		log.Info("err reward snail block", "number", number, "reward hash", br.SnailHash, "fast number", br.FastNumber, "fast hash", br.FastHash)
		return ErrRewardedBlock
	}
	return nil
}

//
// ValidateBody validates the given block's uncles and verifies the the block
// header's transaction and uncle roots. The headers are assumed to be already
// validated at this point.
func (v *BlockValidator) ValidateBody(block *types.SnailBlock) error {
	// Check whether the block's known, and if not, that it's linkable.
	if v.bc.IsCanonicalBlock(block.Hash(), block.NumberU64()) {
		return ErrKnownBlock
	}

	if !v.bc.IsCanonicalBlock(block.ParentHash(), block.NumberU64()-1) {
		if !v.bc.HasBlock(block.ParentHash(), block.NumberU64()-1) {
			return consensus.ErrUnknownAncestor
		}
		return consensus.ErrPrunedAncestor
	}
	// Header validity is known at this point, check the uncles and transactions
	header := block.Header()

	count := len(block.Fruits())
	if count == 0 {
		return ErrNoFruits
	}
	if count > params.MaximumFruits || count < params.MinimumFruits {
		return ErrNoFruits
	}

	temp := uint64(0)
	preBlock := v.bc.GetBlock(block.ParentHash(), block.NumberU64()-1)
	if preBlock == nil {
		log.Info("ValidateBody snail get parent block error", "block", block.Number(), "hash", block.Hash(), "parent", block.ParentHash())
		return consensus.ErrUnknownAncestor
	}
	if preBlock.Number().Cmp(common.Big0) > 0 {
		localFruits := preBlock.Fruits()
		temp = localFruits[len(localFruits)-1].FastNumber().Uint64()
	}
	fruits := block.Fruits()
	maxfb := v.fastchain.GetHeader(fruits[len(fruits)-1].FastHash(), fruits[len(fruits)-1].FastNumber().Uint64())
	minfb := v.fastchain.GetHeader(fruits[0].FastHash(), fruits[0].FastNumber().Uint64())
	if minfb == nil || maxfb == nil {
		return consensus.ErrFutureBlock
	}
	if fruits[len(fruits)-1].Time() == nil || block.Time() == nil || block.Time().Cmp(fruits[len(fruits)-1].Time()) < 0 {
		log.Info("validate time", "block.Time()", block.Time(), "fruits[len(fruits)-1].Time()", fruits[len(fruits)-1].Time())
		return ErrBlockTime
	}
	gap := new(big.Int).Sub(maxfb.Time, minfb.Time)
	if gap.Cmp(params.MinTimeGap) < 0 {
		log.Info("ValidateBody snail validate time gap error", "block", block.Number(), "first fb number", minfb.Number, "first fb time", minfb.Time, "last fb number", maxfb.Number, "last fb time", maxfb.Time, "tim gap", gap)
		return ErrGapFruits
	}
	start := time2.Now()
	for _, fruit := range fruits {
		if fruit.FastNumber().Uint64()-temp != 1 {
			log.Info("ValidateBody snail validate fruit error", "block", block.Number(), "first", fruits[0].FastNumber(), "count", len(fruits),
				"fruit", fruit.FastNumber(), "pre", temp)
			return ErrInvalidFruits
		}
		validate := time2.Now()
		if err := v.ValidateFruit(fruit, block, false); err != nil {
			log.Info("ValidateBody snail validate fruit error", "block", block.Number(), "fruit", fruit.FastNumber(), "hash", fruit.FastHash(), "err", err)
			return err
		}
		log.Info("next()", "ValidateFruit", time2.Since(validate))
		temp = fruit.FastNumber().Uint64()
	}
	log.Info("next()", "validateFruits", time2.Since(start), "len(fruits)", len(fruits))
	if hash := types.DeriveSha(types.Fruits(block.Fruits())); hash != header.FruitsHash {
		return fmt.Errorf("fruits hash mismatch: have %x, want %x", hash, header.FruitsHash)
	}

	log.Info("Validate new snail body", "block", block.Number(), "hash", block.Hash(), "fruits", header.FruitsHash, "first", fruits[0].FastNumber(), "count", len(fruits))
	return nil
}

// VerifySnailSeal checking whether the given block satisfies
// the PoW difficulty requirements.
func (v *BlockValidator) VerifySnailSeal(chain consensus.SnailChainReader, header *types.SnailHeader, isFruit bool) error {
	return v.engine.VerifySnailSeal(chain, header, true)
}

//ValidateFruit is to verify if the fruit is legal
func (v *BlockValidator) ValidateFruit(fruit, block *types.SnailBlock, canonical bool) error {
	//check number(fb)
	//
	currentNumber := v.fastchain.CurrentHeader().Number
	if fruit.FastNumber().Cmp(currentNumber) > 0 {
		log.Warn("ValidateFruit", "currentHeaderNumber", v.fastchain.CurrentHeader().Number, "currentBlockNumber", v.fastchain.CurrentBlock().Number())
		return consensus.ErrFutureBlock
	}

	fb := v.fastchain.GetHeader(fruit.FastHash(), fruit.FastNumber().Uint64())
	if fb == nil {
		return ErrInvalidFast
	}

	//check fruit's time
	if fruit.Time() == nil || fb.Time == nil || fruit.Time().Cmp(fb.Time) < 0 {
		return ErrFruitTime
	}

	//check integrity
	getSignHash := types.CalcSignHash(fruit.Signs())
	if fruit.Header().SignHash != getSignHash {
		log.Info("valid fruit sign hash failed.")
		return ErrInvalidSignHash
	}

	// check freshness
	var blockHeader *types.SnailHeader
	if block != nil {
		blockHeader = block.Header()
	}
	start := time2.Now()
	err := v.engine.VerifyFreshness(v.bc, fruit.Header(), blockHeader, canonical)
	if err != nil {
		log.Debug("ValidateFruit verify freshness error.", "err", err, "fruit", fruit.FastNumber())
		return err
	}
	log.Info("next()", "VerifyFreshness", time2.Since(start))
	start = time2.Now()
	header := fruit.Header()
	if err := v.engine.VerifySnailHeader(v.bc, v.fastchain, header, true, true); err != nil {
		log.Info("validate fruit verify failed.", "err", err)
		return err
	}
	log.Info("next()", "VerifySnailHeader", time2.Since(start))
	start = time2.Now()
	// validate the signatures of this fruit
	if err := v.engine.VerifySigns(fruit.FastNumber(), fruit.FastHash(), fruit.Signs()); err != nil {
		log.Info("validate fruit VerifySigns failed.", "err", err)
		return err
	}
	log.Info("next()", "VerifySigns", time2.Since(start))
	return nil
}
