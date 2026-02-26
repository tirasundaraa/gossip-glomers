package node

import maelstrom "github.com/jepsen-io/maelstrom/demo/go"

type Server struct {
	Node *maelstrom.Node
}

func NewServer() *Server {
	return &Server{
		Node: maelstrom.NewNode(),
	}
}
