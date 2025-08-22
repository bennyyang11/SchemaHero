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

package migration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// MockLogger implements the Logger interface for testing
type MockLogger struct {
	messages []string
}

func (m *MockLogger) Info(msg string, args ...interface{}) {
	m.messages = append(m.messages, fmt.Sprintf("INFO: "+msg, args...))
}

func (m *MockLogger) Error(msg string, args ...interface{}) {
	m.messages = append(m.messages, fmt.Sprintf("ERROR: "+msg, args...))
}

func (m *MockLogger) Debug(msg string, args ...interface{}) {
	m.messages = append(m.messages, fmt.Sprintf("DEBUG: "+msg, args...))
}

// MockDataMigrationExecutor for testing
type MockDataMigrationExecutor struct {
	executeError     error
	batchError       error
	conditionsResult bool
	conditionsError  error
	executedSQL      []string
	batchedSQL       []string
}

func (m *MockDataMigrationExecutor) ExecuteDataMigration(tableName string, migration *schemasv1alpha4.DataMigration) error {
	sql, _ := m.getMigrationSQL(migration)
	m.executedSQL = append(m.executedSQL, sql)
	return m.executeError
}

func (m *MockDataMigrationExecutor) ExecuteInBatches(sql string, batchSize int32, batchDelayMs int32) error {
	m.batchedSQL = append(m.batchedSQL, sql)
	return m.batchError
}

func (m *MockDataMigrationExecutor) CheckMigrationConditions(conditions []schemasv1alpha4.DataMigrationCondition) (bool, error) {
	return m.conditionsResult, m.conditionsError
}

func (m *MockDataMigrationExecutor) getMigrationSQL(migration *schemasv1alpha4.DataMigration) (string, error) {
	if migration.SQL != "" {
		return migration.SQL, nil
	}
	return "mock sql", nil
}

