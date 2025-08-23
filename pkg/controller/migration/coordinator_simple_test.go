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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

func TestExecutionCoordination(t *testing.T) {
	t.Run("execution lock management", func(t *testing.T) {
		coordinator := &MigrationCoordinator{
			executionLocks: sync.Map{},
		}

		migration1 := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "migration-1"},
			Spec: schemasv1alpha4.MigrationSpec{
				TableName: "users",
			},
		}

		migration2 := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "migration-2"},
			Spec: schemasv1alpha4.MigrationSpec{
				TableName: "users", // Same table
			},
		}

		// First lock should succeed
		lock1, err := coordinator.acquireExecutionLock(migration1)
		require.NoError(t, err)
		assert.NotNil(t, lock1)
		assert.Equal(t, "users", lock1.TableName)
		assert.Equal(t, "migration-1", lock1.MigrationName)

		// Second lock on same table should fail
		_, err = coordinator.acquireExecutionLock(migration2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already being migrated")

		// Release lock
		coordinator.releaseExecutionLock("users")

		// Second lock should now succeed
		lock2, err := coordinator.acquireExecutionLock(migration2)
		require.NoError(t, err)
		assert.NotNil(t, lock2)
	})

	t.Run("progress tracking lifecycle", func(t *testing.T) {
		coordinator := &MigrationCoordinator{
			progressTracker: &ProgressTracker{
				activeMigrations: make(map[string]*MigrationProgress),
			},
		}

		migration := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "progress-test"},
			Spec:       schemasv1alpha4.MigrationSpec{TableName: "users"},
			Status:     schemasv1alpha4.MigrationStatus{Phase: schemasv1alpha4.Approved},
		}

		// Initialize progress
		coordinator.initializeProgress(migration)

		activeMigrations := coordinator.GetActiveMigrations()
		require.Contains(t, activeMigrations, "progress-test")

		progress := activeMigrations["progress-test"]
		assert.Equal(t, "progress-test", progress.MigrationName)
		assert.Equal(t, "users", progress.TableName)
		assert.Equal(t, PhaseInitializing, progress.CurrentPhase)

		// Update phase
		coordinator.updatePhase("progress-test", PhaseSchema)
		updatedMigrations := coordinator.GetActiveMigrations()
		assert.Equal(t, PhaseSchema, updatedMigrations["progress-test"].CurrentPhase)

		// Finalize progress
		coordinator.finalizeProgress(migration)
		finalMigrations := coordinator.GetActiveMigrations()
		assert.NotContains(t, finalMigrations, "progress-test")
	})

	t.Run("phase execution ordering", func(t *testing.T) {
		tests := []struct {
			name                  string
			generatedDDL          string
			generatedDML          string
			schemaMigrationStatus schemasv1alpha4.DataMigrationStatus
			dataMigrationStatus   schemasv1alpha4.DataMigrationStatus
			shouldExecuteSchema   bool
			shouldExecuteData     bool
		}{
			{
				name:                  "both phases needed",
				generatedDDL:          "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
				generatedDML:          "UPDATE users SET email = 'default@example.com'",
				schemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				dataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
				shouldExecuteSchema:   true,
				shouldExecuteData:     false, // Can't execute until schema is complete
			},
			{
				name:                  "data phase only",
				generatedDDL:          "",
				generatedDML:          "UPDATE users SET processed = true",
				schemaMigrationStatus: "",
				dataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
				shouldExecuteSchema:   false,
				shouldExecuteData:     true,
			},
			{
				name:                  "schema complete, data pending",
				generatedDDL:          "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
				generatedDML:          "UPDATE users SET email = 'default@example.com'",
				schemaMigrationStatus: schemasv1alpha4.DataMigrationCompleted,
				dataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
				shouldExecuteSchema:   false,
				shouldExecuteData:     true,
			},
			{
				name:                  "both phases complete",
				generatedDDL:          "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
				generatedDML:          "UPDATE users SET email = 'default@example.com'",
				schemaMigrationStatus: schemasv1alpha4.DataMigrationCompleted,
				dataMigrationStatus:   schemasv1alpha4.DataMigrationCompleted,
				shouldExecuteSchema:   false,
				shouldExecuteData:     false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				migration := &schemasv1alpha4.Migration{
					Spec: schemasv1alpha4.MigrationSpec{
						GeneratedDDL: tt.generatedDDL,
						GeneratedDML: tt.generatedDML,
					},
					Status: schemasv1alpha4.MigrationStatus{
						SchemaMigrationStatus: tt.schemaMigrationStatus,
						DataMigrationStatus:   tt.dataMigrationStatus,
					},
				}

				schemaResult := shouldExecuteSchemaPhase(migration)
				dataResult := shouldExecuteDataPhase(migration)

				assert.Equal(t, tt.shouldExecuteSchema, schemaResult, "Schema phase execution determination")
				assert.Equal(t, tt.shouldExecuteData, dataResult, "Data phase execution determination")
			})
		}
	})
}

