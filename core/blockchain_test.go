package core

import (
	"encoding/json"
	"fmt"
	"github.com/bnb-chain/zkbnb-crypto/legend/circuit/bn254/block"
	sdb "github.com/bnb-chain/zkbnb/core/statedb"
	"github.com/bnb-chain/zkbnb/dao/dbcache"
	"github.com/bnb-chain/zkbnb/tree"
	"github.com/stretchr/testify/assert"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os/exec"
	"testing"
	"time"
)

var dsn = "host=localhost user=postgres password=ZkBNB@123 dbname=zkbnb port=5434 sslmode=disable"
var config = &ChainConfig{
	Postgres: struct{ DataSource string }{DataSource: dsn},
	TreeDB: struct {
		Driver        tree.Driver
		LevelDBOption tree.LevelDBOption `json:",optional"`
		RedisDBOption tree.RedisDBOption `json:",optional"`
	}{Driver: "memorydb"},
}

func TestImportBlock(t *testing.T) {
	testDBSetup()
	defer testDBShutdown()
	for i := int64(0); i <= 0; i++ {
		fmt.Println(i)
		chain, err := NewTestBlockChain(config, "testBlock", i)
		p1, _ := chain.StateDB().AccountTree.GetProof(uint64(0))
		bzp1, _ := json.MarshalIndent(p1, "\t", "\t")
		p2, _ := chain.StateDB().AccountTree.GetProof(uint64(block.LastAccountIndex))
		bzp2, _ := json.MarshalIndent(p2, "\t", "\t")

		fmt.Println(string(bzp1))
		fmt.Println(string(bzp2))

		assert.NoError(t, err, fmt.Sprintf("failed to create chain at height %d", i))
		err = chain.importNextBlock()
		assert.NoError(t, err, fmt.Sprintf("failed to import block height %d", i+1))
		w, _ := chain.BlockWitnessModel.GetBlockWitnessByHeight(i + 1)
		bz1, _ := json.MarshalIndent(chain.StateDB().Witnesses, "\t", "\t")
		var cryptoBlock *block.Block
		json.Unmarshal([]byte(w.WitnessData), &cryptoBlock)
		bz2, _ := json.MarshalIndent(cryptoBlock.Txs, "\t", "\t")
		assert.Equal(t, string(bz1), string(bz2))
		fmt.Println(string(bz1))
		fmt.Println(string(bz2))
	}
}

func NewTestBlockChain(config *ChainConfig, moduleName string, curHeight int64) (*BlockChain, error) {
	db, err := gorm.Open(postgres.Open(config.Postgres.DataSource))
	if err != nil {
		logx.Error("gorm connect db failed: ", err)
		return nil, err
	}
	bc := &BlockChain{
		ChainDB:     sdb.NewChainDB(db),
		chainConfig: config,
	}

	bc.currentBlock, err = bc.BlockModel.GetBlockByHeight(curHeight)
	if err != nil {
		return nil, err
	}
	treeCtx := &tree.Context{
		Name:          moduleName,
		Driver:        config.TreeDB.Driver,
		LevelDBOption: &config.TreeDB.LevelDBOption,
		RedisDBOption: &config.TreeDB.RedisDBOption,
	}
	bc.Statedb, err = sdb.NewStateDB(treeCtx, bc.ChainDB, dbcache.NewDummyCache(), bc.currentBlock.StateRoot, curHeight)
	if err != nil {
		return nil, err
	}
	bc.processor = NewCommitProcessor(bc)
	return bc, nil
}

func (b *BlockChain) importNextBlock() error {
	nextBlock, err := b.ChainDB.BlockModel.GetBlockByHeight(b.currentBlock.BlockHeight + 1)
	if err != nil {
		return err
	}
	for _, tx := range nextBlock.Txs {
		err := b.ApplyTransaction(tx)
		if err != nil {
			return err
		}
	}
	return nil
}

func testDBSetup() {
	testDBShutdown()
	time.Sleep(3 * time.Second)
	cmd := exec.Command("docker", "run", "--name", "postgres-ut-witness", "-p", "5434:5432",
		"-e", "POSTGRES_PASSWORD=ZkBNB@123", "-e", "POSTGRES_USER=postgres", "-e", "POSTGRES_DB=zkbnb",
		"-e", "PGDATA=/var/lib/postgresql/pgdata", "-d", "ghcr.io/bnb-chain/zkbnb/zkbnb-ut-postgres:0.0.2")
	if err := cmd.Run(); err != nil {
		panic(err)
	}
	time.Sleep(10 * time.Second)
}

func testDBShutdown() {
	cmd := exec.Command("docker", "kill", "postgres-ut-witness")
	//nolint:errcheck
	cmd.Run()
	time.Sleep(time.Second)
	cmd = exec.Command("docker", "rm", "postgres-ut-witness")
	//nolint:errcheck
	cmd.Run()
}