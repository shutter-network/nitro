package arbtest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/offchainlabs/nitro/blsSignatures"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/offchainlabs/nitro/arbnode"

	"github.com/offchainlabs/nitro/das/dasrpc"

	"github.com/offchainlabs/nitro/das"
)

func startLocalDASServer(
	t *testing.T,
	ctx context.Context,
	dataDir string,
	l1client arbutil.L1Interface,
	seqInboxAddress common.Address,
) (*http.Server, *blsSignatures.PublicKey, dasrpc.BackendConfig) {
	lis, err := net.Listen("tcp", "localhost:0")
	Require(t, err)
	keyDir := t.TempDir()
	pubkey, _, err := das.GenerateAndStoreKeys(keyDir)
	Require(t, err)

	config := das.DataAvailabilityConfig{
		Enable: true,
		KeyConfig: das.KeyConfig{
			KeyDir: keyDir,
		},
		LocalFileStorageConfig: das.LocalFileStorageConfig{
			Enable:  true,
			DataDir: keyDir,
		},
		L1NodeURL: "none",
	}

	storageService, lifecycleManager, err := das.CreatePersistentStorageService(ctx, &config)
	defer lifecycleManager.StopAndWaitUntil(time.Second)

	Require(t, err)
	seqInboxCaller, err := bridgegen.NewSequencerInboxCaller(seqInboxAddress, l1client)
	Require(t, err)
	das, err := das.NewSignAfterStoreDASWithSeqInboxCaller(ctx, config.KeyConfig, seqInboxCaller, storageService)
	Require(t, err)
	dasServer, err := dasrpc.StartDASRPCServerOnListener(ctx, lis, das)
	Require(t, err)
	beConfig := dasrpc.BackendConfig{
		URL:                 "http://" + lis.Addr().String(),
		PubKeyBase64Encoded: blsPubToBase64(pubkey),
		SignerMask:          1,
	}
	return dasServer, pubkey, beConfig
}

func blsPubToBase64(pubkey *blsSignatures.PublicKey) string {
	pubkeyBytes := blsSignatures.PublicKeyToBytes(*pubkey)
	encodedPubkey := make([]byte, base64.StdEncoding.EncodedLen(len(pubkeyBytes)))
	base64.StdEncoding.Encode(encodedPubkey, pubkeyBytes)
	return string(encodedPubkey)
}

func aggConfigForBackend(t *testing.T, backendConfig dasrpc.BackendConfig) das.AggregatorConfig {
	backendsJsonByte, err := json.Marshal([]dasrpc.BackendConfig{backendConfig})
	Require(t, err)
	return das.AggregatorConfig{
		Enable:        true,
		AssumedHonest: 1,
		Backends:      string(backendsJsonByte),
	}
}

