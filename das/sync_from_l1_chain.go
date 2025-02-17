package das

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/arbstate"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/solgen/go/bridgegen"
	"github.com/offchainlabs/nitro/util/arbmath"
	flag "github.com/spf13/pflag"
)

type SyncToStorageConfig struct {
	Eager                  bool          `koanf:"eager"`
	EagerStopsWhenCaughtUp bool          `koanf:"eager-stops-when-caught-up"`
	EagerLowerBoundBlock   uint64        `koanf:"eager-lower-bound-block"`
	RetentionPeriod        time.Duration `koanf:"retention-period"`
	IgnoreWriteErrors      bool          `koanf:"ignore-write-errors"`
}

var DefaultSyncToStorageConfig = SyncToStorageConfig{
	Eager:                  false,
	EagerStopsWhenCaughtUp: false,
	EagerLowerBoundBlock:   0,
	RetentionPeriod:        time.Duration(math.MaxInt64),
	IgnoreWriteErrors:      true,
}

func SyncToStorageConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Bool(prefix+".eager", DefaultSyncToStorageConfig.Eager, "eagerly sync batch data to this DAS's storage from the rest endpoints, using L1 as the index of batch data hashes; otherwise only sync lazily")
	f.Bool(prefix+".eager-stops-when-caught-up", DefaultSyncToStorageConfig.EagerStopsWhenCaughtUp, "stop the sync process as soon as it is caught up, after which this DAS will only get newer batch data via lazy syncing on missed reads or if the batch poster directly requests it to store the data; otherwise leave it running")
	f.Uint64(prefix+".eager-lower-bound-block", DefaultSyncToStorageConfig.EagerLowerBoundBlock, "when eagerly syncing, start indexing forward from this L1 block")
	f.Duration(prefix+".retention-period", DefaultSyncToStorageConfig.RetentionPeriod, "period to retain synced data (defaults to forever)")
	f.Bool(prefix+".ignore-write-errors", DefaultSyncToStorageConfig.IgnoreWriteErrors, "log only on failures to write when syncing; otherwise treat it as an error")
}

func NewSyncingFallbackStorageService(
	ctx context.Context,
	primary StorageService,
	backup arbstate.DataAvailabilityReader,
	backupRetentionSeconds uint64, // how long to retain data that we copy in from the backup (MaxUint64 means forever)
	ignoreRetentionWriteErrors bool, // if true, don't return error if write of retention data to primary fails
	preventRecursiveGets bool, // if true, return NotFound on simultaneous calls to Gets that miss in primary (prevents infinite recursion)
	l1client arbutil.L1Interface,
	seqInboxAddr common.Address,
	lowerBoundL1BlockNum *uint64,
	expirationTime uint64,
	stopWhenCaughtUp bool,
) (*FallbackStorageService, error) {
	go func() {
		err := SyncStorageServiceFromChain(ctx, primary, backup, l1client, seqInboxAddr, lowerBoundL1BlockNum, expirationTime, stopWhenCaughtUp)
		if err != nil {
			log.Warn("Error in SyncStorageServiceFromChain", "err", err)
		}
	}()
	return NewFallbackStorageService(primary, backup, backupRetentionSeconds, ignoreRetentionWriteErrors, preventRecursiveGets), nil
}

func SyncStorageServiceFromChain(
	ctx context.Context,
	syncTo StorageService,
	dataSource arbstate.DataAvailabilityReader,
	l1client arbutil.L1Interface,
	seqInboxAddr common.Address,
	lowerBoundL1BlockNum *uint64,
	expirationTime uint64,
	stopWhenCaughtUp bool,
) error {
	// make sure that as we sync, any Keysets missing from dataSource will fetched from the L1 chain
	dataSource, err := NewChainFetchReader(dataSource, l1client, seqInboxAddr)
	if err != nil {
		return err
	}

	seqInbox, err := bridgegen.NewSequencerInbox(seqInboxAddr, l1client)
	if err != nil {
		return err
	}
	seqInboxFilterer := seqInbox.SequencerInboxFilterer
	eventChan := make(chan *bridgegen.SequencerInboxSequencerBatchData)
	defer close(eventChan)
	subscription, err := seqInboxFilterer.WatchSequencerBatchData(&bind.WatchOpts{Context: ctx, Start: lowerBoundL1BlockNum}, eventChan, nil)
	if err != nil {
		return err
	}
	defer subscription.Unsubscribe()

	latestL1BlockNumber, err := l1client.BlockNumber(ctx)
	if err != nil {
		return err
	}
	for {
		select {
		case event := <-eventChan:
			data := event.Data
			if len(data) >= 41 && arbstate.IsDASMessageHeaderByte(data[40]) {
				preimages := make(map[common.Hash][]byte)
				if _, err = arbstate.RecoverPayloadFromDasBatch(ctx, data, dataSource, preimages); err != nil {
					return err
				}
				for hash, contents := range preimages {
					_, err := syncTo.GetByHash(ctx, hash.Bytes())
					if errors.Is(err, ErrNotFound) {
						if err := syncTo.Put(ctx, contents, arbmath.SaturatingUAdd(uint64(time.Now().Unix()), expirationTime)); err != nil {
							return err
						}
					} else if err != nil {
						return err
					}
				}
			}
			if stopWhenCaughtUp {
				if event.Raw.BlockNumber >= latestL1BlockNumber {
					return syncTo.Sync(ctx)
				}
				latestL1BlockNumber, err = l1client.BlockNumber(ctx)
				if err != nil {
					return err
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