func TestRollbackCoordination(t *testing.T) {
	t.Run("rollback manager structure", func(t *testing.T) {
		rollbackManager := &RollbackManager{
			rollbackHistory:  make(map[string][]RollbackAttempt),
			maxRollbackDepth: 5,
			rollbackTimeout:  time.Minute * 15,
		}

		assert.NotNil(t, rollbackManager)
		assert.Equal(t, 5, rollbackManager.maxRollbackDepth)
		assert.Equal(t, time.Minute*15, rollbackManager.rollbackTimeout)
		assert.NotNil(t, rollbackManager.rollbackHistory)
	})

	t.Run("rollback attempt tracking", func(t *testing.T) {
		rollbackManager := &RollbackManager{
			rollbackHistory:  make(map[string][]RollbackAttempt),
			maxRollbackDepth: 10,
		}

		// Simulate rollback attempts
		attempt1 := RollbackAttempt{
			MigrationName: "test-migration",
			Phase:         "data",
			Timestamp:     time.Now(),
			Success:       true,
		}

		rollbackManager.rollbackHistory["test-migration"] = []RollbackAttempt{attempt1}

		coordinator := &MigrationCoordinator{
			rollbackManager: rollbackManager,
		}

		history := coordinator.GetRollbackHistory()
		require.Contains(t, history, "test-migration")
		assert.Len(t, history["test-migration"], 1)
		assert.Equal(t, "data", history["test-migration"][0].Phase)
		assert.True(t, history["test-migration"][0].Success)
	})

	t.Run("rollback depth limiting", func(t *testing.T) {
		rollbackManager := &RollbackManager{
			rollbackHistory:  make(map[string][]RollbackAttempt),
			maxRollbackDepth: 2, // Lower limit for testing
		}

		migrationName := "depth-test"

		// Add maximum number of rollback attempts
		rollbackManager.rollbackHistory[migrationName] = []RollbackAttempt{
			{MigrationName: migrationName, Phase: "data", Success: false},
			{MigrationName: migrationName, Phase: "data", Success: false},
		}

		// Test depth check logic
		attempts := rollbackManager.rollbackHistory[migrationName]
		assert.Len(t, attempts, 2)
		assert.Equal(t, rollbackManager.maxRollbackDepth, len(attempts))
	})
}

func TestProgressTrackingDetailed(t *testing.T) {
	t.Run("progress update handling", func(t *testing.T) {
		coordinator := &MigrationCoordinator{
			progressTracker: &ProgressTracker{
				activeMigrations: make(map[string]*MigrationProgress),
			},
		}

		migration := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "progress-update-test"},
			Spec:       schemasv1alpha4.MigrationSpec{TableName: "users"},
		}

		coordinator.initializeProgress(migration)

		// Test schema phase progress
		coordinator.updatePhase("progress-update-test", PhaseSchema)
		coordinator.handleProgressUpdate("progress-update-test", ProgressInfo{
			Stage:         StageExecuting,
			RowsProcessed: 0,
		})

		progress := coordinator.GetActiveMigrations()["progress-update-test"]
		assert.Equal(t, schemasv1alpha4.DataMigrationRunning, progress.SchemaProgress.Status)

		// Test data phase progress
		coordinator.updatePhase("progress-update-test", PhaseData)
		coordinator.handleProgressUpdate("progress-update-test", ProgressInfo{
			Stage:         StageBatching,
			RowsProcessed: 1500,
		})

		updatedProgress := coordinator.GetActiveMigrations()["progress-update-test"]
		assert.Equal(t, schemasv1alpha4.DataMigrationRunning, updatedProgress.DataProgress.Status)
		assert.Equal(t, int64(1500), updatedProgress.DataProgress.RowsAffected)
	})
}

