package p2p

import (
	"fmt"

	"github.com/cosmos/gogoproto/proto"
)

func (m *Valp2PAddr) Wrap() proto.Message {
	pm := &Valp2PMessage{}
	pm.Sum = &Valp2PMessage_Valp2PAddr{Valp2PAddr: m}
	return pm
}

func (m *Valp2PRequest) Wrap() proto.Message {
	pm := &Valp2PMessage{}
	pm.Sum = &Valp2PMessage_Valp2PRequest{Valp2PRequest: m}
	return pm
}

func (m *Valp2PResponse) Wrap() proto.Message {
	pm := &Valp2PMessage{}
	pm.Sum = &Valp2PMessage_Valp2PResponse{Valp2PResponse: m}
	return pm
}

// Unwrap implements the p2p Wrapper interface and unwraps a wrapped PEX
// message.
func (m *Valp2PMessage) Unwrap() (proto.Message, error) {
	switch msg := m.Sum.(type) {
	case *Valp2PMessage_Valp2PAddr:
		return msg.Valp2PAddr, nil
	case *Valp2PMessage_Valp2PRequest:
		return msg.Valp2PRequest, nil
	case *Valp2PMessage_Valp2PResponse:
		return msg.Valp2PResponse, nil
	default:
		return nil, fmt.Errorf("unknown valp2p message: %T", msg)
	}
}