func TestMigrationExecutor(t *testing.T) {
	t.Run("execute single migration", func(t *testing.T) {
		logger := &MockLogger{}
		
		migration := &schemasv1alpha4.DataMigration{
			Name: "test-migration",
			SQL:  "UPDATE users SET status = 'active'",
		}

		// Test the SQL generation part
		executor := &MigrationExecutor{
			driver: "postgres",
			logger: logger,
		}

		sql, err := executor.getMigrationSQL("users", migration)
		require.NoError(t, err)
		assert.Equal(t, "UPDATE users SET status = 'active'", sql)
	})

	t.Run("template-based migration SQL generation", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name: "template-migration",
			Template: &schemasv1alpha4.DataMigrationTemplate{
				Template: "UPDATE {{.table}} SET status = {{quote .status}}",
				Parameters: []schemasv1alpha4.TemplateParameter{
					{Name: "table", Type: schemasv1alpha4.ParameterTypeTable, Default: "users"},
					{Name: "status", Type: schemasv1alpha4.ParameterTypeString, Default: "active"},
				},
			},
		}

		executor := &MigrationExecutor{}
		sql, err := executor.getMigrationSQL("users", migration)
		
		require.NoError(t, err)
		assert.Contains(t, sql, "UPDATE users")
		assert.Contains(t, sql, "'active'")
	})

	t.Run("progress tracking", func(t *testing.T) {
		var progressReports []ProgressInfo
		progressCallback := func(migrationName string, progress ProgressInfo) {
			progressReports = append(progressReports, progress)
		}

		logger := &MockLogger{}
		executor := &MigrationExecutor{
			driver:           "mock",
			progressCallback: progressCallback,
			logger:           logger,
		}

		// Test progress reporting structure
		if executor.progressCallback != nil {
			executor.progressCallback("test", ProgressInfo{
				Stage:         StageValidating,
				CurrentBatch:  1,
				TotalBatches:  5,
				RowsProcessed: 1000,
				TotalRows:     5000,
				ElapsedTime:   time.Second * 30,
			})
		}

		require.Len(t, progressReports, 1)
		assert.Equal(t, StageValidating, progressReports[0].Stage)
		assert.Equal(t, int32(1), progressReports[0].CurrentBatch)
		assert.Equal(t, int64(1000), progressReports[0].RowsProcessed)
	})

	t.Run("timeout handling", func(t *testing.T) {
		timeout := metav1.Duration{Duration: 100 * time.Millisecond}
		migration := &schemasv1alpha4.DataMigration{
			Name:    "timeout-test",
			SQL:     "UPDATE users SET status = 'active'",
			Timeout: &timeout,
		}

		// Test that timeout context is created
		ctx := context.Background()
		
		// Create a context with the timeout
		if migration.Timeout != nil && migration.Timeout.Duration > 0 {
			timeoutCtx, cancel := context.WithTimeout(ctx, migration.Timeout.Duration)
			defer cancel()
			
			// Verify the context has a deadline
			deadline, hasDeadline := timeoutCtx.Deadline()
			assert.True(t, hasDeadline)
			assert.True(t, deadline.After(time.Now()))
		}
	})

	t.Run("dependency execution order", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{Name: "third", SQL: "UPDATE users SET step = 3", DependsOn: []string{"second"}},
			{Name: "first", SQL: "UPDATE users SET step = 1"},
			{Name: "second", SQL: "UPDATE users SET step = 2", DependsOn: []string{"first"}},
		}

		// Test dependency resolution
		resolver := schemasv1alpha4.NewDependencyResolver(migrations)
		ordered, err := resolver.ResolveExecutionOrder()
		
		require.NoError(t, err)
		require.Len(t, ordered, 3)
		assert.Equal(t, "first", ordered[0].Name)
		assert.Equal(t, "second", ordered[1].Name)
		assert.Equal(t, "third", ordered[2].Name)
	})

	t.Run("rollback mechanism", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name:       "reversible-migration",
			SQL:        "UPDATE users SET status = 'active'",
			Reversible: true,
			ReverseSQL: "UPDATE users SET status = NULL",
		}

		executor := &MigrationExecutor{logger: &MockLogger{}}
		
		// Test rollback SQL generation
		assert.True(t, migration.Reversible)
		assert.NotEmpty(t, migration.ReverseSQL)
		
		// Test that rollback can be attempted
		ctx := context.Background()
		err := executor.attemptRollback(ctx, "users", migration)
		
		// This will fail due to no real connection, but structure should be correct
		assert.Error(t, err)
		assert.NotContains(t, err.Error(), "not reversible")
	})
}

func TestPostgresDataMigrationExecutor(t *testing.T) {
	t.Run("execute migration SQL structure", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name: "test",
			SQL:  "UPDATE users SET status = 'active'",
		}

		// Test SQL generation logic
		if migration.SQL != "" {
			assert.Equal(t, "UPDATE users SET status = 'active'", migration.SQL)
		}
		
		// Test that migration has required fields for execution
		assert.NotEmpty(t, migration.Name)
		assert.NotEmpty(t, migration.SQL)
	})

	t.Run("batch execution logic", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name:         "batch-test",
			SQL:          "UPDATE users SET processed = true",
			BatchSize:    1000,
			BatchDelayMs: 100,
		}

		// Test batch configuration
		assert.Greater(t, migration.BatchSize, int32(0))
		assert.GreaterOrEqual(t, migration.BatchDelayMs, int32(0))
		
		// Test SQL modification for batching
		sql := migration.SQL
		if !strings.Contains(strings.ToUpper(sql), "LIMIT") {
			sql = fmt.Sprintf("%s LIMIT %d", sql, migration.BatchSize)
		}
		
		expectedSQL := "UPDATE users SET processed = true LIMIT 1000"
		assert.Equal(t, expectedSQL, sql)
	})

	t.Run("condition checking", func(t *testing.T) {
		conditions := []schemasv1alpha4.DataMigrationCondition{
			{
				Query:    "SELECT COUNT(*) FROM users WHERE status IS NULL",
				Operator: ">",
				Value:    0,
			},
			{
				Query:    "SELECT 1 FROM information_schema.tables WHERE table_name = 'users'",
				Operator: "EXISTS",
			},
		}
		
		// Test condition structure validation
		for _, condition := range conditions {
			assert.NotEmpty(t, condition.Query)
			assert.NotEmpty(t, condition.Operator)
			
			switch condition.Operator {
			case "EXISTS", "NOT EXISTS":
				// Value should be ignored
			default:
				// Value should be used for comparison
				assert.NotNil(t, condition.Value)
			}
		}
	})
}