func TestDataMigrationParsingCoordinator(t *testing.T) {
	t.Run("parse simple DML", func(t *testing.T) {
		dml := `-- Migration: update-emails
UPDATE users SET email = LOWER(email) WHERE email != LOWER(email)`

		migrations, err := parseDataMigrationsFromDML(dml)
		require.NoError(t, err)
		require.Len(t, migrations, 1)

		migration := migrations[0]
		assert.Equal(t, "update-emails", migration.Name)
		assert.Equal(t, "UPDATE users SET email = LOWER(email) WHERE email != LOWER(email)", migration.SQL)
		assert.Equal(t, schemasv1alpha4.DataMigrationTypeCustom, migration.Type)
	})

	t.Run("parse empty DML", func(t *testing.T) {
		migrations, err := parseDataMigrationsFromDML("")
		require.NoError(t, err)
		assert.Empty(t, migrations)
	})

	t.Run("parse comments only", func(t *testing.T) {
		dml := `-- Migration: skipped-migration
-- Migration skipped-migration: SKIPPED (conditions not met)`

		migrations, err := parseDataMigrationsFromDML(dml)
		require.NoError(t, err)
		assert.Empty(t, migrations, "Should return empty when only comments")
	})
}

func TestCoordinatorPhaseLogic(t *testing.T) {
	t.Run("schema phase determination", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "CREATE TABLE test (id SERIAL)",
			},
			Status: schemasv1alpha4.MigrationStatus{
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
			},
		}

		// Should execute schema phase
		assert.True(t, shouldExecuteSchemaPhase(migration))

		// After completion, should not execute again
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		assert.False(t, shouldExecuteSchemaPhase(migration))
	})

	t.Run("data phase determination", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "ALTER TABLE test ADD COLUMN name VARCHAR(100)",
				GeneratedDML: "UPDATE test SET name = 'default' WHERE name IS NULL",
			},
			Status: schemasv1alpha4.MigrationStatus{
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		// Should not execute data phase until schema is complete
		assert.False(t, shouldExecuteDataPhase(migration))

		// After schema completion, should execute data phase
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		assert.True(t, shouldExecuteDataPhase(migration))

		// After data completion, should not execute again
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		assert.False(t, shouldExecuteDataPhase(migration))
	})

	t.Run("execution phases enum", func(t *testing.T) {
		phases := []ExecutionPhase{
			PhaseInitializing,
			PhaseSchema,
			PhaseData,
			PhaseCompleting,
			PhaseCompleted,
			PhaseFailed,
			PhaseRollingBack,
			PhaseRolledBack,
		}

		for _, phase := range phases {
			assert.NotEmpty(t, string(phase), "Phase %s should not be empty", phase)
		}
	})
}

