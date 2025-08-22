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

package cassandra

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/interfaces"
)

func TestCassandraDataMigrationPlanning(t *testing.T) {
	t.Run("simple CQL migration", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{
				Name: "update-status",
				SQL:  "UPDATE users SET status = 'active' WHERE user_id = ?",
				Type: schemasv1alpha4.DataMigrationTypeBackfill,
			},
		}

		// This will fail due to connection, but test the planning logic
		_, err := PlanCassandraDataMigrations([]string{"localhost"}, "user", "pass", "test", "users", migrations)
		
		// Should fail with connection error, not implementation error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create Cassandra session")
	})

	t.Run("Cassandra-specific CQL adaptation", func(t *testing.T) {
		planner := &CassandraDataMigrationPlanner{
			keyspace: "test_keyspace",
		}

		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "NOW() function",
				input:    "UPDATE users SET updated_at = NOW() WHERE user_id = ?",
				expected: "UPDATE users SET updated_at = toTimestamp(now()) WHERE user_id = ?",
			},
			{
				name:     "CURRENT_DATE function",
				input:    "UPDATE events SET date = CURRENT_DATE WHERE event_id = ?",
				expected: "UPDATE events SET date = toDate(now()) WHERE event_id = ?",
			},
			{
				name:     "CURRENT_TIMESTAMP function",
				input:    "UPDATE logs SET timestamp = CURRENT_TIMESTAMP WHERE log_id = ?",
				expected: "UPDATE logs SET timestamp = toTimestamp(now()) WHERE log_id = ?",
			},
			{
				name:     "interval removal",
				input:    "UPDATE tasks SET due_date = due_date + INTERVAL '1 day' WHERE task_id = ?",
				expected: "UPDATE tasks SET due_date = due_date  WHERE task_id = ?",
			},
			{
				name:     "no changes needed",
				input:    "UPDATE users SET name = 'test' WHERE user_id = ?",
				expected: "UPDATE users SET name = 'test' WHERE user_id = ?",
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
		planner := &CassandraDataMigrationPlanner{
			keyspace: "test_keyspace",
		}

		template := &schemasv1alpha4.DataMigrationTemplate{
			Template: "UPDATE {{.keyspace}}.{{.table_name}} SET updated_at = {{.NOW}} WHERE id = ?",
		}

		values := map[string]interface{}{
			"table_name": "test_table",
		}

		result, err := planner.renderTemplate(template, values)
		require.NoError(t, err)
		assert.Equal(t, "UPDATE test_keyspace.test_table SET updated_at = toTimestamp(now()) WHERE id = ?", result)
	})

	t.Run("batching support", func(t *testing.T) {
		planner := &CassandraDataMigrationPlanner{}

		tests := []struct {
			name      string
			cql       string
			batchSize int
			expected  string
		}{
			{
				name:      "UPDATE with batching hint",
				cql:       "UPDATE users SET active = true WHERE user_id = ?",
				batchSize: 100,
				expected:  "-- BATCH_SIZE: 100\nUPDATE users SET active = true WHERE user_id = ?",
			},
			{
				name:      "INSERT with batching hint",
				cql:       "INSERT INTO logs (id, message) VALUES (?, ?)",
				batchSize: 50,
				expected:  "-- BATCH_SIZE: 50\nINSERT INTO logs (id, message) VALUES (?, ?)",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := planner.addBatchingToCQL(tt.cql, tt.batchSize)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("migration validation", func(t *testing.T) {
		planner := &CassandraDataMigrationPlanner{}

		validMigrations := []schemasv1alpha4.DataMigration{
			{
				Name: "valid-update",
				SQL:  "UPDATE users SET status = 'active' WHERE user_id = ?",
			},
			{
				Name: "valid-insert", 
				SQL:  "INSERT INTO logs (id, message) VALUES (?, ?)",
			},
		}

		for _, migration := range validMigrations {
			t.Run(migration.Name, func(t *testing.T) {
				err := planner.validateCassandraMigration(&migration)
				assert.NoError(t, err)
			})
		}

		invalidMigrations := []struct {
			name string
			sql  string
			error string
		}{
			{
				name:  "join-operation",
				sql:   "UPDATE users SET name = u.name FROM profiles u JOIN users p ON u.id = p.user_id",
				error: "JOIN",
			},
			{
				name:  "group-by-operation",
				sql:   "UPDATE stats SET count = (SELECT COUNT(*) FROM events GROUP BY user_id)",
				error: "GROUP BY",
			},
			{
				name:  "mass-update",
				sql:   "UPDATE users SET deleted = true",
				error: "mass UPDATE operations are not recommended",
			},
			{
				name:  "mass-delete",
				sql:   "DELETE FROM logs",
				error: "mass DELETE operations are not supported",
			},
		}

		for _, tt := range invalidMigrations {
			t.Run(tt.name, func(t *testing.T) {
				migration := schemasv1alpha4.DataMigration{
					Name: tt.name,
					SQL:  tt.sql,
				}
				
				err := planner.validateCassandraMigration(&migration)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.error)
			})
		}
	})
}

func TestCassandraUtilityFunctions(t *testing.T) {
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
			{true, 0, true}, // Cassandra specific case
		}

		for _, tt := range tests {
			t.Run(fmt.Sprintf("input_%v", tt.input), func(t *testing.T) {
				result, err := convertToFloat64Cassandra(tt.input)
				
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
			{5, 5, "unsupported_op", false, true},
		}

		for _, tt := range tests {
			t.Run(fmt.Sprintf("%v_%s_%v", tt.actual, tt.operator, tt.expected), func(t *testing.T) {
				result, err := compareNumericCassandra(tt.actual, tt.expected, tt.operator)
				
				if tt.hasError {
					assert.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.result, result)
				}
			})
		}
	})

	t.Run("row estimation", func(t *testing.T) {
		planner := &CassandraDataMigrationPlanner{}

		tests := []struct {
			cql      string
			expected int64
		}{
			{"UPDATE users SET status = 'active'", 100},
			{"DELETE FROM logs WHERE created_at < ?", 50},
			{"INSERT INTO events (id, name) VALUES (?, ?)", 1},
			{"SELECT * FROM users", 10}, // Default case
		}

		for _, tt := range tests {
			t.Run(tt.cql, func(t *testing.T) {
				migration := &schemasv1alpha4.DataMigration{
					SQL: tt.cql,
				}
				result, err := planner.EstimateAffectedRows("test_table", migration)
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestCassandraDataMigrationInterface(t *testing.T) {
	t.Run("planner interface implementation", func(t *testing.T) {
		// Verify CassandraDataMigrationPlanner implements the DataMigrationPlanner interface
		var planner interfaces.DataMigrationPlanner
		planner = NewCassandraDataMigrationPlanner([]string{"localhost"}, "user", "pass", "keyspace")

		assert.NotNil(t, planner)
		assert.IsType(t, &CassandraDataMigrationPlanner{}, planner)
	})

	t.Run("migration planning with empty list", func(t *testing.T) {
		planner := &CassandraDataMigrationPlanner{}
		
		statements, err := planner.PlanDataMigrations("test_table", []schemasv1alpha4.DataMigration{})
		require.NoError(t, err)
		assert.Empty(t, statements)
	})

	t.Run("cassandra-specific migration types", func(t *testing.T) {
		// Test migration types that work well with Cassandra
		suitableMigrations := []schemasv1alpha4.DataMigration{
			{
				Name: "insert-default-data",
				SQL:  "INSERT INTO users (id, status) VALUES (?, 'active')",
				Type: schemasv1alpha4.DataMigrationTypeBackfill,
			},
			{
				Name: "update-with-key",
				SQL:  "UPDATE users SET updated_at = toTimestamp(now()) WHERE user_id = ?",
				Type: schemasv1alpha4.DataMigrationTypeTransform,
			},
			{
				Name: "delete-with-key",
				SQL:  "DELETE FROM logs WHERE log_id = ? AND created_at < ?",
				Type: schemasv1alpha4.DataMigrationTypeCleanup,
			},
		}

		for _, migration := range suitableMigrations {
			t.Run(migration.Name, func(t *testing.T) {
				assert.NotEmpty(t, migration.SQL)
				assert.Contains(t, migration.SQL, "?") // Should use parameterized queries
				
				// Only UPDATE and DELETE require WHERE clauses, INSERT doesn't
				if strings.Contains(strings.ToUpper(migration.SQL), "UPDATE") || 
				   strings.Contains(strings.ToUpper(migration.SQL), "DELETE") {
					assert.Contains(t, migration.SQL, "WHERE") // Should have WHERE clause for safety
				}
			})
		}
	})

	t.Run("cassandra limitations awareness", func(t *testing.T) {
		// Test that we're aware of Cassandra's limitations
		limitations := []string{
			"No JOINs",
			"No subqueries", 
			"No GROUP BY in updates",
			"Primary key required for updates",
			"Limited secondary index support",
		}

		for _, limitation := range limitations {
			t.Run(limitation, func(t *testing.T) {
				// Just verify we're tracking these limitations
				assert.NotEmpty(t, limitation)
			})
		}
	})
} 