package block_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dymensionxyz/dymint/mempool"
	mempoolv1 "github.com/dymensionxyz/dymint/mempool/v1"
	"github.com/dymensionxyz/dymint/types"
	tmcfg "github.com/tendermint/tendermint/config"

	"github.com/dymensionxyz/dymint/testutil"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proxy"
)

func TestCreateEmptyBlocksEnableDisable(t *testing.T) {
	const blockTime = 200 * time.Millisecond
	const EmptyBlocksMaxTime = blockTime * 10
	const runTime = 10 * time.Second

	assert := assert.New(t)
	require := require.New(t)
	app := testutil.GetAppMock()
	// Create proxy app
	clientCreator := proxy.NewLocalClientCreator(app)
	proxyApp := proxy.NewAppConns(clientCreator)
	err := proxyApp.Start()
	require.NoError(err)

	// Init manager with empty blocks feature disabled
	managerConfigCreatesEmptyBlocks := testutil.GetManagerConfig()
	managerConfigCreatesEmptyBlocks.BlockTime = blockTime
	managerConfigCreatesEmptyBlocks.EmptyBlocksMaxTime = 0 * time.Second
	managerWithEmptyBlocks, err := testutil.GetManager(managerConfigCreatesEmptyBlocks, nil, nil, 1, 1, 0, proxyApp, nil)
	require.NoError(err)

	// Init manager with empty blocks feature enabled
	managerConfig := testutil.GetManagerConfig()
	managerConfig.BlockTime = blockTime
	managerConfig.EmptyBlocksMaxTime = EmptyBlocksMaxTime
	manager, err := testutil.GetManager(managerConfig, nil, nil, 1, 1, 0, proxyApp, nil)
	require.NoError(err)

	// Check initial height
	initialHeight := uint64(0)
	require.Equal(initialHeight, manager.Store.Height())

	mCtx, cancel := context.WithTimeout(context.Background(), runTime)
	defer cancel()
	go manager.ProduceBlockLoop(mCtx)
	go managerWithEmptyBlocks.ProduceBlockLoop(mCtx)
	<-mCtx.Done()

	require.Greater(manager.Store.Height(), initialHeight)
	require.Greater(managerWithEmptyBlocks.Store.Height(), initialHeight)
	assert.Greater(managerWithEmptyBlocks.Store.Height(), manager.Store.Height())

	// Check that blocks are created with empty blocks feature disabled
	assert.LessOrEqual(manager.Store.Height(), uint64(runTime/EmptyBlocksMaxTime))
	assert.LessOrEqual(managerWithEmptyBlocks.Store.Height(), uint64(runTime/blockTime))

	for i := uint64(2); i < managerWithEmptyBlocks.Store.Height(); i++ {
		prevBlock, err := managerWithEmptyBlocks.Store.LoadBlock(i - 1)
		assert.NoError(err)

		block, err := managerWithEmptyBlocks.Store.LoadBlock(i)
		assert.NoError(err)
		assert.NotZero(block.Header.Time)

		diff := time.Unix(0, int64(block.Header.Time)).Sub(time.Unix(0, int64(prevBlock.Header.Time)))
		assert.Greater(diff, blockTime-blockTime/10)
		assert.Less(diff, blockTime+blockTime/10)
	}

	for i := uint64(2); i < manager.Store.Height(); i++ {
		prevBlock, err := manager.Store.LoadBlock(i - 1)
		assert.NoError(err)

		block, err := manager.Store.LoadBlock(i)
		assert.NoError(err)
		assert.NotZero(block.Header.Time)

		diff := time.Unix(0, int64(block.Header.Time)).Sub(time.Unix(0, int64(prevBlock.Header.Time)))
		assert.Greater(diff, manager.Conf.EmptyBlocksMaxTime)
	}
}

