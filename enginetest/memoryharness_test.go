// Copyright 2020 Liquidata, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enginetest_test

import (
	"github.com/liquidata-inc/go-mysql-server/enginetest"
	"github.com/liquidata-inc/go-mysql-server/memory"
	"github.com/liquidata-inc/go-mysql-server/sql"
)

type memoryHarness struct {
	name                  string
	numTablePartitions    int
	indexDriverInitalizer indexDriverInitalizer
}

func newMemoryHarness(numTablePartitions int, indexDriverInitalizer indexDriverInitalizer) *memoryHarness {
	return &memoryHarness{
		numTablePartitions: numTablePartitions,
		indexDriverInitalizer: indexDriverInitalizer}
}

var _ enginetest.Harness = (*memoryHarness)(nil)
var _ enginetest.IndexDriverHarness = (*memoryHarness)(nil)
var _ enginetest.IndexHarness = (*memoryHarness)(nil)
var _ enginetest.VersionedDBHarness = (*memoryHarness)(nil)

func (m *memoryHarness) SupportsNativeIndexCreation() bool {
	return true
}

func (m *memoryHarness) NewTableAsOf(db sql.VersionedDatabase, name string, schema sql.Schema, asOf interface{}) sql.Table {
	table := memory.NewPartitionedTable(name, schema, m.numTablePartitions)
	db.(*memory.HistoryDatabase).AddTableAsOf(name, table, asOf)
	return table
}

func (m *memoryHarness) IndexDriver(dbs []sql.Database) sql.IndexDriver {
	if m.indexDriverInitalizer != nil {
		return m.indexDriverInitalizer(dbs)
	}
	return nil
}

func (m *memoryHarness) NewDatabase(name string) sql.Database {
	return memory.NewHistoryDatabase(name)
}

func (m *memoryHarness) NewTable(db sql.Database, name string, schema sql.Schema) sql.Table {
	table := memory.NewPartitionedTable(name, schema, m.numTablePartitions)
	db.(*memory.HistoryDatabase).AddTable(name, table)
	return table
}

func (m *memoryHarness) NewContext() *sql.Context {
	panic("implement me")
}

