// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package dasrpc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum/go-ethereum/rpc"

	"github.com/offchainlabs/nitro/blsSignatures"
	"github.com/offchainlabs/nitro/das"
	"github.com/offchainlabs/nitro/util/pretty"
)

type DASRPCServer struct {
	localDAS das.DataAvailabilityService
}

func StartDASRPCServer(ctx context.Context, addr string, portNum uint64, localDAS das.DataAvailabilityService) (*http.Server, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", addr, portNum))
	if err != nil {
		return nil, err
	}
	return StartDASRPCServerOnListener(ctx, listener, localDAS)
}

func StartDASRPCServerOnListener(ctx context.Context, listener net.Listener, localDAS das.DataAvailabilityService) (*http.Server, error) {
	rpcServer := rpc.NewServer()
	err := rpcServer.RegisterName("das", &DASRPCServer{localDAS: localDAS})
	if err != nil {
		return nil, err
	}

	srv := &http.Server{
		Handler: rpcServer,
	}

	go func() {
		err := srv.Serve(listener)
		if err != nil {
			return
		}
	}()
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	return srv, nil
}

type StoreResult struct {
	DataHash    hexutil.Bytes  `json:"dataHash,omitempty"`
	Timeout     hexutil.Uint64 `json:"timeout,omitempty"`
	SignersMask hexutil.Uint64 `json:"signersMask,omitempty"`
	KeysetHash  hexutil.Bytes  `json:"keysetHash,omitempty"`
	Sig         hexutil.Bytes  `json:"sig,omitempty"`
}

func (serv *DASRPCServer) Store(ctx context.Context, message hexutil.Bytes, timeout hexutil.Uint64, sig hexutil.Bytes) (*StoreResult, error) {
	log.Trace("dasRpc.DASRPCServer.Store", "message", pretty.FirstFewBytes(message), "message length", len(message), "timeout", time.Unix(int64(timeout), 0), "sig", pretty.FirstFewBytes(sig), "this", serv)

	cert, err := serv.localDAS.Store(ctx, message, uint64(timeout), sig)
	if err != nil {
		return nil, err
	}
	return &StoreResult{
		KeysetHash:  cert.KeysetHash[:],
		DataHash:    cert.DataHash[:],
		Timeout:     hexutil.Uint64(cert.Timeout),
		SignersMask: hexutil.Uint64(cert.SignersMask),
		Sig:         blsSignatures.SignatureToBytes(cert.Sig),
	}, nil
}

func (serv *DASRPCServer) GetByHash(ctx context.Context, certBytes hexutil.Bytes) (hexutil.Bytes, error) {
	return serv.localDAS.GetByHash(ctx, certBytes)
}

func (serv *DASRPCServer) HealthCheck(ctx context.Context) error {
	return serv.localDAS.HealthCheck(ctx)
}

func (serv *DASRPCServer) ExpirationPolicy(ctx context.Context) (string, error) {
	expirationPolicy, err := serv.localDAS.ExpirationPolicy(ctx)
	if err != nil {
		return "", err
	}
	return expirationPolicy.String()
}
