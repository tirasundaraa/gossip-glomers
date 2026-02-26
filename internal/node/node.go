package node

import (
	"sync/atomic"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

type Server struct {
	Node    *maelstrom.Node
	counter atomic.Int64
}

func NewServer() *Server {
	return &Server{
		Node: maelstrom.NewNode(),
	}
}
