package core

import (
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// txNoncer is a tiny virtual state database to manage the executable nonces of
// accounts in the pool, falling back to reading from a real state database if
// an account is unknown.
type txNonceCache struct {
	cache          map[common.Address]uint64
	nonces         map[common.Address]uint64
	lock           sync.Mutex
	number         *big.Int
	request        chan NonceRequest
	balances       map[common.Address]*big.Int
	requestBalance chan BalanceRequest
	balanceLocker  sync.Mutex
	nonceLocker    sync.Mutex
}

type BalanceRequest struct {
	addr common.Address
	ch   chan *big.Int
}
type NonceRequest struct {
	addr common.Address
	ch   chan uint64
}

// newTxNoncer creates a new virtual state database to track the pool nonces.
func newTxNonceCache(signer types.Signer, block *types.Block) *txNonceCache {
	cache := make(map[common.Address]uint64)
	for _, tx := range block.Transactions() {
		from, _ := Sender(signer, tx)
		cache[from] = tx.Nonce() + 1
	}
	obj := &txNonceCache{
		cache:    cache,
		nonces:   make(map[common.Address]uint64),
		number:   big.NewInt(0).Set(block.Number()),
		balances: make(map[common.Address]*big.Int),
	}
	return obj
}
func Sender(signer types.Signer, tx *types.Transaction) (common.Address, error) {
	addr, err := signer.Sender(tx)
	if err != nil {
		return common.Address{}, err
	}
	return addr, nil
}

// get returns the current nonce of an account, falling back to a real state
// database if the account is unknown.
func (txn *txNonceCache) get(addr common.Address) uint64 {
	// We use mutex for get operation is the underlying
	// state will mutate db even for read access.
	txn.lock.Lock()
	defer txn.lock.Unlock()
	if _, ok := txn.nonces[addr]; !ok {
		txn.nonces[addr] = txn.cache[addr]
	}
	return txn.nonces[addr]
}

// set inserts a new virtual nonce into the virtual state database to be returned
// whenever the pool requests it instead of reaching into the real state database.
func (txn *txNonceCache) set(addr common.Address, nonce uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()

	txn.nonces[addr] = nonce
}

// setIfLower updates a new virtual nonce into the virtual state database if the
// the new one is lower.
func (txn *txNonceCache) setIfLower(addr common.Address, nonce uint64) {
	txn.lock.Lock()
	defer txn.lock.Unlock()

	if _, ok := txn.nonces[addr]; !ok {
		txn.nonces[addr] = txn.cache[addr]
	}
	if txn.nonces[addr] <= nonce {
		return
	}
	txn.nonces[addr] = nonce
}

func (txn *txNonceCache) getNonceFromCache(addr common.Address) uint64 {
	txn.nonceLocker.Lock()
	defer txn.nonceLocker.Unlock()
	return txn.cache[addr]
}

// 有点变态
func (txn *txNonceCache) setNonceFromCache(addr common.Address, nonce uint64) {
	txn.nonceLocker.Lock()
	defer txn.nonceLocker.Unlock()
	if _, ok := txn.cache[addr]; !ok {
		txn.cache[addr] = nonce
	} else {

	}
}
func (txn *txNonceCache) reset(signer types.Signer, block *types.Block) {
	//实时nonce
	txn.nonces = make(map[common.Address]uint64)
	if block != nil {
		//新块优秀
		if block.Number().Cmp(txn.number) == 1 {
			//更新nonce
			for _, tx := range block.Transactions() {
				from, _ := Sender(signer, tx)
				if _, ok := txn.cache[from]; !ok {
					txn.cache[from] = tx.Nonce() + 1
				} else {
					if tx.Nonce() >= txn.cache[from] {
						txn.cache[from] = tx.Nonce() + 1
					}
				}
			}
			//更新高度
			txn.number.Set(block.Number())
		}
	}
}

var baseBalance *big.Int = big.NewInt(1e+18)

func (txn *txNonceCache) GetBalance(addr common.Address) *big.Int {
	txn.balanceLocker.Lock()
	defer txn.balanceLocker.Unlock()
	if _, ok := txn.balances[addr]; !ok {
		txn.balances[addr] = baseBalance
	}
	return txn.balances[addr]
}
