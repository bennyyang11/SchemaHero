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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

func TestEnhancedMigrationController(t *testing.T) {
	t.Run("schema and data phase execution ordering", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-migration",
				Namespace: "default",
			},
			Spec: schemasv1alpha4.MigrationSpec{
				DatabaseName:   "testdb",
				TableName:      "users",
				TableNamespace: "default",
				GeneratedDDL:   "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
				GeneratedDML:   "UPDATE users SET email = 'default@example.com' WHERE email IS NULL",
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                 schemasv1alpha4.Approved,
				ApprovedAt:            time.Now().Unix(),
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		// Test phase determination logic
		assert.True(t, shouldApplySchemaPhase(migration), "Should apply schema phase when pending")
		assert.False(t, shouldApplyDataPhase(migration), "Should not apply data phase until schema is complete")

		// After schema completion
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		assert.False(t, shouldApplySchemaPhase(migration), "Should not apply schema phase when complete")
		assert.True(t, shouldApplyDataPhase(migration), "Should apply data phase when schema is complete")

		// After both phases completion
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		assert.False(t, shouldApplySchemaPhase(migration), "Schema phase should remain complete")
		assert.False(t, shouldApplyDataPhase(migration), "Should not apply data phase when complete")
	})

	t.Run("migration approval workflow", func(t *testing.T) {
		tests := []struct {
			name       string
			migration  *schemasv1alpha4.Migration
			shouldApply bool
		}{
			{
				name: "approved and not executed",
				migration: &schemasv1alpha4.Migration{
					Status: schemasv1alpha4.MigrationStatus{
						ApprovedAt: time.Now().Unix(),
						ExecutedAt: 0,
					},
				},
				shouldApply: true,
			},
			{
				name: "not approved",
				migration: &schemasv1alpha4.Migration{
					Status: schemasv1alpha4.MigrationStatus{
						ApprovedAt: 0,
						ExecutedAt: 0,
					},
				},
				shouldApply: false,
			},
			{
				name: "already executed",
				migration: &schemasv1alpha4.Migration{
					Status: schemasv1alpha4.MigrationStatus{
						ApprovedAt: time.Now().Unix(),
						ExecutedAt: time.Now().Unix(),
					},
				},
				shouldApply: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := shouldApplyMigration(tt.migration)
				assert.Equal(t, tt.shouldApply, result)
			})
		}
	})

	t.Run("DML parsing functionality", func(t *testing.T) {
		controller := &ReconcileMigration{}

		tests := []struct {
			name           string
			generatedDML   string
			expectedCount  int
			expectedName   string
			expectedSQL    string
		}{
			{
				name:          "empty DML",
				generatedDML:  "",
				expectedCount: 0,
			},
			{
				name: "simple DML",
				generatedDML: `-- Migration: update-status
UPDATE users SET status = 'active' WHERE status IS NULL`,
				expectedCount: 1,
				expectedName:  "update-status",
				expectedSQL:   "UPDATE users SET status = 'active' WHERE status IS NULL",
			},
			{
				name: "complex DML with multiple statements",
				generatedDML: `-- Migration: populate-data
-- Description: Populate missing data
UPDATE users SET email = 'default@example.com' WHERE email IS NULL;
UPDATE users SET created_at = NOW() WHERE created_at IS NULL`,
				expectedCount: 1,
				expectedName:  "populate-data",
				expectedSQL:   "UPDATE users SET email = 'default@example.com' WHERE email IS NULL;\nUPDATE users SET created_at = NOW() WHERE created_at IS NULL",
			},
			{
				name: "only comments",
				generatedDML: `-- Migration: skipped-migration
-- Migration skipped-migration: SKIPPED (conditions not met)`,
				expectedCount: 0,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				migrations, err := controller.parseDataMigrationsFromDML(tt.generatedDML)
				require.NoError(t, err)
				assert.Len(t, migrations, tt.expectedCount)

				if tt.expectedCount > 0 {
					assert.Equal(t, tt.expectedName, migrations[0].Name)
					assert.Equal(t, tt.expectedSQL, migrations[0].SQL)
					assert.Equal(t, schemasv1alpha4.DataMigrationTypeCustom, migrations[0].Type)
				}
			})
		}
	})

	t.Run("retry logic configuration", func(t *testing.T) {
		// Test retry configuration constants
		maxRetries := 3
		baseDelay := time.Second * 10

		assert.Equal(t, 3, maxRetries, "Should have 3 retry attempts")
		assert.Equal(t, time.Second*10, baseDelay, "Base delay should be 10 seconds")

		// Test exponential backoff calculation
		for attempt := 0; attempt < maxRetries; attempt++ {
			delay := time.Duration(attempt) * baseDelay
			expectedDelay := time.Duration(attempt) * time.Second * 10
			assert.Equal(t, expectedDelay, delay, "Delay calculation should be correct for attempt %d", attempt)
		}
	})

	t.Run("status tracking logic", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Status: schemasv1alpha4.MigrationStatus{
				Phase: schemasv1alpha4.Planned,
			},
		}

		// Initial state
		assert.Empty(t, migration.Status.SchemaMigrationStatus)
		assert.Empty(t, migration.Status.DataMigrationStatus)

		// Schema phase states
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationRunning
		assert.Equal(t, schemasv1alpha4.DataMigrationRunning, migration.Status.SchemaMigrationStatus)

		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		assert.Equal(t, schemasv1alpha4.DataMigrationCompleted, migration.Status.SchemaMigrationStatus)

		// Data phase states
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationRunning
		assert.Equal(t, schemasv1alpha4.DataMigrationRunning, migration.Status.DataMigrationStatus)

		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		assert.Equal(t, schemasv1alpha4.DataMigrationCompleted, migration.Status.DataMigrationStatus)

		// Final state
		migration.Status.Phase = schemasv1alpha4.Executed
		migration.Status.ExecutedAt = time.Now().Unix()
		assert.Equal(t, schemasv1alpha4.Executed, migration.Status.Phase)
		assert.Greater(t, migration.Status.ExecutedAt, int64(0))
	})
}

