package block

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gookit/color"
	d "github.com/itzmeanjan/ette/app/data"
	"github.com/itzmeanjan/ette/app/db"
	"gorm.io/gorm"
)

// FetchBlockByHash - Fetching block content using blockHash
func FetchBlockByHash(client *ethclient.Client, hash common.Hash, number string, _db *gorm.DB, redis *d.RedisInfo, _status *d.StatusHolder, lock *ProcessQueueLock) {

	_number, err := strconv.ParseUint(number, 10, 64)
	if err != nil {
		log.Printf("[!] Failed to parse block number : %s\n", err.Error())
		return
	}

	// communication channel to be used between this worker
	// lock manager go routine
	comm := make(chan bool)

	// attempting to acquire lock, but failed in first chance
	if !lock.Acquire(&LockRequest{Block: _number, Communication: comm}) {

		log.Print(color.LightWhite.Sprintf("[!] Failed to acquire lock for processing block : %s, waiting", number))

		// waiting to learn when previous go routine which was processing same
		// block number, done with its job
		//
		// it can receive a no go for this block processing, then it's
		// not attempted anymore, rather it's retry queue manager's job now
		// to reprocess this block
		if !<-comm {

			// Pushing block number into Redis queue for retrying later
			PushBlockIntoRetryQueue(redis, number)

			log.Print(color.Red.Sprintf("[!] Failed to acquire lock for processing block : %s", number))
			return

		}

	}

	// letting lock manager know processing of this certain block number
	// done
	defer func() {

		lock.Release(comm)

	}()

	// Starting block processing at
	startingAt := time.Now().UTC()

	block, err := client.BlockByHash(context.Background(), hash)
	if err != nil {
		// Pushing block number into Redis queue for retrying later
		PushBlockIntoRetryQueue(redis, number)

		log.Print(color.Red.Sprintf("[!] Failed to fetch block by hash [ block : %s] : %s", number, err.Error()))
		return
	}

	ProcessBlockContent(client, block, _db, redis, true, _status, startingAt)

}

// FetchBlockByNumber - Fetching block content using block number
func FetchBlockByNumber(client *ethclient.Client, number uint64, _db *gorm.DB, redis *d.RedisInfo, publishable bool, _status *d.StatusHolder, lock *ProcessQueueLock) {

	// communication channel to be used between this worker
	// lock manager go routine
	comm := make(chan bool)

	// attempting to acquire lock, but failed in first chance
	if !lock.Acquire(&LockRequest{Block: number, Communication: comm}) {

		log.Print(color.LightWhite.Sprintf("[!] Failed to acquire lock for processing block : %d, waiting", number))

		// waiting to learn when previous go routine which was processing same
		// block number, done with its job
		//
		// it can receive a no go for this block processing, then it's
		// not attempted anymore, rather it's retry queue manager's job now
		// to reprocess this block
		if !<-comm {

			// Pushing block number into Redis queue for retrying later
			PushBlockIntoRetryQueue(redis, fmt.Sprintf("%d", number))

			log.Print(color.Red.Sprintf("[!] Failed to acquire lock for processing block : %d", number))
			return

		}

	}

	// letting lock manager know processing of this certain block number
	// done
	defer func() {

		lock.Release(comm)

	}()

	// Starting block processing at
	startingAt := time.Now().UTC()

	_num := big.NewInt(0)
	_num.SetUint64(number)

	block, err := client.BlockByNumber(context.Background(), _num)
	if err != nil {
		// Pushing block number into Redis queue for retrying later
		PushBlockIntoRetryQueue(redis, fmt.Sprintf("%d", number))

		log.Print(color.Red.Sprintf("[!] Failed to fetch block by number [ block : %d ] : %s", number, err))
		return
	}

	// If attempt to process block by number went successful
	// we can consider removing this block number's entry from
	// attempt count tracker table
	if ProcessBlockContent(client, block, _db, redis, publishable, _status, startingAt) {

		RemoveBlockFromAttemptCountTrackerTable(redis, fmt.Sprintf("%d", number))

	}

}

// FetchTransactionByHash - Fetching specific transaction related data, tries to publish data if required
// & lets listener go routine know about all tx, event data it collected while processing this tx,
// which will be attempted to be stored in database
func FetchTransactionByHash(client *ethclient.Client, block *types.Block, tx *types.Transaction, _db *gorm.DB, redis *d.RedisInfo, publishable bool, _status *d.StatusHolder, returnValChan chan *db.PackedTransaction) {

	receipt, err := client.TransactionReceipt(context.Background(), tx.Hash())
	if err != nil {
		log.Print(color.Red.Sprintf("[!] Failed to fetch tx receipt [ block : %d ] : %s", block.NumberU64(), err.Error()))

		// Passing nil, to denote, failed to fetch all tx data
		// from blockchain node
		returnValChan <- nil
		return
	}

	sender, err := client.TransactionSender(context.Background(), tx, block.Hash(), receipt.TransactionIndex)
	if err != nil {
		log.Print(color.Red.Sprintf("[!] Failed to fetch tx sender [ block : %d ] : %s", block.NumberU64(), err.Error()))

		// Passing nil, to denote, failed to fetch all tx data
		// from blockchain node
		returnValChan <- nil
		return
	}

	// Passing all tx related data to listener go routine
	// so that it can attempt to store whole block data
	// into database
	returnValChan <- BuildPackedTx(tx, sender, receipt)
}
