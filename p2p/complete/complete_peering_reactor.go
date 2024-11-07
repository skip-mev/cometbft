package complete

import (
	tmp2p "github.com/cometbft/cometbft/api/cometbft/p2p/v1"
	"github.com/cometbft/cometbft/p2p"
	na "github.com/cometbft/cometbft/p2p/netaddr"
	"github.com/cometbft/cometbft/p2p/nodeinfo"
	tcpconn "github.com/cometbft/cometbft/p2p/transport/tcp/conn"
)

const (
	// PeeringChannel is the channel for peer discovery messages
	PeeringChannel = byte(0x93)

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

// CompletePeeringReactor handles peer discovery to create a complete network
type CompletePeeringReactor struct {
	p2p.BaseReactor
	sw *p2p.Switch
}

// NewCompletePeeringReactor creates a new reactor for complete peer networking
func NewCompletePeeringReactor(sw *p2p.Switch) *CompletePeeringReactor {
	r := &CompletePeeringReactor{
		sw: sw,
	}
	r.BaseReactor = *p2p.NewBaseReactor("CompletePeering", r)
	return r
}

// StreamDescriptors implements Reactor
func (r *CompletePeeringReactor) StreamDescriptors() []p2p.StreamDescriptor {
	return []p2p.StreamDescriptor{
		&tcpconn.ChannelDescriptor{
			ID:                  PeeringChannel,
			Priority:            1,
			SendQueueCapacity:   100,
			RecvMessageCapacity: maxMsgSize,
			MessageTypeI:        &tmp2p.Message{},
		},
	}
}

// OnStart implements BaseService
func (r *CompletePeeringReactor) OnStart() error {
	r.Logger.Info("Starting complete peering reactor")
	return nil
}

// OnStop implements BaseService
func (r *CompletePeeringReactor) OnStop() {}

// AddPeer implements Reactor by broadcasting new peer info
func (r *CompletePeeringReactor) AddPeer(peer p2p.Peer) {
	if !r.IsRunning() {
		return
	}

	if !peer.NodeInfo().(nodeinfo.Default).IsValidator {
		return
	}

	r.Logger.Info("Adding peer", "peer", peer)

	// Broadcast peer info to all other peers
	addr := &tmp2p.PexAddrs{
		Addrs: []tmp2p.NetAddress{
			peer.SocketAddr().ToProto(),
		},
	}

	r.Logger.Info("Broadcasting peer info", "addr", addr)

	r.sw.Broadcast(p2p.Envelope{
		ChannelID: PeeringChannel,
		Message:   addr,
	})
}

// Receive implements Reactor by handling peer info messages
func (r *CompletePeeringReactor) Receive(e p2p.Envelope) {
	if !r.IsRunning() {
		return
	}

	msg, ok := e.Message.(*tmp2p.PexAddrs)
	if !ok {
		r.Logger.Error("received invalid message", "msg", e.Message)
		return
	}

	addr, err := na.NewFromProto(msg.Addrs[0])
	if err != nil {
		r.Logger.Error("failed to parse peer address", "err", err)
		return
	}

	// TODO(wllmshao): remove
	r.Logger.Info("Received complete peering address", "addr", addr)

	// Check if we already know this peer
	if r.Switch.Peers().HasIP(addr.IP) {
		return
	}

	// Dial the new peer
	go func() {
		if err := r.sw.DialPeerWithAddress(addr); err != nil {
			r.Logger.Error("failed to dial peer", "addr", addr, "err", err)
		}
		r.Logger.Info("Connected to peer", "addr", addr)
	}()
}