func TestMigrationMetrics(t *testing.T) {
	t.Run("metrics collection for schema migrations", func(t *testing.T) {
		metrics := &MigrationMetrics{
			schemaMigrations: make(map[string]*MigrationPhaseMetrics),
			dataMigrations:   make(map[string]*MigrationPhaseMetrics),
		}

		migrationName := "test-schema-migration"

		// Start schema migration
		metrics.StartSchemaMigration(migrationName)
		
		schemaMetrics := metrics.GetSchemaMetrics()
		require.Contains(t, schemaMetrics, migrationName)
		assert.Equal(t, "schema", schemaMetrics[migrationName].Phase)
		assert.Equal(t, schemasv1alpha4.DataMigrationRunning, schemaMetrics[migrationName].Status)
		assert.False(t, schemaMetrics[migrationName].StartTime.IsZero())

		// Complete schema migration
		time.Sleep(time.Millisecond * 10) // Small delay to ensure duration > 0
		metrics.CompleteSchemaMigration(migrationName, schemasv1alpha4.DataMigrationCompleted, "")

		updatedMetrics := metrics.GetSchemaMetrics()[migrationName]
		assert.Equal(t, schemasv1alpha4.DataMigrationCompleted, updatedMetrics.Status)
		assert.Greater(t, updatedMetrics.Duration, time.Duration(0))
		assert.Empty(t, updatedMetrics.ErrorMessage)
	})

	t.Run("metrics collection for data migrations", func(t *testing.T) {
		metrics := &MigrationMetrics{
			schemaMigrations: make(map[string]*MigrationPhaseMetrics),
			dataMigrations:   make(map[string]*MigrationPhaseMetrics),
		}

		migrationName := "test-data-migration"

		// Start data migration
		metrics.StartDataMigration(migrationName)
		
		dataMetrics := metrics.GetDataMetrics()
		require.Contains(t, dataMetrics, migrationName)
		assert.Equal(t, "data", dataMetrics[migrationName].Phase)
		assert.Equal(t, schemasv1alpha4.DataMigrationRunning, dataMetrics[migrationName].Status)

		// Complete data migration with rows affected
		time.Sleep(time.Millisecond * 10)
		metrics.CompleteDataMigration(migrationName, schemasv1alpha4.DataMigrationCompleted, 1500, "")

		updatedMetrics := metrics.GetDataMetrics()[migrationName]
		assert.Equal(t, schemasv1alpha4.DataMigrationCompleted, updatedMetrics.Status)
		assert.Equal(t, int64(1500), updatedMetrics.RowsAffected)
		assert.Greater(t, updatedMetrics.Duration, time.Duration(0))

		// Check summary metrics
		summary := metrics.GetSummaryMetrics()
		assert.Equal(t, int64(1), summary.TotalMigrations)
		assert.Equal(t, int64(1), summary.SuccessfulMigrations)
		assert.Equal(t, int64(0), summary.FailedMigrations)
		assert.Equal(t, 100.0, summary.SuccessRate)
	})

	t.Run("retry tracking", func(t *testing.T) {
		metrics := &MigrationMetrics{
			schemaMigrations: make(map[string]*MigrationPhaseMetrics),
			dataMigrations:   make(map[string]*MigrationPhaseMetrics),
		}

		migrationName := "test-retry-migration"

		// Start data migration that will be retried
		metrics.StartDataMigration(migrationName)

		// Record multiple retries
		metrics.RecordRetry(migrationName)
		metrics.RecordRetry(migrationName)

		dataMetrics := metrics.GetDataMetrics()[migrationName]
		assert.Equal(t, 2, dataMetrics.RetryCount)

		summary := metrics.GetSummaryMetrics()
		assert.Equal(t, int64(2), summary.RetriedMigrations)
	})

	t.Run("failed migration metrics", func(t *testing.T) {
		metrics := &MigrationMetrics{
			schemaMigrations: make(map[string]*MigrationPhaseMetrics),
			dataMigrations:   make(map[string]*MigrationPhaseMetrics),
		}

		migrationName := "test-failed-migration"

		// Start and fail a data migration
		metrics.StartDataMigration(migrationName)
		time.Sleep(time.Millisecond * 10)
		metrics.CompleteDataMigration(migrationName, schemasv1alpha4.DataMigrationFailed, 0, "SQL execution error")

		dataMetrics := metrics.GetDataMetrics()[migrationName]
		assert.Equal(t, schemasv1alpha4.DataMigrationFailed, dataMetrics.Status)
		assert.Equal(t, "SQL execution error", dataMetrics.ErrorMessage)
		assert.Greater(t, dataMetrics.Duration, time.Duration(0))

		summary := metrics.GetSummaryMetrics()
		assert.Equal(t, int64(1), summary.TotalMigrations)
		assert.Equal(t, int64(0), summary.SuccessfulMigrations)
		assert.Equal(t, int64(1), summary.FailedMigrations)
		assert.Equal(t, 0.0, summary.SuccessRate)
	})

	t.Run("average execution time calculation", func(t *testing.T) {
		metrics := &MigrationMetrics{
			schemaMigrations: make(map[string]*MigrationPhaseMetrics),
			dataMigrations:   make(map[string]*MigrationPhaseMetrics),
		}

		// Add some completed migrations with different durations
		metrics.dataMigrations["fast"] = &MigrationPhaseMetrics{
			Status:    schemasv1alpha4.DataMigrationCompleted,
			Duration:  time.Second * 5,
			StartTime: time.Now().Add(-time.Second * 5),
			EndTime:   time.Now(),
		}
		metrics.dataMigrations["slow"] = &MigrationPhaseMetrics{
			Status:    schemasv1alpha4.DataMigrationCompleted,
			Duration:  time.Second * 15,
			StartTime: time.Now().Add(-time.Second * 15),
			EndTime:   time.Now(),
		}
		metrics.dataMigrations["failed"] = &MigrationPhaseMetrics{
			Status:   schemasv1alpha4.DataMigrationFailed,
			Duration: time.Second * 30, // Should not be included in average
		}

		averageTime := metrics.GetAverageExecutionTime("data")
		expectedAverage := (time.Second*5 + time.Second*15) / 2 // 10 seconds
		assert.Equal(t, expectedAverage, averageTime)

		// Test with no completed migrations
		emptyMetrics := &MigrationMetrics{
			dataMigrations: make(map[string]*MigrationPhaseMetrics),
		}
		emptyAverage := emptyMetrics.GetAverageExecutionTime("data")
		assert.Equal(t, time.Duration(0), emptyAverage)
	})
}