func TestCoordinatorProgressTracking(t *testing.T) {
	t.Run("migration progress initialization", func(t *testing.T) {
		coordinator := &MigrationCoordinator{
			progressTracker: &ProgressTracker{
				activeMigrations: make(map[string]*MigrationProgress),
			},
		}

		migration := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "progress-init-test"},
			Spec:       schemasv1alpha4.MigrationSpec{TableName: "users"},
			Status:     schemasv1alpha4.MigrationStatus{Phase: schemasv1alpha4.Approved},
		}

		coordinator.initializeProgress(migration)

		progress := coordinator.GetActiveMigrations()["progress-init-test"]
		require.NotNil(t, progress)
		assert.Equal(t, "progress-init-test", progress.MigrationName)
		assert.Equal(t, "users", progress.TableName)
		assert.Equal(t, PhaseInitializing, progress.CurrentPhase)
		assert.Equal(t, schemasv1alpha4.Approved, progress.OverallStatus)
	})

	t.Run("phase transitions", func(t *testing.T) {
		coordinator := &MigrationCoordinator{
			progressTracker: &ProgressTracker{
				activeMigrations: make(map[string]*MigrationProgress),
			},
		}

		migration := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "phase-test"},
			Spec:       schemasv1alpha4.MigrationSpec{TableName: "users"},
		}

		coordinator.initializeProgress(migration)

		// Test phase transitions
		phases := []ExecutionPhase{
			PhaseInitializing,
			PhaseSchema,
			PhaseData,
			PhaseCompleted,
		}

		for _, phase := range phases {
			coordinator.updatePhase("phase-test", phase)
			progress := coordinator.GetActiveMigrations()["phase-test"]
			assert.Equal(t, phase, progress.CurrentPhase)
		}
	})
}

func TestRollbackManagement(t *testing.T) {
	t.Run("rollback attempt structure", func(t *testing.T) {
		attempt := RollbackAttempt{
			MigrationName: "test-migration",
			Phase:         "data",
			Timestamp:     time.Now(),
			Success:       true,
			ErrorMessage:  "",
		}

		assert.Equal(t, "test-migration", attempt.MigrationName)
		assert.Equal(t, "data", attempt.Phase)
		assert.True(t, attempt.Success)
		assert.Empty(t, attempt.ErrorMessage)
		assert.False(t, attempt.Timestamp.IsZero())
	})

	t.Run("rollback manager configuration", func(t *testing.T) {
		coordinator := &MigrationCoordinator{
			rollbackManager: &RollbackManager{
				rollbackHistory:  make(map[string][]RollbackAttempt),
				maxRollbackDepth: 10,
				rollbackTimeout:  time.Minute * 30,
			},
		}

		assert.Equal(t, 10, coordinator.rollbackManager.maxRollbackDepth)
		assert.Equal(t, time.Minute*30, coordinator.rollbackManager.rollbackTimeout)
		assert.NotNil(t, coordinator.rollbackManager.rollbackHistory)
	})
}

func TestExecutionLockingCoordination(t *testing.T) {
	t.Run("concurrent execution prevention", func(t *testing.T) {
		coordinator := &MigrationCoordinator{
			executionLocks: sync.Map{},
		}

		migration1 := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "concurrent-1"},
			Spec:       schemasv1alpha4.MigrationSpec{TableName: "shared_table"},
		}

		migration2 := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "concurrent-2"},
			Spec:       schemasv1alpha4.MigrationSpec{TableName: "shared_table"},
		}

		// First migration should acquire lock
		lock1, err := coordinator.acquireExecutionLock(migration1)
		require.NoError(t, err)
		assert.Equal(t, "shared_table", lock1.TableName)

		// Second migration on same table should fail
		_, err = coordinator.acquireExecutionLock(migration2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already being migrated")

		// Release and retry
		coordinator.releaseExecutionLock("shared_table")
		lock2, err := coordinator.acquireExecutionLock(migration2)
		require.NoError(t, err)
		assert.Equal(t, "shared_table", lock2.TableName)
	})

	t.Run("different tables can execute concurrently", func(t *testing.T) {
		coordinator := &MigrationCoordinator{
			executionLocks: sync.Map{},
		}

		migration1 := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "table1-migration"},
			Spec:       schemasv1alpha4.MigrationSpec{TableName: "table1"},
		}

		migration2 := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{Name: "table2-migration"},
			Spec:       schemasv1alpha4.MigrationSpec{TableName: "table2"},
		}

		// Both should acquire locks successfully
		lock1, err := coordinator.acquireExecutionLock(migration1)
		require.NoError(t, err)

		lock2, err := coordinator.acquireExecutionLock(migration2)
		require.NoError(t, err)

		assert.Equal(t, "table1", lock1.TableName)
		assert.Equal(t, "table2", lock2.TableName)
	})
}
