package arbnode

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/rpc"
)

type ShutterAPI struct {
	blockchain *core.BlockChain
}

func NewShutterAPI(blockchain *core.BlockChain) rpc.API {
	return rpc.API{
		Namespace: "shutter",
		Version:   "1.0",
		Service:   &ShutterAPI{blockchain: blockchain},
		Public:    false,
	}
}

// Hello world as an RPC service.
// Can be called with
// curl -v --header "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"shutter_hello","params":["world"],"id":1}' http://127.0.0.1:8547
func (shapi *ShutterAPI) Hello(s string) string {
	return fmt.Sprintf("Hello %s", s)
}