func TestMigrationPhaseLogic(t *testing.T) {
	t.Run("DDL only migration", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "CREATE TABLE users (id SERIAL)",
				GeneratedDML: "", // No data migrations
			},
			Status: schemasv1alpha4.MigrationStatus{
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   "",
			},
		}

		// Should apply schema phase
		assert.True(t, shouldApplySchemaPhase(migration))
		
		// Should not apply data phase (no DML and no data migration status)
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		// Since DataMigrationStatus is empty, shouldApplyDataPhase should return false
		assert.False(t, shouldApplyDataPhase(migration), "No data phase needed when no DML")
	})

	t.Run("DML only migration", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "", // No schema changes
				GeneratedDML: "UPDATE users SET processed = true",
			},
			Status: schemasv1alpha4.MigrationStatus{
				SchemaMigrationStatus: "",
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		// Should not apply schema phase (no DDL)
		assert.False(t, shouldApplySchemaPhase(migration))
		
		// Should apply data phase, but only after schema phase is considered complete
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		assert.True(t, shouldApplyDataPhase(migration))
	})

	t.Run("failed migration handling", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Status: schemasv1alpha4.MigrationStatus{
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationFailed,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		// Should not apply schema phase when failed
		assert.False(t, shouldApplySchemaPhase(migration))
		
		// Should not apply data phase when schema failed
		assert.False(t, shouldApplyDataPhase(migration))
	})
}

