// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package das

import (
	"bytes"
	"context"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"
)

const LocalServerAddressForTest = "localhost:9877"

func TestRestfulServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbPath, err := ioutil.TempDir("/tmp", "das_test")
	defer func() { _ = os.RemoveAll(dbPath) }()
	Require(t, err)

	storage := NewLocalDiskStorageService(dbPath)
	data := []byte("Testing a restful server now.")
	dataHash := crypto.Keccak256(data)

	server := NewRestfulDasServerHTTP(LocalServerAddressForTest, storage)

	err = storage.PutByHash(ctx, dataHash, data, uint64(time.Now().Add(time.Hour).Unix()))
	Require(t, err)

	urlString := "http://" + LocalServerAddressForTest + "/get-by-hash/" + (hexutil.Encode(dataHash)[2:])
	resp, err := http.Get(urlString) //nolint:gosec
	Require(t, err)
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}
	bodyContents, err := io.ReadAll(resp.Body)
	Require(t, err)
	if !bytes.Equal(bodyContents, data) {
		t.Fatal()
	}

	err = server.Shutdown()
	Require(t, err)
}