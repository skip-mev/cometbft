package valp2p

import (
	"time"

	tmp2p "github.com/cometbft/cometbft/api/cometbft/p2p/v1"
	"github.com/cometbft/cometbft/internal/rand"
	"github.com/cometbft/cometbft/p2p"
	na "github.com/cometbft/cometbft/p2p/netaddr"
	"github.com/cometbft/cometbft/p2p/nodeinfo"
	"github.com/cometbft/cometbft/p2p/nodekey"
	tcpconn "github.com/cometbft/cometbft/p2p/transport/tcp/conn"
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

	valAddressBook map[nodekey.ID]na.NetAddr

	rng *rand.Rand
}

func NewValp2pReactor(sw *p2p.Switch, isValidator bool, valPeerCountLow int, valPeerCountHigh int, valPeerCountTarget int) *Valp2pReactor {
	r := &Valp2pReactor{
		sw:                 sw,
		isValidator:        isValidator,
		valPeerCountLow:    valPeerCountLow,
		valPeerCountHigh:   valPeerCountHigh,
		valPeerCountTarget: valPeerCountTarget,
		valAddressBook:     make(map[nodekey.ID]na.NetAddr),
		rng:                rand.NewRand(),
	}
	r.BaseReactor = *p2p.NewBaseReactor("Valp2p", r)
	return r
}

func (r *Valp2pReactor) StreamDescriptors() []p2p.StreamDescriptor {
	return []p2p.StreamDescriptor{
		&tcpconn.ChannelDescriptor{
			ID:                  Valp2pChannel,
			Priority:            1,
			SendQueueCapacity:   100,
			RecvMessageCapacity: maxMsgSize,
			MessageTypeI:        &tmp2p.Valp2PMessage{},
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

			if len(r.valAddressBook) < r.valPeerCountHigh {
				// send a valP2pRequest to a random peer
				r.Logger.Info("Sending valP2pRequest to a random peer")
				peer := r.sw.Peers().Random()
				peer.Send(p2p.Envelope{
					ChannelID: Valp2pChannel,
					Message:   &tmp2p.Valp2PRequest{},
				})
			}

			if r.sw.NumValPeers() < r.valPeerCountTarget && len(r.valAddressBook) > 0 {
				// dial a random validator
				r.Logger.Info("Dialing a random validator")
				keys := make([]nodekey.ID, 0, len(r.valAddressBook))
				for key := range r.valAddressBook {
					keys = append(keys, key)
				}
				idx := r.rng.Intn(len(keys))
				addr := r.valAddressBook[keys[idx]]
				r.sw.DialValidatorWithAddress(&addr)
			}
		}
	}()

	go func() {
		for {
			time.Sleep(1 * time.Minute)

			for k := range r.valAddressBook {
				if peer := r.sw.Peers().Get(k); peer != nil && !peer.NodeInfo().(nodeinfo.Default).IsValidator {
					r.Logger.Info("Removing non-validator from address book", "addr", k)
					delete(r.valAddressBook, k)
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

	if !peer.NodeInfo().(nodeinfo.Default).IsValidator {
		r.Logger.Info("Not broadcasting val info for non-validator", "peer", peer)
		return
	}

	r.Logger.Info("Adding val", "val", peer)
	r.valAddressBook[peer.SocketAddr().ID] = *peer.SocketAddr()

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
		if _, ok := r.valAddressBook[nodekey.ID(msg.Addr.ID)]; ok {
			return
		}

		addr, err := na.NewFromProto(msg.Addr)
		if err != nil {
			r.Logger.Error("failed to parse val address", "err", err)
			return
		}
		r.valAddressBook[nodekey.ID(msg.Addr.ID)] = *addr
		r.Logger.Info("Added val", "addr", addr)

		r.sw.Broadcast(p2p.Envelope{
			ChannelID: Valp2pChannel,
			Message:   msg,
		})
	case *tmp2p.Valp2PRequest:
		r.Logger.Info("Received val request", "msg", msg)
		addrs := make([]tmp2p.NetAddress, 0, len(r.valAddressBook))
		for _, addr := range r.valAddressBook {
			addrs = append(addrs, addr.ToProto())
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
			na, err := na.NewFromProto(addr)
			if err != nil {
				r.Logger.Error("failed to parse val address", "err", err)
				continue
			}
			r.valAddressBook[nodekey.ID(addr.ID)] = *na
			r.Logger.Info("Added val", "addr", na)
		}
	}
}