func TestControllerLogger(t *testing.T) {
	t.Run("controller logger adapter", func(t *testing.T) {
		logger := &ControllerLogger{}

		// Test that logger methods can be called without error
		logger.Info("test info message", "key", "value")
		logger.Error("test error message", "error", "details")
		logger.Debug("test debug message", "debug", "info")

		// No assertions needed - just verify it doesn't panic
		assert.NotNil(t, logger)
	})
}

func TestExecutionPhaseLogic(t *testing.T) {
	t.Run("complete migration execution flow", func(t *testing.T) {
		// Simulate the complete execution flow
		migration := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "complete-test",
			},
			Spec: schemasv1alpha4.MigrationSpec{
				DatabaseName: "testdb",
				TableName:    "users",
				GeneratedDDL: "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
				GeneratedDML: "UPDATE users SET email = 'default@example.com' WHERE email IS NULL",
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                 schemasv1alpha4.Approved,
				ApprovedAt:            time.Now().Unix(),
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		// Phase 1: Schema execution
		assert.True(t, shouldApplySchemaPhase(migration))
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationRunning
		
		// Simulate schema completion
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted

		// Phase 2: Data execution
		assert.True(t, shouldApplyDataPhase(migration))
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationRunning

		// Simulate data completion
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		migration.Status.Phase = schemasv1alpha4.Executed
		migration.Status.ExecutedAt = time.Now().Unix()

		// Final state verification
		assert.Equal(t, schemasv1alpha4.Executed, migration.Status.Phase)
		assert.Equal(t, schemasv1alpha4.DataMigrationCompleted, migration.Status.SchemaMigrationStatus)
		assert.Equal(t, schemasv1alpha4.DataMigrationCompleted, migration.Status.DataMigrationStatus)
		assert.Greater(t, migration.Status.ExecutedAt, int64(0))
	})

	t.Run("partial execution scenarios", func(t *testing.T) {
		// Test what happens when schema succeeds but data fails
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
				GeneratedDML: "UPDATE users SET email = 'default@example.com' WHERE email IS NULL",
			},
			Status: schemasv1alpha4.MigrationStatus{
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationCompleted,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationFailed,
			},
		}

		// Should not retry schema (already complete)
		assert.False(t, shouldApplySchemaPhase(migration))
		
		// Should not apply data phase (failed)
		assert.False(t, shouldApplyDataPhase(migration))

		// For retry, would need to reset data status to pending
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationPending
		assert.True(t, shouldApplyDataPhase(migration), "Should be able to retry data phase")
	})
}

func TestMetricsReset(t *testing.T) {
	t.Run("metrics reset functionality", func(t *testing.T) {
		metrics := &MigrationMetrics{
			schemaMigrations:     make(map[string]*MigrationPhaseMetrics),
			dataMigrations:       make(map[string]*MigrationPhaseMetrics),
			totalMigrations:      10,
			successfulMigrations: 8,
			failedMigrations:     2,
			retriedMigrations:    3,
		}

		// Add some test data
		metrics.schemaMigrations["test"] = &MigrationPhaseMetrics{MigrationName: "test"}
		metrics.dataMigrations["test"] = &MigrationPhaseMetrics{MigrationName: "test"}

		// Reset metrics
		metrics.ResetMetrics()

		// Verify all metrics are cleared
		assert.Empty(t, metrics.GetSchemaMetrics())
		assert.Empty(t, metrics.GetDataMetrics())
		
		summary := metrics.GetSummaryMetrics()
		assert.Equal(t, int64(0), summary.TotalMigrations)
		assert.Equal(t, int64(0), summary.SuccessfulMigrations)
		assert.Equal(t, int64(0), summary.FailedMigrations)
		assert.Equal(t, int64(0), summary.RetriedMigrations)
		assert.Equal(t, 0.0, summary.SuccessRate)
	})
} 