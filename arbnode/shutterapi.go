package arbnode

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/pkg/errors"
)

var errBadType = errors.New("not a batch transaction")

// DecodeTx decodes a 0x prefixed hex string to a Transaction. The hex decoded data is assumed to
// be a batch transaction in canonical encoding.
func DecodeTx(hexString string) (*types.Transaction, error) {
	b, err := hexutil.Decode(hexString)
	if err != nil {
		return nil, err
	}
	var tx types.Transaction
	err = tx.UnmarshalBinary(b)
	if err != nil {
		return nil, err
	}
	if tx.Type() != types.BatchTxType {
		return nil, errBadType
	}
	return &tx, nil
}

type ShutterAPI struct {
	txPublisher TransactionPublisher
}

func NewShutterAPI(txPublisher TransactionPublisher) rpc.API {
	return rpc.API{
		Namespace: "shutter",
		Version:   "1.0",
		Service:   &ShutterAPI{txPublisher: txPublisher},
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
	ctx := context.TODO()
	tx, err := DecodeTx(s)
	if err != nil {
		return err
	}
	err = shapi.txPublisher.PublishTransaction(ctx, tx)
	return err
}
