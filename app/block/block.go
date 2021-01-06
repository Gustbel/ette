package block

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gammazero/workerpool"
	"github.com/gookit/color"
	cfg "github.com/itzmeanjan/ette/app/config"
	d "github.com/itzmeanjan/ette/app/data"
	"github.com/itzmeanjan/ette/app/db"
	"gorm.io/gorm"
)

// Fetching block content using blockHash
func fetchBlockByHash(client *ethclient.Client, hash common.Hash, number string, _db *gorm.DB, redis *d.RedisInfo, _status *d.StatusHolder) {
	block, err := client.BlockByHash(context.Background(), hash)
	if err != nil {
		// Pushing block number into Redis queue for retrying later
		pushBlockHashIntoRedisQueue(redis, number)

		log.Print(color.Red.Sprintf("[!] Failed to fetch block by hash [ block : %s] : %s", number, err.Error()))
		return
	}

	// Publishes block data to all listening parties
	// on `block` channel
	publishBlock := func() {
		if err := redis.Client.Publish(context.Background(), "block", &d.Block{
			Hash:                block.Hash().Hex(),
			Number:              block.NumberU64(),
			Time:                block.Time(),
			ParentHash:          block.ParentHash().Hex(),
			Difficulty:          block.Difficulty().String(),
			GasUsed:             block.GasUsed(),
			GasLimit:            block.GasLimit(),
			Nonce:               block.Nonce(),
			Miner:               block.Coinbase().Hex(),
			Size:                float64(block.Size()),
			TransactionRootHash: block.TxHash().Hex(),
			ReceiptRootHash:     block.ReceiptHash().Hex(),
		}).Err(); err != nil {
			log.Print(color.Red.Sprintf("[!] Failed to publish block %d in channel : %s", block.NumberU64(), err.Error()))
		}
	}

	// Controlling behaviour of ette depending upon value of `EtteMode`
	switch cfg.Get("EtteMode") {
	case "1":
		if !db.StoreBlock(_db, block, _status) {
			// Pushing block number into Redis queue for retrying later
			// because it failed to store block in database
			pushBlockHashIntoRedisQueue(redis, number)
			return
		}
	case "2":
		publishBlock()
	case "3":
		// Try completing task of publishing block data, first
		// then we'll attempt to store it, is that fails, we'll push it to retry queue
		publishBlock()

		if !db.StoreBlock(_db, block, _status) {
			// Pushing block number into Redis queue for retrying later
			// because it failed to store block in database
			pushBlockHashIntoRedisQueue(redis, number)
			return
		}
	}

	FetchBlockContent(client, block, _db, redis, true, _status)
}

// Fetching block content using block number
func fetchBlockByNumber(client *ethclient.Client, number uint64, _db *gorm.DB, redis *d.RedisInfo, _status *d.StatusHolder) {
	_num := big.NewInt(0)
	_num = _num.SetUint64(number)

	block, err := client.BlockByNumber(context.Background(), _num)
	if err != nil {
		// Pushing block number into Redis queue for retrying later
		pushBlockHashIntoRedisQueue(redis, fmt.Sprintf("%d", number))

		log.Print(color.Red.Sprintf("[!] Failed to fetch block by number [ block : %d ] : %s", number, err))
		return
	}

	// Either creates new entry or updates existing one
	if !db.StoreBlock(_db, block, _status) {
		// Pushing block number into Redis queue for retrying later
		pushBlockHashIntoRedisQueue(redis, fmt.Sprintf("%d", number))
		return
	}

	FetchBlockContent(client, block, _db, redis, false, _status)
}

// FetchBlockContent - Fetching all transactions in this block, along with their receipt
func FetchBlockContent(client *ethclient.Client, block *types.Block, _db *gorm.DB, redis *d.RedisInfo, publishable bool, _status *d.StatusHolder) {
	if block.Transactions().Len() == 0 {
		log.Print(color.Green.Sprintf("[+] Block %d with 0 tx(s)", block.NumberU64()))

		safeUpdationOfSyncState(_status)
		return
	}

	// Communication channel to be shared between multiple executing go routines
	// which are trying to fetch all tx(s) present in block, concurrently
	returnValChan := make(chan *db.PackedTransaction, runtime.NumCPU()*int(cfg.GetConcurrencyFactor()))

	// -- Tx processing starting
	// Creating job processor queue
	// which will process all tx(s), concurrently
	wp := workerpool.New(runtime.NumCPU() * int(cfg.GetConcurrencyFactor()))

	// Concurrently trying to process all tx(s) for this block, in hope of better performance
	for _, v := range block.Transactions() {

		// Concurrently trying to fetch multiple tx(s) present in block
		// and expecting their status result to be published on shared channel
		//
		// Which is being read 👇
		func(tx *types.Transaction) {
			wp.Submit(func() {

				FetchTransactionByHash(client,
					block,
					tx,
					_db,
					redis,
					publishable,
					_status,
					returnValChan)

			})
		}(v)

	}

	// Keeping track of how many of these tx fetchers succeded & how many of them failed
	result := d.ResultStatus{}
	// Data received from tx fetchers, to be stored here
	packedTxs := make([]*db.PackedTransaction, block.Transactions().Len())

	for v := range returnValChan {
		if v != nil {
			result.Success++
		} else {
			result.Failure++
		}

		// #-of tx fetchers completed their job till now
		//
		// Either successfully or failed some how
		total := int(result.Total())
		// Storing tx data received from just completed go routine
		packedTxs[total-1] = v

		// All go routines have completed their job
		if total == block.Transactions().Len() {
			break
		}
	}

	// Stopping job processor forcefully
	// because by this time all jobs have been completed
	//
	// Otherwise control flow will not be able to come here
	// it'll keep looping in 👆 loop, reading from channel
	wp.Stop()
	// -- Tx processing ending

	// When all tx(s) are successfully processed ( as they have informed us over go channel ),
	// we're happy to exit from this context, given that none of them failed
	if result.Failure == 0 {
		log.Print(color.Green.Sprintf("[+] Block %d with %d tx(s)", block.NumberU64(), len(block.Transactions())))

		safeUpdationOfSyncState(_status)
		return
	}

	// Pushing block number into Redis queue for retrying later
	// because it failed to complete some of its jobs 👆
	pushBlockHashIntoRedisQueue(redis, block.Number().String())
}

// Updating shared varible between worker go routines, denoting progress of
// `ette`, in terms of data syncing
func safeUpdationOfSyncState(status *d.StatusHolder) {
	status.IncrementBlocksProcessed()
}