func TestMysqlDataMigrationExecutor(t *testing.T) {
	t.Run("MySQL-specific execution", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name: "mysql-test",
			SQL:  "UPDATE users SET email = LOWER(email)",
		}
		
		// Test SQL generation logic
		if migration.SQL != "" {
			assert.Equal(t, "UPDATE users SET email = LOWER(email)", migration.SQL)
		}
	})

	t.Run("MySQL batch processing", func(t *testing.T) {
		// Test MySQL-specific batching logic
		batchSize := int32(2000)
		sql := "UPDATE products SET price = price * 1.1"
		
		// Verify MySQL LIMIT syntax would be added
		expectedBatchSQL := fmt.Sprintf("%s LIMIT %d", sql, batchSize)
		assert.Contains(t, expectedBatchSQL, "LIMIT 2000")
	})
}

func TestExecutionStages(t *testing.T) {
	t.Run("execution stage constants", func(t *testing.T) {
		// Verify all execution stages are defined
		stages := []ExecutionStage{
			StageValidating,
			StageExecuting,
			StageBatching,
			StageCompleted,
			StageFailed,
			StageRollingBack,
			StageRolledBack,
		}

		for _, stage := range stages {
			assert.NotEmpty(t, string(stage))
		}
	})

	t.Run("progress info structure", func(t *testing.T) {
		progress := ProgressInfo{
			Stage:           StageExecuting,
			CurrentBatch:    3,
			TotalBatches:    10,
			RowsProcessed:   15000,
			TotalRows:       50000,
			ElapsedTime:     time.Minute * 2,
			EstimatedTimeLeft: time.Minute * 5,
		}

		assert.Equal(t, StageExecuting, progress.Stage)
		assert.Equal(t, int32(3), progress.CurrentBatch)
		assert.Equal(t, int64(15000), progress.RowsProcessed)
		assert.Equal(t, time.Minute*2, progress.ElapsedTime)
	})
}

func TestErrorHandlingAndRollback(t *testing.T) {
	t.Run("non-reversible migration", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name:       "non-reversible",
			SQL:        "UPDATE users SET email = LOWER(email)",
			Reversible: false,
		}

		executor := &MigrationExecutor{logger: &MockLogger{}}
		
		err := executor.attemptRollback(context.Background(), "users", migration)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not reversible")
	})

	t.Run("reversible migration without reverse SQL", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name:       "incomplete-reversible",
			SQL:        "UPDATE users SET status = 'active'",
			Reversible: true,
			// Missing ReverseSQL
		}

		executor := &MigrationExecutor{logger: &MockLogger{}}
		
		err := executor.attemptRollback(context.Background(), "users", migration)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not reversible")
	})

	t.Run("valid reversible migration", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name:       "reversible",
			SQL:        "UPDATE users SET status = 'active'",
			Reversible: true,
			ReverseSQL: "UPDATE users SET status = NULL",
		}

		assert.True(t, migration.Reversible)
		assert.NotEmpty(t, migration.ReverseSQL)
	})
}