func TestDASRekey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup L1 chain and contracts
	chainConfig := params.ArbitrumDevTestDASChainConfig()
	l1info, l1client, _, l1stack := CreateTestL1BlockChain(t, nil)
	defer l1stack.Close()
	addresses := DeployOnTestL1(t, ctx, l1info, l1client, chainConfig.ChainID)

	// Setup DAS servers
	dasDataDir := t.TempDir()
	dasServerA, pubkeyA, backendConfigA := startLocalDASServer(t, ctx, dasDataDir, l1client, addresses.SequencerInbox)
	authorizeDASKeyset(t, ctx, pubkeyA, l1info, l1client)

	// Setup L2 chain
	l2info, l2stack, l2chainDb, l2blockchain := createL2BlockChain(t, nil, chainConfig)
	l2info.GenerateAccount("User2")

	// Setup DAS config
	l1NodeConfigA := arbnode.ConfigDefaultL1Test()
	l1NodeConfigA.DataAvailability.Enable = true
	l1NodeConfigA.DataAvailability.AggregatorConfig = aggConfigForBackend(t, backendConfigA)

	sequencerTxOpts := l1info.GetDefaultTransactOpts("Sequencer", ctx)
	sequencerTxOptsPtr := &sequencerTxOpts
	nodeA, err := arbnode.CreateNode(ctx, l2stack, l2chainDb, l1NodeConfigA, l2blockchain, l1client, addresses, sequencerTxOptsPtr, nil)
	Require(t, err)
	Require(t, nodeA.Start(ctx))
	l2clientA := ClientForArbBackend(t, nodeA.Backend)

	l1NodeConfigB := arbnode.ConfigDefaultL1Test()
	l1NodeConfigB.BatchPoster.Enable = false
	l1NodeConfigB.BlockValidator.Enable = false
	l1NodeConfigA.DataAvailability.Enable = true
	l1NodeConfigB.DataAvailability.AggregatorConfig = aggConfigForBackend(t, backendConfigA)
	l2clientB, nodeB := Create2ndNodeWithConfig(t, ctx, nodeA, l1stack, &l2info.ArbInitData, l1NodeConfigB)
	checkBatchPosting(t, ctx, l1client, l2clientA, l1info, l2info, big.NewInt(1e12), l2clientB)
	nodeA.StopAndWait()
	nodeB.StopAndWait()

	err = dasServerA.Shutdown(ctx)
	Require(t, err)
	dasServerB, pubkeyB, backendConfigB := startLocalDASServer(t, ctx, dasDataDir, l1client, addresses.SequencerInbox)
	defer func() {
		err = dasServerB.Shutdown(ctx)
		Require(t, err)
	}()
	authorizeDASKeyset(t, ctx, pubkeyB, l1info, l1client)

	// Restart the node on the new keyset against the new DAS server running on the same disk as the first with new keys

	l2stack, err = arbnode.CreateDefaultStack()
	Require(t, err)
	l2blockchain, err = arbnode.GetBlockChain(l2chainDb, nil, chainConfig)
	Require(t, err)
	l1NodeConfigA.DataAvailability.AggregatorConfig = aggConfigForBackend(t, backendConfigB)
	nodeA, err = arbnode.CreateNode(ctx, l2stack, l2chainDb, l1NodeConfigA, l2blockchain, l1client, addresses, sequencerTxOptsPtr, nil)
	Require(t, err)
	Require(t, nodeA.Start(ctx))
	l2clientA = ClientForArbBackend(t, nodeA.Backend)

	l1NodeConfigB.DataAvailability.AggregatorConfig = aggConfigForBackend(t, backendConfigB)
	l2clientB, nodeB = Create2ndNodeWithConfig(t, ctx, nodeA, l1stack, &l2info.ArbInitData, l1NodeConfigB)
	checkBatchPosting(t, ctx, l1client, l2clientA, l1info, l2info, big.NewInt(2e12), l2clientB)

	nodeA.StopAndWait()
	nodeB.StopAndWait()
}

func checkBatchPosting(t *testing.T, ctx context.Context, l1client, l2clientA *ethclient.Client, l1info, l2info info, expectedBalance *big.Int, l2ClientsToCheck ...*ethclient.Client) {
	tx := l2info.PrepareTx("Owner", "User2", l2info.TransferGas, big.NewInt(1e12), nil)
	err := l2clientA.SendTransaction(ctx, tx)
	Require(t, err)

	_, err = EnsureTxSucceeded(ctx, l2clientA, tx)
	Require(t, err)

	// give the inbox reader a bit of time to pick up the delayed message
	time.Sleep(time.Millisecond * 100)

	// sending l1 messages creates l1 blocks.. make enough to get that delayed inbox message in
	for i := 0; i < 30; i++ {
		SendWaitTestTransactions(t, ctx, l1client, []*types.Transaction{
			l1info.PrepareTx("Faucet", "User", 30000, big.NewInt(1e12), nil),
		})
	}

	for _, client := range l2ClientsToCheck {
		_, err = WaitForTx(ctx, client, tx.Hash(), time.Second*5)
		Require(t, err)

		l2balance, err := client.BalanceAt(ctx, l2info.GetAddress("User2"), nil)
		Require(t, err)

		if l2balance.Cmp(expectedBalance) != 0 {
			Fail(t, "Unexpected balance:", l2balance)
		}

	}
}

