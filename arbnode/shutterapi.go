package arbnode

import (
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
)

// DecodeBatchTx decodes a hex string to a types.BatchTx. The hex decoded data is assumed to be an
// RLP encoded BatchTx
func DecodeBatchTx(hexString string) (*types.BatchTx, error) {
	b, err := hex.DecodeString(hexString)
	if err != nil {
		return nil, err
	}
	var batchTx types.BatchTx
	err = rlp.DecodeBytes(b, &batchTx)
	if err != nil {
		return nil, err
	}
	return &batchTx, nil
}

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

func (shapi *ShutterAPI) SubmitBatch(s string) error {
	batchTx, err := DecodeBatchTx(s)
	if err != nil {
		return err
	}
	_ = batchTx // XXX
	return nil
}