func TestCreateEmptyBlocksNew(t *testing.T) {
	t.Skip("FIXME: fails to submit tx to test the empty blocks feature")
	assert := assert.New(t)
	require := require.New(t)
	app := testutil.GetAppMock()
	// Create proxy app
	clientCreator := proxy.NewLocalClientCreator(app)
	proxyApp := proxy.NewAppConns(clientCreator)
	err := proxyApp.Start()
	require.NoError(err)
	// Init manager
	managerConfig := testutil.GetManagerConfig()
	managerConfig.BlockTime = 200 * time.Millisecond
	managerConfig.EmptyBlocksMaxTime = 1 * time.Second
	manager, err := testutil.GetManager(managerConfig, nil, nil, 1, 1, 0, proxyApp, nil)
	require.NoError(err)

	abciClient, err := clientCreator.NewABCIClient()
	require.NoError(err)
	require.NotNil(abciClient)
	require.NoError(abciClient.Start())

	defer func() {
		// Capture the error returned by abciClient.Stop and handle it.
		err := abciClient.Stop()
		if err != nil {
			t.Logf("Error stopping ABCI client: %v", err)
		}
	}()

	mempoolCfg := tmcfg.DefaultMempoolConfig()
	mempoolCfg.KeepInvalidTxsInCache = false
	mempoolCfg.Recheck = false

	mpool := mempoolv1.NewTxMempool(log.TestingLogger(), tmcfg.DefaultMempoolConfig(), proxy.NewAppConnMempool(abciClient), 0)

	// Check initial height
	expectedHeight := uint64(0)
	assert.Equal(expectedHeight, manager.Store.Height())

	mCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go manager.ProduceBlockLoop(mCtx)

	<-time.Tick(1 * time.Second)
	err = mpool.CheckTx([]byte{1, 2, 3, 4}, nil, mempool.TxInfo{})
	require.NoError(err)

	<-mCtx.Done()
	foundTx := false
	assert.LessOrEqual(manager.Store.Height(), uint64(10))
	for i := uint64(2); i < manager.Store.Height(); i++ {
		prevBlock, err := manager.Store.LoadBlock(i - 1)
		assert.NoError(err)

		block, err := manager.Store.LoadBlock(i)
		assert.NoError(err)
		assert.NotZero(block.Header.Time)

		diff := time.Unix(0, int64(block.Header.Time)).Sub(time.Unix(0, int64(prevBlock.Header.Time)))
		txsCount := len(block.Data.Txs)
		if txsCount == 0 {
			assert.Greater(diff, manager.Conf.EmptyBlocksMaxTime)
			assert.Less(diff, manager.Conf.EmptyBlocksMaxTime+1*time.Second)
		} else {
			foundTx = true
			assert.Less(diff, manager.Conf.BlockTime+100*time.Millisecond)
		}

		fmt.Println("time diff:", diff, "tx len", 0)
	}
	assert.True(foundTx)
}

func TestInvalidBatch(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	manager, err := testutil.GetManager(testutil.GetManagerConfig(), nil, nil, 1, 1, 0, nil, nil)
	require.NoError(err)

	batchSize := uint64(5)
	syncTarget := uint64(10)

	// Create cases
	cases := []struct {
		startHeight uint64
		endHeight   uint64
		shouldError bool
	}{
		{startHeight: syncTarget + 1, endHeight: syncTarget + batchSize, shouldError: false},
		// batch with endHight < startHeight
		{startHeight: syncTarget + 1, endHeight: syncTarget, shouldError: true},
		// batch with startHeight != previousEndHeight + 1
		{startHeight: syncTarget, endHeight: syncTarget + batchSize + batchSize, shouldError: true},
	}
	for _, c := range cases {
		batch := &types.Batch{
			StartHeight: c.startHeight,
			EndHeight:   c.endHeight,
		}

		manager.UpdateSyncParams(syncTarget)
		err := manager.ValidateBatch(batch)
		if c.shouldError {
			assert.Error(err)
		} else {
			assert.NoError(err)
		}
	}
}