func TestDASComplexConfigAndRestMirror(t *testing.T) {
	initTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup L1 chain and contracts
	chainConfig := params.ArbitrumDevTestDASChainConfig()
	l1info, l1client, _, l1stack := CreateTestL1BlockChain(t, nil)
	defer l1stack.Close()
	addresses := DeployOnTestL1(t, ctx, l1info, l1client, chainConfig.ChainID)

	lis, err := net.Listen("tcp", "localhost:0")
	Require(t, err)
	keyDir, fileDataDir, dbDataDir := t.TempDir(), t.TempDir(), t.TempDir()
	pubkey, _, err := das.GenerateAndStoreKeys(keyDir)
	Require(t, err)

	serverConfig := das.DataAvailabilityConfig{
		Enable: true,

		LocalCacheConfig: das.BigCacheConfig{
			Enable:     true,
			Expiration: time.Hour,
		},
		RedisCacheConfig: das.RedisConfig{
			Enable:     false,
			RedisUrl:   "",
			Expiration: time.Hour,
			KeyConfig:  "",
		},

		LocalFileStorageConfig: das.LocalFileStorageConfig{
			Enable:  true,
			DataDir: fileDataDir,
		},
		LocalDBStorageConfig: das.LocalDBStorageConfig{
			Enable:  true,
			DataDir: dbDataDir,
		},
		S3StorageServiceConfig: das.S3StorageServiceConfig{
			Enable:    false,
			AccessKey: "",
			Bucket:    "",
			Region:    "",
			SecretKey: "",
		},

		RestfulClientAggregatorConfig: das.RestfulClientAggregatorConfig{
			Enable:                 false,
			Urls:                   []string{},
			Strategy:               "",
			StrategyUpdateInterval: time.Second,
			WaitBeforeTryNext:      time.Second,
			MaxPerEndpointStats:    20,
			SimpleExploreExploitStrategyConfig: das.SimpleExploreExploitStrategyConfig{
				ExploreIterations: 1,
				ExploitIterations: 1,
			},
		},

		KeyConfig: das.KeyConfig{
			KeyDir: keyDir,
		},

		// L1NodeURL: normally we would have to set this but we are passing in the already constructed client and addresses to the factory
	}

	dasServerStack, lifecycleManager, err := arbnode.SetUpDataAvailability(ctx, &serverConfig, l1client, addresses)
	Require(t, err)
	dasServer, err := dasrpc.StartDASRPCServerOnListener(ctx, lis, dasServerStack)
	Require(t, err)

	_ = dasServer
	pubkeyA := pubkey
	authorizeDASKeyset(t, ctx, pubkeyA, l1info, l1client)

	//
	l1NodeConfigA := arbnode.ConfigDefaultL1Test()
	l1NodeConfigA.DataAvailability = das.DataAvailabilityConfig{
		Enable: true,

		LocalCacheConfig: das.BigCacheConfig{
			Enable:     true,
			Expiration: time.Hour,
		},
		RedisCacheConfig: das.RedisConfig{
			Enable:     false,
			RedisUrl:   "",
			Expiration: time.Hour,
			KeyConfig:  "",
		},

		// AggregatorConfig set up below
	}

	beConfigA := dasrpc.BackendConfig{
		URL:                 "http://" + lis.Addr().String(),
		PubKeyBase64Encoded: blsPubToBase64(pubkey),
		SignerMask:          1,
	}

	l1NodeConfigA.DataAvailability.AggregatorConfig = aggConfigForBackend(t, beConfigA)

	var daSigner das.DasSigner = func(data []byte) ([]byte, error) {
		return crypto.Sign(data, l1info.Accounts["Sequencer"].PrivateKey)
	}

	Require(t, err)

	// Setup L2 chain
	l2info, l2stack, l2chainDb, l2blockchain := createL2BlockChain(t, nil, chainConfig)
	l2info.GenerateAccount("User2")

	sequencerTxOpts := l1info.GetDefaultTransactOpts("Sequencer", ctx)
	sequencerTxOptsPtr := &sequencerTxOpts
	nodeA, err := arbnode.CreateNode(ctx, l2stack, l2chainDb, l1NodeConfigA, l2blockchain, l1client, addresses, sequencerTxOptsPtr, daSigner)
	Require(t, err)
	Require(t, nodeA.Start(ctx))
	l2clientA := ClientForArbBackend(t, nodeA.Backend)

	l1NodeConfigB := arbnode.ConfigDefaultL1Test()
	l1NodeConfigB.DataAvailability = das.DataAvailabilityConfig{
		Enable: true,

		LocalCacheConfig: das.BigCacheConfig{
			Enable:     true,
			Expiration: time.Hour,
		},
		RedisCacheConfig: das.RedisConfig{
			Enable:     false,
			RedisUrl:   "",
			Expiration: time.Hour,
			KeyConfig:  "",
		},

		// AggregatorConfig set up below

		L1NodeURL: "none",
	}

	l1NodeConfigB.BatchPoster.Enable = false
	l1NodeConfigB.BlockValidator.Enable = false
	l1NodeConfigA.DataAvailability.Enable = true
	l1NodeConfigB.DataAvailability.AggregatorConfig = aggConfigForBackend(t, beConfigA)
	l2clientB, nodeB := Create2ndNodeWithConfig(t, ctx, nodeA, l1stack, &l2info.ArbInitData, l1NodeConfigB)

	// Now create a separate REST DAS server using the same local disk storage
	// and connect a node to it, and make sure it syncs.
	restServerConfig := das.DataAvailabilityConfig{
		Enable: true,

		LocalFileStorageConfig: das.LocalFileStorageConfig{
			Enable:  true,
			DataDir: fileDataDir,
		},
	}

	restServerDAS, rpcServerLifecycleManager, err := das.CreatePersistentStorageService(ctx, &restServerConfig)
	Require(t, err)
	restLis, err := net.Listen("tcp", "localhost:0")
	Require(t, err)
	restServer, err := das.NewRestfulDasServerOnListener(restLis, restServerDAS)
	Require(t, err)

	l1NodeConfigC := arbnode.ConfigDefaultL1Test()
	l1NodeConfigC.BatchPoster.Enable = false
	l1NodeConfigC.BlockValidator.Enable = false
	l1NodeConfigC.DataAvailability = das.DataAvailabilityConfig{
		Enable: true,

		LocalCacheConfig: das.BigCacheConfig{
			Enable:     true,
			Expiration: time.Hour,
		},

		RestfulClientAggregatorConfig: das.RestfulClientAggregatorConfig{
			Enable:                 true,
			Urls:                   []string{"http://" + restLis.Addr().String()},
			Strategy:               "simple-explore-exploit",
			StrategyUpdateInterval: time.Second,
			WaitBeforeTryNext:      time.Second,
			MaxPerEndpointStats:    20,
			SimpleExploreExploitStrategyConfig: das.SimpleExploreExploitStrategyConfig{
				ExploreIterations: 1,
				ExploitIterations: 5,
			},
		},

		// L1NodeURL: normally we would have to set this but we are passing in the already constructed client and addresses to the factory
	}
	l2clientC, nodeC := Create2ndNodeWithConfig(t, ctx, nodeA, l1stack, &l2info.ArbInitData, l1NodeConfigC)

	checkBatchPosting(t, ctx, l1client, l2clientA, l1info, l2info, big.NewInt(1e12), l2clientB, l2clientC)

	nodeA.StopAndWait()
	nodeB.StopAndWait()
	nodeC.StopAndWait()

	err = restServer.Shutdown()
	Require(t, err)

	lifecycleManager.StopAndWaitUntil(time.Second)
	rpcServerLifecycleManager.StopAndWaitUntil(time.Second)

}

func enableLogging(logLvl int) {
	glogger := log.NewGlogHandler(log.StreamHandler(os.Stderr, log.TerminalFormat(false)))
	glogger.Verbosity(log.Lvl(logLvl))
	log.Root().SetHandler(glogger)
}

func initTest(t *testing.T) {
	loggingStr := os.Getenv("LOGGING")
	if len(loggingStr) > 0 {
		var err error
		logLvl, err := strconv.Atoi(loggingStr)
		Require(t, err, "Failed to parse string")
		enableLogging(logLvl)
	}
}
