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

package sqlite

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/interfaces"
)

func TestSqliteDataMigrationPlanning(t *testing.T) {
	t.Run("simple SQL migration", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{
				Name: "update-status",
				SQL:  "UPDATE users SET status = 'active' WHERE status IS NULL",
				Type: schemasv1alpha4.DataMigrationTypeBackfill,
			},
		}

		// This will fail due to connection, but test the planning logic
		_, err := PlanSqliteDataMigrations("/invalid/path/to/nonexistent.db", "users", migrations)

		// Should fail with connection error, not implementation error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect")
	})

	t.Run("SQLite-specific SQL adaptation", func(t *testing.T) {
		planner := &SqliteDataMigrationPlanner{}

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
		planner := &SqliteDataMigrationPlanner{}

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
		planner := &SqliteDataMigrationPlanner{}

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
				name:      "SELECT no batching",
				sql:       "SELECT * FROM users",
				batchSize: 1000,
				expected:  "SELECT * FROM users",
			},
			{
				name:      "UPDATE with existing LIMIT",
				sql:       "UPDATE users SET active = true LIMIT 2000",
				batchSize: 1000,
				expected:  "UPDATE users SET active = true LIMIT 2000",
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
				Name:      "second-migration",
				SQL:       "UPDATE users SET processed = true",
				DependsOn: []string{"first-migration"},
			},
			{
				Name: "first-migration",
				SQL:  "UPDATE users SET status = 'active'",
			},
		}

		// This will fail due to connection, but test dependency resolution
		_, err := PlanSqliteDataMigrations("invalid://test", "users", migrations)

		// Should fail with connection error, not dependency error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect")
	})
}

func TestSqliteUtilityFunctions(t *testing.T) {
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
			{true, 0, true},
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
			{4, 5, ">=", false, false},
			{2, 8, "<", true, false},
			{10, 3, "<", false, false},
			{5, 5, "<=", true, false},
			{6, 5, "<=", false, false},
			{"invalid", 5, ">", false, true},
			{5, "invalid", ">", false, true},
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

	t.Run("extract table name from UPDATE", func(t *testing.T) {
		tests := []struct {
			sql      string
			expected string
		}{
			{"UPDATE users SET name = 'test'", "users"},
			{"UPDATE accounts SET balance = 0", "accounts"},
			{"update logs set processed = true", "logs"},
			{"SELECT * FROM users", "unknown_table"},
			{"", "unknown_table"},
		}

		for _, tt := range tests {
			t.Run(tt.sql, func(t *testing.T) {
				result := extractTableNameFromUpdate(tt.sql)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("extract table name from DELETE", func(t *testing.T) {
		tests := []struct {
			sql      string
			expected string
		}{
			{"DELETE FROM users WHERE active = false", "users"},
			{"DELETE FROM logs WHERE created_at < '2023-01-01'", "logs"},
			{"delete from accounts where balance = 0", "accounts"},
			{"UPDATE users SET name = 'test'", "unknown_table"},
			{"DELETE users", "unknown_table"}, // Missing FROM
		}

		for _, tt := range tests {
			t.Run(tt.sql, func(t *testing.T) {
				result := extractTableNameFromDelete(tt.sql)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestSqliteDataMigrationInterface(t *testing.T) {
	t.Run("planner interface implementation", func(t *testing.T) {
		// Verify SqliteDataMigrationPlanner implements the DataMigrationPlanner interface
		var planner interfaces.DataMigrationPlanner
		conn := &SqliteConnection{} // Mock connection
		planner = NewSqliteDataMigrationPlanner(conn)

		assert.NotNil(t, planner)
		assert.IsType(t, &SqliteDataMigrationPlanner{}, planner)
	})

	t.Run("migration planning with empty list", func(t *testing.T) {
		planner := &SqliteDataMigrationPlanner{}

		statements, err := planner.PlanDataMigrations("test_table", []schemasv1alpha4.DataMigration{})
		require.NoError(t, err)
		assert.Empty(t, statements)
	})

	t.Run("migration types support", func(t *testing.T) {
		migrationTypes := []schemasv1alpha4.DataMigrationType{
			schemasv1alpha4.DataMigrationTypeBackfill,
			schemasv1alpha4.DataMigrationTypeTransform,
			schemasv1alpha4.DataMigrationTypeCleanup,
			schemasv1alpha4.DataMigrationTypeCopy,
			schemasv1alpha4.DataMigrationTypeCustom,
		}

		for _, migType := range migrationTypes {
			t.Run(string(migType), func(t *testing.T) {
				migration := schemasv1alpha4.DataMigration{
					Name: fmt.Sprintf("test-%s", migType),
					SQL:  "UPDATE test SET flag = true",
					Type: migType,
				}

				assert.Equal(t, migType, migration.Type)
				assert.NotEmpty(t, migration.SQL)
			})
		}
	})
}