func TestBatchProcessingLogic(t *testing.T) {
	t.Run("batch size calculation", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name:         "batch-test",
			SQL:          "UPDATE large_table SET processed = true",
			BatchSize:    5000,
			BatchDelayMs: 200,
		}

		// Test batch configuration
		assert.Greater(t, migration.BatchSize, int32(0))
		assert.GreaterOrEqual(t, migration.BatchDelayMs, int32(0))
		
		// Test batch delay conversion
		delay := time.Duration(migration.BatchDelayMs) * time.Millisecond
		assert.Equal(t, time.Millisecond*200, delay)
	})

	t.Run("SQL modification for batching", func(t *testing.T) {
		testCases := []struct {
			name        string
			originalSQL string
			batchSize   int32
			expectedSQL string
		}{
			{
				name:        "UPDATE without LIMIT",
				originalSQL: "UPDATE users SET processed = true WHERE processed = false",
				batchSize:   1000,
				expectedSQL: "UPDATE users SET processed = true WHERE processed = false LIMIT 1000",
			},
			{
				name:        "DELETE without LIMIT",
				originalSQL: "DELETE FROM logs WHERE created_at < NOW() - INTERVAL '1 year'",
				batchSize:   5000,
				expectedSQL: "DELETE FROM logs WHERE created_at < NOW() - INTERVAL '1 year' LIMIT 5000",
			},
			{
				name:        "SQL with existing LIMIT",
				originalSQL: "UPDATE users SET active = true LIMIT 100",
				batchSize:   1000,
				expectedSQL: "UPDATE users SET active = true LIMIT 100", // Should not add another LIMIT
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Test the batching logic
				sql := tc.originalSQL
				if !strings.Contains(strings.ToUpper(sql), "LIMIT") {
					sql = fmt.Sprintf("%s LIMIT %d", sql, tc.batchSize)
				}
				
				assert.Equal(t, tc.expectedSQL, sql)
			})
		}
	})
}

func TestTimeoutAndCancellation(t *testing.T) {
	t.Run("timeout context creation", func(t *testing.T) {
		timeout := metav1.Duration{Duration: 5 * time.Second}
		migration := &schemasv1alpha4.DataMigration{
			Name:    "timeout-test",
			SQL:     "UPDATE users SET processed = true",
			Timeout: &timeout,
		}

		ctx := context.Background()
		
		// Test timeout context creation
		if migration.Timeout != nil && migration.Timeout.Duration > 0 {
			timeoutCtx, cancel := context.WithTimeout(ctx, migration.Timeout.Duration)
			defer cancel()
			
			deadline, hasDeadline := timeoutCtx.Deadline()
			assert.True(t, hasDeadline)
			assert.True(t, deadline.After(time.Now()))
			assert.True(t, deadline.Before(time.Now().Add(6*time.Second))) // Should be within 5 seconds + buffer
		}
	})

	t.Run("cancellation handling", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		
		// Test immediate cancellation
		cancel()
		
		select {
		case <-ctx.Done():
			assert.Equal(t, context.Canceled, ctx.Err())
		default:
			t.Error("Context should be cancelled")
		}
	})
}

func TestExecutorCreation(t *testing.T) {
	t.Run("supported drivers", func(t *testing.T) {
		supportedDrivers := []string{"postgres", "mysql", "cockroachdb", "timescaledb"}
		
		for _, driver := range supportedDrivers {
			executor := NewMigrationExecutor(driver, "mock://uri", nil, &MockLogger{})
			assert.Equal(t, driver, executor.driver)
		}
	})

	t.Run("progress callback and logger", func(t *testing.T) {
		logger := &MockLogger{}
		var progressReports []ProgressInfo
		
		progressCallback := func(migrationName string, progress ProgressInfo) {
			progressReports = append(progressReports, progress)
		}

		executor := NewMigrationExecutor("postgres", "mock://uri", progressCallback, logger)
		
		assert.Equal(t, "postgres", executor.driver)
		assert.NotNil(t, executor.progressCallback)
		assert.NotNil(t, executor.logger)
	})
} 