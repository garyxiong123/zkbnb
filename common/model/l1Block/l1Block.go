/*
 * Copyright © 2021 Zkbas Protocol
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package l1Block

import (
	"encoding/json"
	"errors"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"gorm.io/gorm"

	asset "github.com/bnb-chain/zkbas/common/model/assetInfo"
	"github.com/bnb-chain/zkbas/common/model/block"
	"github.com/bnb-chain/zkbas/common/model/mempool"
	"github.com/bnb-chain/zkbas/common/model/priorityRequest"
	"github.com/bnb-chain/zkbas/common/model/sysconfig"
	"github.com/bnb-chain/zkbas/errorcode"
)

type (
	L1BlockModel interface {
		CreateL1BlockTable() error
		DropL1BlockTable() error
		CreateMonitorsInfoAndUpdateBlocksAndTxs(
			blockInfo *L1Block,
			txEventMonitors []*priorityRequest.PriorityRequest,
			pendingUpdateBlocks []*block.Block,
			pendingUpdateMempoolTxs []*mempool.MempoolTx,
		) (err error)
		CreateGovernanceMonitorInfo(
			blockInfo *L1Block,
			l2AssetInfos []*asset.AssetInfo,
			pendingUpdateL2AssetInfos []*asset.AssetInfo,
			pendingNewSysconfigInfos []*sysconfig.Sysconfig,
			pendingUpdateSysconfigInfos []*sysconfig.Sysconfig,
		) (err error)
		GetLatestL1BlockByType(blockType int) (blockInfo *L1Block, err error)
		GetLatestL1BlockMonitorByGovernance() (blockInfo *L1Block, err error)
	}

	defaultL1BlockMonitorModel struct {
		sqlc.CachedConn
		table string
		DB    *gorm.DB
	}

	L1Block struct {
		gorm.Model
		// l1 block height
		L1BlockHeight int64
		// block info, array of hashes
		BlockInfo string
		Type      int
	}
)

func (*L1Block) TableName() string {
	return TableName
}

func NewL1BlockModel(conn sqlx.SqlConn, c cache.CacheConf, db *gorm.DB) L1BlockModel {
	return &defaultL1BlockMonitorModel{
		CachedConn: sqlc.NewConn(conn, c),
		table:      TableName,
		DB:         db,
	}
}

func (m *defaultL1BlockMonitorModel) CreateL1BlockTable() error {
	return m.DB.AutoMigrate(L1Block{})
}

func (m *defaultL1BlockMonitorModel) DropL1BlockTable() error {
	return m.DB.Migrator().DropTable(m.table)
}

func (m *defaultL1BlockMonitorModel) CreateMonitorsInfoAndUpdateBlocksAndTxs(
	blockInfo *L1Block,
	txEventMonitors []*priorityRequest.PriorityRequest,
	pendingUpdateBlocks []*block.Block,
	pendingUpdateMempoolTxs []*mempool.MempoolTx,
) (err error) {
	const (
		Txs = "Txs"
	)

	err = m.DB.Transaction(
		func(tx *gorm.DB) error { // transact
			// create data for l1 block info
			dbTx := tx.Table(m.table).Create(blockInfo)
			if dbTx.Error != nil {
				return dbTx.Error
			}
			if dbTx.RowsAffected == 0 {
				return errors.New("unable to create l1 block info")
			}
			// create data in batches for l2 txVerification event monitor
			dbTx = tx.Table(priorityRequest.TableName).CreateInBatches(txEventMonitors, len(txEventMonitors))
			if dbTx.Error != nil {
				return dbTx.Error
			}
			if dbTx.RowsAffected != int64(len(txEventMonitors)) {
				return errors.New("unable to create l2 txVerification event monitors")
			}

			// update blocks
			for _, pendingUpdateBlock := range pendingUpdateBlocks {
				dbTx := tx.Table(block.BlockTableName).Where("id = ?", pendingUpdateBlock.ID).
					Omit(Txs).
					Select("*").
					Updates(&pendingUpdateBlock)
				if dbTx.Error != nil {
					logx.Errorf("update block error, err: %s", dbTx.Error.Error())
					return dbTx.Error
				}
				if dbTx.RowsAffected == 0 {
					blocksInfo, err := json.Marshal(pendingUpdateBlocks)
					if err != nil {
						logx.Errorf("marshal block error, err: %s", err.Error())
						return err
					}
					logx.Errorf("invalid block:  %s", string(blocksInfo))
					return errors.New("invalid block")
				}
			}

			// delete mempool txs
			for _, pendingDeleteMempoolTx := range pendingUpdateMempoolTxs {
				for _, detail := range pendingDeleteMempoolTx.MempoolDetails {
					dbTx := tx.Table(mempool.DetailTableName).Where("id = ?", detail.ID).Delete(&detail)
					if dbTx.Error != nil {
						logx.Errorf("delete tx detail error, err: %s", dbTx.Error.Error())
						return dbTx.Error
					}
					if dbTx.RowsAffected == 0 {
						return errors.New("delete invalid mempool tx")
					}
				}
				dbTx := tx.Table(mempool.MempoolTableName).Where("id = ?", pendingDeleteMempoolTx.ID).Delete(&pendingDeleteMempoolTx)
				if dbTx.Error != nil {
					logx.Errorf("delete mempool tx error, err: %s", dbTx.Error.Error())
					return dbTx.Error
				}
				if dbTx.RowsAffected == 0 {
					return errors.New("delete invalid mempool tx")
				}
			}
			return nil
		},
	)
	return err
}

func (m *defaultL1BlockMonitorModel) CreateGovernanceMonitorInfo(
	blockInfo *L1Block,
	pendingNewL2AssetInfos []*asset.AssetInfo,
	pendingUpdateL2AssetInfos []*asset.AssetInfo,
	pendingNewSysconfigInfos []*sysconfig.Sysconfig,
	pendingUpdateSysconfigInfos []*sysconfig.Sysconfig,
) (err error) {
	err = m.DB.Transaction(
		func(tx *gorm.DB) error { // transact
			// create data for l1 block info
			dbTx := tx.Table(m.table).Create(blockInfo)
			if dbTx.Error != nil {
				return dbTx.Error
			}
			if dbTx.RowsAffected == 0 {
				return errors.New("unable to create l1 block info")
			}
			// create l2 asset info
			if len(pendingNewL2AssetInfos) != 0 {
				dbTx = tx.Table(asset.AssetInfoTableName).CreateInBatches(pendingNewL2AssetInfos, len(pendingNewL2AssetInfos))
				if dbTx.Error != nil {
					return dbTx.Error
				}
				if dbTx.RowsAffected != int64(len(pendingNewL2AssetInfos)) {
					logx.Errorf("the length of created rows doesn't match, created=%d, toCreate=%s", dbTx.RowsAffected, len(pendingNewL2AssetInfos))
					return errors.New("invalid l2 asset info")
				}
			}
			// update l2 asset info
			for _, pendingUpdateL2AssetInfo := range pendingUpdateL2AssetInfos {
				dbTx = tx.Table(asset.AssetInfoTableName).Where("id = ?", pendingUpdateL2AssetInfo.ID).Select("*").Updates(&pendingUpdateL2AssetInfo)
				if dbTx.Error != nil {
					return dbTx.Error
				}
			}
			// create new sys config
			if len(pendingNewSysconfigInfos) != 0 {
				dbTx = tx.Table(sysconfig.TableName).CreateInBatches(pendingNewSysconfigInfos, len(pendingNewSysconfigInfos))
				if dbTx.Error != nil {
					return dbTx.Error
				}
				if dbTx.RowsAffected != int64(len(pendingNewSysconfigInfos)) {
					logx.Errorf("the length of created rows doesn't match, created=%d, toCreate=%s", dbTx.RowsAffected, len(pendingNewSysconfigInfos))
					return errors.New("invalid sys config info")
				}
			}
			// update sys config
			for _, pendingUpdateSysconfigInfo := range pendingUpdateSysconfigInfos {
				dbTx = tx.Table(sysconfig.TableName).Where("id = ?", pendingUpdateSysconfigInfo.ID).Select("*").Updates(&pendingUpdateSysconfigInfo)
				if dbTx.Error != nil {
					return dbTx.Error
				}
			}
			return nil
		},
	)
	return err
}

func (m *defaultL1BlockMonitorModel) GetLatestL1BlockByType(blockType int) (blockInfo *L1Block, err error) {
	dbTx := m.DB.Table(m.table).Where("type = ?", blockType).Order("l1_block_height desc").Find(&blockInfo)
	if dbTx.Error != nil {
		logx.Errorf("get monitor blocks error, err: %s", err.Error())
		return nil, errorcode.DbErrSqlOperation
	}
	if dbTx.RowsAffected == 0 {
		return nil, errorcode.DbErrNotFound
	}
	return blockInfo, nil
}

func (m *defaultL1BlockMonitorModel) GetLatestL1BlockMonitorByGovernance() (blockInfo *L1Block, err error) {
	dbTx := m.DB.Table(m.table).Where("type = ?", MonitorTypeGovernance).Order("l1_block_height desc").Find(&blockInfo)
	if dbTx.Error != nil {
		logx.Errorf("get governance blocks error, err: %s", err.Error())
		return nil, errorcode.DbErrSqlOperation
	}
	if dbTx.RowsAffected == 0 {
		return nil, errorcode.DbErrNotFound
	}
	return blockInfo, nil
}