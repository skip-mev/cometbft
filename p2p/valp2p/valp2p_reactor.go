package valp2p

import (
	"sync"
	"time"

	"github.com/cometbft/cometbft/libs/rand"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/p2p/conn"
	tmp2p "github.com/cometbft/cometbft/proto/tendermint/p2p"
)

const (
	// Valp2pChannel is the channel for valp2p messages
	Valp2pChannel = byte(0x94)

	// max addresses returned by GetSelection
	// NOTE: this must match "maxMsgSize".
	maxGetSelection = 250

	// over-estimate of max na.NetAddr size
	// hexID (40) + IP (16) + Port (2) + Name (100) ...
	// NOTE: dont use massive DNS name ..
	maxAddressSize = 256

	// NOTE: amplificaiton factor!
	// small request results in up to maxMsgSize response.
	maxMsgSize = maxAddressSize * maxGetSelection
)

type Valp2pReactor struct {
	p2p.BaseReactor
	sw *p2p.Switch

	isValidator bool

	valPeerCountLow    int
	valPeerCountHigh   int
	valPeerCountTarget int

	valAddressBook *valAddressBook

	rng *rand.Rand
}

type valAddressBook struct {
	mu sync.RWMutex
	m  map[p2p.ID]p2p.NetAddress
}

func (sm *valAddressBook) Load(key p2p.ID) (p2p.NetAddress, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	val, ok := sm.m[key]
	return val, ok
}

func (sm *valAddressBook) Store(key p2p.ID, value p2p.NetAddress) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.m[key] = value
}

func (sm *valAddressBook) Len() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.m)
}

func (sm *valAddressBook) Keys() []p2p.ID {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	keys := make([]p2p.ID, 0, len(sm.m))
	for key := range sm.m {
		keys = append(keys, key)
	}
	return keys
}

func (sm *valAddressBook) Delete(key p2p.ID) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.m, key)
}

func NewValp2pReactor(sw *p2p.Switch, isValidator bool, valPeerCountLow int, valPeerCountHigh int, valPeerCountTarget int) *Valp2pReactor {
	r := &Valp2pReactor{
		sw:                 sw,
		isValidator:        isValidator,
		valPeerCountLow:    valPeerCountLow,
		valPeerCountHigh:   valPeerCountHigh,
		valPeerCountTarget: valPeerCountTarget,
		valAddressBook:     &valAddressBook{m: make(map[p2p.ID]p2p.NetAddress)},
		rng:                rand.NewRand(),
	}
	r.BaseReactor = *p2p.NewBaseReactor("Valp2p", r)
	return r
}

func (r *Valp2pReactor) GetChannels() []*conn.ChannelDescriptor {
	return []*conn.ChannelDescriptor{
		{
			ID:                  Valp2pChannel,
			Priority:            1,
			SendQueueCapacity:   100,
			RecvMessageCapacity: maxMsgSize,
			MessageType:         &tmp2p.Valp2PMessage{},
		},
	}
}

func (r *Valp2pReactor) OnStart() error {
	r.Logger.Info("Starting valp2p reactor")

	// Start a goroutine that dials validator peers every 30 seconds
	go func() {
		// TODO(wllmshao): this is very crude right now, consider adjusting the parameters to make more sense
		for {
			time.Sleep(30 * time.Second)

			// Send a valP2pRequest to a random peer if we don't have enough validators in the address book
			if r.isValidator && r.valAddressBook.Len() < r.valPeerCountHigh {
				// send a valP2pRequest to a random peer
				r.Logger.Info("Sending valP2pRequest to a random peer")
				peer := r.sw.Peers().List()[r.rng.Intn(len(r.sw.Peers().List()))]
				if peer != nil {
					peer.Send(p2p.Envelope{
						ChannelID: Valp2pChannel,
						Message:   &tmp2p.Valp2PRequest{},
					})
				}
			}

			// Dial a random validator if we don't have enough validator peers
			if r.isValidator && r.sw.NumValPeers() < r.valPeerCountTarget && r.valAddressBook.Len() > 0 {
				// dial a random validator
				r.Logger.Info("Dialing a random validator")
				keys := make([]p2p.ID, 0, r.valAddressBook.Len())
				for key := range r.valAddressBook.m {
					keys = append(keys, key)
				}
				idx := r.rng.Intn(len(keys))
				addr, ok := r.valAddressBook.Load(keys[idx])
				if ok {
					r.sw.DialValidatorWithAddress(&addr)
				}
			}

			// Remove non-validator peers from the address book
			for _, key := range r.valAddressBook.Keys() {
				if peer := r.sw.Peers().Get(key); peer == nil || !peer.NodeInfo().(p2p.DefaultNodeInfo).IsValidator {
					r.Logger.Info("Removing non-validator from address book", "addr", key)
					r.valAddressBook.Delete(key)
				}
			}
		}
	}()

	return nil
}

func (r *Valp2pReactor) OnStop() {}

func (r *Valp2pReactor) AddPeer(peer p2p.Peer) {
	if !r.IsRunning() {
		return
	}

	if !peer.NodeInfo().(p2p.DefaultNodeInfo).IsValidator {
		r.Logger.Info("Not broadcasting val info for non-validator", "peer", peer)
		return
	}

	r.Logger.Info("Adding val", "val", peer)
	r.valAddressBook.Store(peer.SocketAddr().ID, *peer.SocketAddr())

	// Broadcast peer info to all other peers
	addr := &tmp2p.Valp2PAddr{
		Addr: peer.SocketAddr().ToProto(),
	}

	r.Logger.Info("Broadcasting val info", "addr", addr)

	r.sw.Broadcast(p2p.Envelope{
		ChannelID: Valp2pChannel,
		Message:   addr,
	})
}

func (r *Valp2pReactor) Receive(e p2p.Envelope) {
	if !r.IsRunning() {
		return
	}

	r.Logger.Info("Received message", "src", e.Src, "chId", e.ChannelID, "msg", e.Message)

	switch msg := e.Message.(type) {
	case *tmp2p.Valp2PAddr:
		r.Logger.Info("Received val info", "addr", msg.Addr)
		if _, ok := r.valAddressBook.Load(p2p.ID(msg.Addr.ID)); ok {
			return
		}

		addr, err := p2p.NetAddressFromProto(msg.Addr)
		if err != nil {
			r.Logger.Error("failed to parse val address", "err", err)
			return
		}
		r.valAddressBook.Store(p2p.ID(msg.Addr.ID), *addr)
		r.Logger.Info("Added val", "addr", addr)

		r.sw.Broadcast(p2p.Envelope{
			ChannelID: Valp2pChannel,
			Message:   msg,
		})
	case *tmp2p.Valp2PRequest:
		r.Logger.Info("Received val request", "msg", msg)
		addrs := make([]tmp2p.NetAddress, 0, r.valAddressBook.Len())
		for _, addr := range r.valAddressBook.Keys() {
			na, ok := r.valAddressBook.Load(addr)
			if ok {
				addrs = append(addrs, na.ToProto())
			}
		}

		e.Src.Send(p2p.Envelope{
			ChannelID: Valp2pChannel,
			Message: &tmp2p.Valp2PResponse{
				Addrs: addrs,
			},
		})
	case *tmp2p.Valp2PResponse:
		r.Logger.Info("Received val response", "msg", msg)
		for _, addr := range msg.Addrs {
			na, err := p2p.NetAddressFromProto(addr)
			if err != nil {
				r.Logger.Error("failed to parse val address", "err", err)
				continue
			}
			r.valAddressBook.Store(p2p.ID(addr.ID), *na)
			r.Logger.Info("Added val", "addr", na)
		}
	}
}
