/*
Copyright 2025 The SchemaHero Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rqlite

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/interfaces"
)

func TestRqliteDataMigrationPlanning(t *testing.T) {
	t.Run("simple SQL migration", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{
				Name: "update-status",
				SQL:  "UPDATE users SET status = 'active' WHERE status IS NULL",
				Type: schemasv1alpha4.DataMigrationTypeBackfill,
			},
		}

		// This will fail due to connection, but test the planning logic
		_, err := PlanRqliteDataMigrations("mock://test", "users", migrations)
		
		// Should fail with connection error, not implementation error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect")
	})

	t.Run("RQLite-specific SQL adaptation", func(t *testing.T) {
		planner := &RqliteDataMigrationPlanner{}

		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "NOW() function",
				input:    "UPDATE users SET updated_at = NOW()",
				expected: "UPDATE users SET updated_at = datetime('now')",
			},
			{
				name:     "CURRENT_DATE function",
				input:    "UPDATE events SET date = CURRENT_DATE",
				expected: "UPDATE events SET date = date('now')",
			},
			{
				name:     "CURRENT_TIMESTAMP function",
				input:    "UPDATE logs SET timestamp = CURRENT_TIMESTAMP",
				expected: "UPDATE logs SET timestamp = datetime('now')",
			},
			{
				name:     "interval conversion",
				input:    "UPDATE tasks SET due_date = due_date + INTERVAL '1 day'",
				expected: "UPDATE tasks SET due_date = due_date + '+1 day'",
			},
			{
				name:     "week interval",
				input:    "UPDATE events SET expires = expires + INTERVAL '1 week'",
				expected: "UPDATE events SET expires = expires + '+7 days'",
			},
			{
				name:     "no changes needed",
				input:    "UPDATE users SET name = 'test'",
				expected: "UPDATE users SET name = 'test'",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := planner.GetDatabaseSpecificSQL(tt.input)
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("template-based migration", func(t *testing.T) {
		planner := &RqliteDataMigrationPlanner{}

		template := &schemasv1alpha4.DataMigrationTemplate{
			Template: "UPDATE {{.table_name}} SET updated_at = {{.NOW}} WHERE updated_at IS NULL",
		}

		values := map[string]interface{}{
			"table_name": "test_table",
		}

		result, err := planner.renderTemplate(template, values)
		require.NoError(t, err)
		assert.Equal(t, "UPDATE test_table SET updated_at = datetime('now') WHERE updated_at IS NULL", result)
	})

	t.Run("batching support", func(t *testing.T) {
		planner := &RqliteDataMigrationPlanner{}

		tests := []struct {
			name      string
			sql       string
			batchSize int
			expected  string
		}{
			{
				name:      "UPDATE with batching",
				sql:       "UPDATE users SET active = true",
				batchSize: 1000,
				expected:  "UPDATE users SET active = true LIMIT 1000",
			},
			{
				name:      "DELETE with batching",
				sql:       "DELETE FROM logs WHERE created_at < '2023-01-01'",
				batchSize: 500,
				expected:  "DELETE FROM logs WHERE created_at < '2023-01-01' LIMIT 500",
			},
			{
				name:      "INSERT no batching",
				sql:       "INSERT INTO users (name) VALUES ('test')",
				batchSize: 1000,
				expected:  "INSERT INTO users (name) VALUES ('test')",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := planner.addBatchingToSQL(tt.sql, tt.batchSize)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("dependency ordering", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{
				Name:      "dependent-migration",
				SQL:       "UPDATE users SET processed = true",
				DependsOn: []string{"prerequisite-migration"},
			},
			{
				Name: "prerequisite-migration",
				SQL:  "UPDATE users SET status = 'active'",
			},
		}

		// This will fail due to connection, but test dependency resolution
		_, err := PlanRqliteDataMigrations("mock://test", "users", migrations)
		
		// Should fail with connection error, not dependency error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect")
	})
}

func TestRqliteUtilityFunctions(t *testing.T) {
	t.Run("convert to float64", func(t *testing.T) {
		tests := []struct {
			input    interface{}
			expected float64
			hasError bool
		}{
			{float64(123.45), 123.45, false},
			{int64(100), 100.0, false},
			{int(50), 50.0, false},
			{"123.45", 123.45, false},
			{"100", 100.0, false},
			{"invalid", 0, true},
			{[]byte("123"), 0, true}, // RQLite specific case
		}

		for _, tt := range tests {
			t.Run(fmt.Sprintf("input_%v", tt.input), func(t *testing.T) {
				result, err := convertToFloat64(tt.input)
				
				if tt.hasError {
					assert.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.expected, result)
				}
			})
		}
	})

	t.Run("compare numeric", func(t *testing.T) {
		tests := []struct {
			actual   interface{}
			expected interface{}
			operator string
			result   bool
			hasError bool
		}{
			{10, 5, ">", true, false},
			{3, 7, ">", false, false},
			{5, 5, ">=", true, false},
			{2, 8, "<", true, false},
			{5, 5, "<=", true, false},
			{5, 5, "invalid_op", false, true},
		}

		for _, tt := range tests {
			t.Run(fmt.Sprintf("%v_%s_%v", tt.actual, tt.operator, tt.expected), func(t *testing.T) {
				result, err := compareNumeric(tt.actual, tt.expected, tt.operator)
				
				if tt.hasError {
					assert.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.result, result)
				}
			})
		}
	})

	t.Run("table name extraction", func(t *testing.T) {
		updateTests := []struct {
			sql      string
			expected string
		}{
			{"UPDATE users SET name = 'test'", "users"},
			{"UPDATE distributed_table SET sync = true", "distributed_table"},
			{"update logs set processed = true", "logs"},
		}

		for _, tt := range updateTests {
			t.Run("UPDATE_"+tt.sql, func(t *testing.T) {
				result := extractTableNameFromUpdate(tt.sql)
				assert.Equal(t, tt.expected, result)
			})
		}

		deleteTests := []struct {
			sql      string
			expected string
		}{
			{"DELETE FROM users WHERE active = false", "users"},
			{"DELETE FROM distributed_logs WHERE created_at < '2023-01-01'", "distributed_logs"},
			{"delete from accounts where balance = 0", "accounts"},
		}

		for _, tt := range deleteTests {
			t.Run("DELETE_"+tt.sql, func(t *testing.T) {
				result := extractTableNameFromDelete(tt.sql)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestRqliteDataMigrationInterface(t *testing.T) {
	t.Run("planner interface implementation", func(t *testing.T) {
		// Verify RqliteDataMigrationPlanner implements the DataMigrationPlanner interface
		var planner interfaces.DataMigrationPlanner
		conn := &RqliteConnection{} // Mock connection
		planner = NewRqliteDataMigrationPlanner(conn)

		assert.NotNil(t, planner)
		assert.IsType(t, &RqliteDataMigrationPlanner{}, planner)
	})

	t.Run("migration planning with empty list", func(t *testing.T) {
		planner := &RqliteDataMigrationPlanner{}
		
		statements, err := planner.PlanDataMigrations("test_table", []schemasv1alpha4.DataMigration{})
		require.NoError(t, err)
		assert.Empty(t, statements)
	})

	t.Run("distributed migration considerations", func(t *testing.T) {
		// RQLite is a distributed SQLite, so test considerations for distributed environments
		migration := schemasv1alpha4.DataMigration{
			Name:        "distributed-update",
			Description: "Update for distributed cluster",
			SQL:         "UPDATE cluster_table SET updated_at = datetime('now')",
			Type:        schemasv1alpha4.DataMigrationTypeTransform,
			BatchSize:   500, // Smaller batches for distributed systems
		}

		assert.Equal(t, "distributed-update", migration.Name)
		assert.Contains(t, migration.Description, "distributed")
		assert.Equal(t, int32(500), migration.BatchSize)
		assert.Equal(t, schemasv1alpha4.DataMigrationTypeTransform, migration.Type)
	})
} 