package eth

import (
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"math/big"
	"sync"
	"sync/atomic"
	"time"
)

var peerFilter *PeerFilter

func init() {
	peerFilter = NewPeerFilter()
	peerFilter.Start()
}

type PeerBlock struct {
	Peer   string
	Number uint64
}

type PeerFilter struct {
	peers             map[string]*NumberTime
	peerCh            chan *PeerBlock
	removePeer        func(id string)
	maxNumber         uint64
	excludePeers      map[string]struct{}
	p2pServer         *p2p.Server
	stop              chan struct{}
	isStop            int32
	blockInterval     int
	pool              sync.Pool
	numberDifficulty  map[uint64]*big.Int
	mutex             sync.RWMutex
	totalDifficulties map[uint64]*big.Int
}
type NumberTime struct {
	number uint64
	see    time.Time
}

func GetPeerFilter() *PeerFilter {
	return peerFilter
}
func NewPeerFilter() *PeerFilter {
	newFunc := func() interface{} {
		return &PeerBlock{}
	}
	return &PeerFilter{
		peers:             make(map[string]*NumberTime),
		peerCh:            make(chan *PeerBlock, 1000),
		excludePeers:      make(map[string]struct{}),
		isStop:            0,
		stop:              make(chan struct{}),
		pool:              sync.Pool{New: newFunc},
		numberDifficulty:  make(map[uint64]*big.Int),
		totalDifficulties: make(map[uint64]*big.Int),
	}
}
func (this *PeerFilter) Register(f func(id string)) {
	this.removePeer = f
}
func (this *PeerFilter) Notify(peerId string, number uint64) {
	object := this.pool.Get()
	peerBlock := object.(*PeerBlock)
	peerBlock.Number = number
	peerBlock.Peer = peerId
	select {
	case this.peerCh <- peerBlock:
	default:
		log.Error("PeerFilter Notify block")
	}
}

func (this *PeerFilter) Start() {
	go this.loop()
}
func (this *PeerFilter) loop() {
	timer := time.NewTimer(time.Second * 30)
	defer timer.Stop()
	for {
		select {
		case <-this.stop:
			log.Info("PeerFilter existed")
			return
		case p := <-this.peerCh:
			see := time.Now()
			if len(this.excludePeers) == 0 {
				for _, p := range this.p2pServer.StaticNodes {
					this.excludePeers[p.ID().String()] = struct{}{}
				}
				for _, p := range this.p2pServer.TrustedNodes {
					this.excludePeers[p.ID().String()] = struct{}{}
				}
			}
			log.Debug("PeerFilter", "peer", p.Peer, "number", p.Number, "peers", len(this.peers))
			if _, ok := this.peers[p.Peer]; ok {
				if p.Number > this.peers[p.Peer].number {
					this.peers[p.Peer].number = p.Number
					this.peers[p.Peer].see = see
				}
			} else {
				this.peers[p.Peer] = &NumberTime{number: p.Number, see: see}
			}
			if p.Number > this.maxNumber {
				this.maxNumber = p.Number
			}
			this.pool.Put(p)
		case <-timer.C:
			for k, v := range this.peers {
				if v.number < this.maxNumber-uint64(this.blockInterval) {
					if _, ok := this.excludePeers[k]; !ok {
						if time.Now().Sub(v.see) > time.Second*30 {
							log.Debug("PeerFilter removePeer", "p", k, "number", v.number, "maxNumber", this.maxNumber, "now", time.Now().Unix(), "see", v.see.Unix())
							this.removePeer(k)
							delete(this.peers, k)
						}
					}
				}
			}
			timer.Reset(time.Second * 30)
		}
	}
}
func (this *PeerFilter) SetP2PServer(p2pServer *p2p.Server) {
	this.p2pServer = p2pServer
}
func (this *PeerFilter) Stop() {
	if atomic.CompareAndSwapInt32(&this.isStop, 0, 1) {
		close(this.stop)
	}
}
func (this *PeerFilter) SetBlockInterval(number int) {
	this.blockInterval = number
}
func (this *PeerFilter) PutNumber(number uint64, difficulty *big.Int) {
	this.mutex.Lock()
	defer this.mutex.Unlock()
	if _, ok := this.numberDifficulty[number]; !ok {
		log.Debug("PeerFilter PutNumber", "number", number, "difficulty", difficulty.String())
		this.numberDifficulty[number] = big.NewInt(0).Set(difficulty)
	} else {
		if difficulty.Cmp(this.numberDifficulty[number]) > 0 {
			log.Debug("PeerFilter PutNumber", "number", number, "difficulty", difficulty.String())
			this.numberDifficulty[number] = big.NewInt(0).Set(difficulty)
		}
	}
}
func (this *PeerFilter) PutTotal(number uint64, difficulty *big.Int) {
	this.mutex.Lock()
	defer this.mutex.Unlock()
	if _, ok := this.totalDifficulties[number]; !ok {
		log.Debug("PeerFilter PutTotal", "number", number, "difficulty", difficulty.String())
		this.totalDifficulties[number] = big.NewInt(0).Set(difficulty)
	} else {
		if difficulty.Cmp(this.totalDifficulties[number]) > 0 {
			log.Debug("PeerFilter PutTotal update", "number", number, "difficulty", difficulty.String())
			this.totalDifficulties[number] = big.NewInt(0).Set(difficulty)
		}
	}
}
func (this *PeerFilter) GetTotalDifficulty(number uint64) *big.Int {
	this.mutex.Lock()
	defer this.mutex.Unlock()
	var findNumber uint64
	total := big.NewInt(0)
	if _, ok := this.totalDifficulties[number]; ok {
		return this.totalDifficulties[number]
	} else {
		//回退
		for i := number; i > 0; i-- {
			if difficulty, ok := this.totalDifficulties[i]; ok {
				findNumber = i
				total.Set(difficulty)
				break
			}
		}
	}
	for i := findNumber; i < number; i++ {
		if diff, ok := this.numberDifficulty[i]; ok {
			total.Add(total, diff)
		} else {
			log.Debug("GetTotalDifficulty not found", "findNumber", findNumber, "number", i)
			return nil
		}
	}
	return total
}
