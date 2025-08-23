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
	"sync"
	"time"

	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database"
	"github.com/schemahero/schemahero/pkg/logger"
	"go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MigrationCoordinator orchestrates the execution of schema and data migrations
type MigrationCoordinator struct {
	client          client.Client
	executionLocks  sync.Map // map[string]*ExecutionLock
	statusReporter  StatusReporter
	rollbackManager *RollbackManager
	progressTracker *ProgressTracker
}

// ExecutionLock prevents concurrent migration execution on the same table
type ExecutionLock struct {
	TableName     string
	MigrationName string
	StartTime     time.Time
	mutex         sync.Mutex
}

// StatusReporter handles migration status updates and reporting
type StatusReporter struct {
	client            client.Client
	progressCallbacks []ProgressCallback
}

// RollbackManager handles rollback coordination for failed migrations
type RollbackManager struct {
	rollbackHistory  map[string][]RollbackAttempt
	maxRollbackDepth int
	rollbackTimeout  time.Duration
	mutex            sync.RWMutex
}

// RollbackAttempt tracks a rollback attempt
type RollbackAttempt struct {
	MigrationName string
	Phase         string // "schema" or "data"
	Timestamp     time.Time
	Success       bool
	ErrorMessage  string
}

// ProgressTracker tracks migration execution progress
type ProgressTracker struct {
	activeMigrations map[string]*MigrationProgress
	mutex            sync.RWMutex
}

// MigrationProgress tracks the progress of a specific migration
type MigrationProgress struct {
	MigrationName  string
	TableName      string
	StartTime      time.Time
	CurrentPhase   ExecutionPhase
	SchemaProgress PhaseProgress
	DataProgress   PhaseProgress
	OverallStatus  schemasv1alpha4.Phase
}

// ExecutionPhase represents the current execution phase
type ExecutionPhase string

const (
	PhaseInitializing ExecutionPhase = "INITIALIZING"
	PhaseSchema       ExecutionPhase = "SCHEMA"
	PhaseData         ExecutionPhase = "DATA"
	PhaseCompleting   ExecutionPhase = "COMPLETING"
	PhaseCompleted    ExecutionPhase = "COMPLETED"
	PhaseFailed       ExecutionPhase = "FAILED"
	PhaseRollingBack  ExecutionPhase = "ROLLING_BACK"
	PhaseRolledBack   ExecutionPhase = "ROLLED_BACK"
)

// PhaseProgress tracks progress of a specific phase
type PhaseProgress struct {
	Status       schemasv1alpha4.DataMigrationStatus
	StartTime    time.Time
	EndTime      time.Time
	RowsAffected int64
	ErrorMessage string
}

// NewMigrationCoordinator creates a new migration execution coordinator
func NewMigrationCoordinator(client client.Client) *MigrationCoordinator {
	return &MigrationCoordinator{
		client:         client,
		executionLocks: sync.Map{},
		statusReporter: StatusReporter{
			client:            client,
			progressCallbacks: []ProgressCallback{},
		},
		rollbackManager: &RollbackManager{
			rollbackHistory:  make(map[string][]RollbackAttempt),
			maxRollbackDepth: 10,
			rollbackTimeout:  time.Minute * 30,
		},
		progressTracker: &ProgressTracker{
			activeMigrations: make(map[string]*MigrationProgress),
		},
	}
}

// ExecuteMigration orchestrates the complete execution of a migration
func (c *MigrationCoordinator) ExecuteMigration(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database) error {
	// Acquire execution lock
	_, err := c.acquireExecutionLock(migration)
	if err != nil {
		return errors.Wrap(err, "failed to acquire execution lock")
	}
	defer c.releaseExecutionLock(migration.Spec.TableName)

	// Initialize progress tracking
	c.initializeProgress(migration)
	defer c.finalizeProgress(migration)

	// Execute migration phases in order
	if err := c.executePhases(ctx, migration, db); err != nil {
		// Attempt rollback if possible
		if rollbackErr := c.handleExecutionFailure(ctx, migration, db, err); rollbackErr != nil {
			return errors.Wrapf(err, "migration failed and rollback also failed: %v", rollbackErr)
		}
		return err
	}

	return nil
}

// executePhases executes schema and data phases in the correct order
func (c *MigrationCoordinator) executePhases(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database) error {
	metrics := GetGlobalMetrics()

	// Phase 1: Schema Execution (DDL)
	if shouldExecuteSchemaPhase(migration) {
		logger.Info("starting schema execution phase",
			zap.String("migration", migration.Name))

		c.updatePhase(migration.Name, PhaseSchema)
		metrics.StartSchemaMigration(migration.Name)

		if err := c.executeSchemaPhaseCoordinated(ctx, migration, db); err != nil {
			metrics.CompleteSchemaMigration(migration.Name, schemasv1alpha4.DataMigrationFailed, err.Error())
			return errors.Wrap(err, "schema phase failed")
		}

		metrics.CompleteSchemaMigration(migration.Name, schemasv1alpha4.DataMigrationCompleted, "")
		logger.Info("schema execution phase completed",
			zap.String("migration", migration.Name))
	}

	// Phase 2: Data Execution (DML)
	if shouldExecuteDataPhase(migration) {
		logger.Info("starting data execution phase",
			zap.String("migration", migration.Name))

		c.updatePhase(migration.Name, PhaseData)
		metrics.StartDataMigration(migration.Name)

		rowsAffected, err := c.executeDataPhaseCoordinated(ctx, migration, db)
		if err != nil {
			metrics.CompleteDataMigration(migration.Name, schemasv1alpha4.DataMigrationFailed, 0, err.Error())
			return errors.Wrap(err, "data phase failed")
		}

		metrics.CompleteDataMigration(migration.Name, schemasv1alpha4.DataMigrationCompleted, rowsAffected, "")
		logger.Info("data execution phase completed",
			zap.String("migration", migration.Name),
			zap.Int64("rowsAffected", rowsAffected))
	}

	c.updatePhase(migration.Name, PhaseCompleted)
	return nil
}

// acquireExecutionLock prevents concurrent execution on the same table
func (c *MigrationCoordinator) acquireExecutionLock(migration *schemasv1alpha4.Migration) (*ExecutionLock, error) {
	lockKey := migration.Spec.TableName

	newLock := &ExecutionLock{
		TableName:     migration.Spec.TableName,
		MigrationName: migration.Name,
		StartTime:     time.Now(),
	}

	// Try to store the lock atomically
	if actual, loaded := c.executionLocks.LoadOrStore(lockKey, newLock); loaded {
		existingLock := actual.(*ExecutionLock)
		return nil, fmt.Errorf("table %s is already being migrated by %s (started at %v)",
			lockKey, existingLock.MigrationName, existingLock.StartTime)
	}

	logger.Info("acquired execution lock",
		zap.String("table", lockKey),
		zap.String("migration", migration.Name))

	return newLock, nil
}

// releaseExecutionLock releases the execution lock for a table
func (c *MigrationCoordinator) releaseExecutionLock(tableName string) {
	c.executionLocks.Delete(tableName)
	logger.Debug("released execution lock",
		zap.String("table", tableName))
}

// executeSchemaPhaseCoordinated executes the schema phase with coordination
func (c *MigrationCoordinator) executeSchemaPhaseCoordinated(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database) error {
	// Update migration status
	migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationRunning
	if err := c.statusReporter.UpdateMigrationStatus(ctx, migration); err != nil {
		return errors.Wrap(err, "failed to update schema status to running")
	}

	// Execute DDL statements
	statements := db.GetStatementsFromDDL(migration.Spec.GeneratedDDL)
	if err := db.ApplySync(statements); err != nil {
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationFailed
		c.statusReporter.UpdateMigrationStatus(ctx, migration)
		return errors.Wrap(err, "failed to execute schema statements")
	}

	// Update status to completed
	migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted
	return c.statusReporter.UpdateMigrationStatus(ctx, migration)
}

// executeDataPhaseCoordinated executes the data phase with coordination and returns rows affected
func (c *MigrationCoordinator) executeDataPhaseCoordinated(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database) (int64, error) {
	// Update migration status
	migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationRunning
	if err := c.statusReporter.UpdateMigrationStatus(ctx, migration); err != nil {
		return 0, errors.Wrap(err, "failed to update data status to running")
	}

	// Create executor with progress tracking
	var totalRowsAffected int64
	progressCallback := func(migrationName string, progress ProgressInfo) {
		c.handleProgressUpdate(migrationName, progress)
		totalRowsAffected = progress.RowsProcessed
	}

	controllerLogger := &ControllerLogger{}
	executor := NewMigrationExecutor(db.Driver, db.URI, progressCallback, controllerLogger)

	// Parse and execute data migrations
	dataMigrations, err := parseDataMigrationsFromDML(migration.Spec.GeneratedDML)
	if err != nil {
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationFailed
		c.statusReporter.UpdateMigrationStatus(ctx, migration)
		return 0, errors.Wrap(err, "failed to parse data migrations")
	}

	if err := executor.ExecuteDataMigrations(ctx, migration.Spec.TableName, dataMigrations); err != nil {
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationFailed
		c.statusReporter.UpdateMigrationStatus(ctx, migration)
		return totalRowsAffected, errors.Wrap(err, "failed to execute data migrations")
	}

	// Update status to completed
	migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationCompleted
	if err := c.statusReporter.UpdateMigrationStatus(ctx, migration); err != nil {
		return totalRowsAffected, errors.Wrap(err, "failed to update data status to completed")
	}

	return totalRowsAffected, nil
}

// handleExecutionFailure coordinates rollback when migrations fail
func (c *MigrationCoordinator) handleExecutionFailure(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database, originalError error) error {
	logger.Info("migration execution failed, attempting rollback",
		zap.String("migration", migration.Name),
		zap.Error(originalError))

	c.updatePhase(migration.Name, PhaseRollingBack)

	// Attempt rollback using the rollback manager
	rollbackErr := c.rollbackManager.AttemptRollback(ctx, migration, db, c.client)
	if rollbackErr != nil {
		c.updatePhase(migration.Name, PhaseFailed)
		return rollbackErr
	}

	c.updatePhase(migration.Name, PhaseRolledBack)
	logger.Info("migration rollback completed",
		zap.String("migration", migration.Name))

	return nil
}

// initializeProgress initializes progress tracking for a migration
func (c *MigrationCoordinator) initializeProgress(migration *schemasv1alpha4.Migration) {
	c.progressTracker.mutex.Lock()
	defer c.progressTracker.mutex.Unlock()

	c.progressTracker.activeMigrations[migration.Name] = &MigrationProgress{
		MigrationName: migration.Name,
		TableName:     migration.Spec.TableName,
		StartTime:     time.Now(),
		CurrentPhase:  PhaseInitializing,
		OverallStatus: migration.Status.Phase,
	}
}

// updatePhase updates the current execution phase
func (c *MigrationCoordinator) updatePhase(migrationName string, phase ExecutionPhase) {
	c.progressTracker.mutex.Lock()
	defer c.progressTracker.mutex.Unlock()

	if progress, exists := c.progressTracker.activeMigrations[migrationName]; exists {
		progress.CurrentPhase = phase
	}
}

// finalizeProgress completes progress tracking for a migration
func (c *MigrationCoordinator) finalizeProgress(migration *schemasv1alpha4.Migration) {
	c.progressTracker.mutex.Lock()
	defer c.progressTracker.mutex.Unlock()

	delete(c.progressTracker.activeMigrations, migration.Name)
}

// handleProgressUpdate processes progress updates from the executor
func (c *MigrationCoordinator) handleProgressUpdate(migrationName string, progress ProgressInfo) {
	c.progressTracker.mutex.Lock()
	defer c.progressTracker.mutex.Unlock()

	if migrationProgress, exists := c.progressTracker.activeMigrations[migrationName]; exists {
		switch progress.Stage {
		case StageValidating:
			migrationProgress.CurrentPhase = PhaseInitializing
		case StageExecuting, StageBatching:
			if migrationProgress.CurrentPhase == PhaseSchema {
				migrationProgress.SchemaProgress.Status = schemasv1alpha4.DataMigrationRunning
			} else if migrationProgress.CurrentPhase == PhaseData {
				migrationProgress.DataProgress.Status = schemasv1alpha4.DataMigrationRunning
				migrationProgress.DataProgress.RowsAffected = progress.RowsProcessed
			}
		case StageCompleted:
			if migrationProgress.CurrentPhase == PhaseSchema {
				migrationProgress.SchemaProgress.Status = schemasv1alpha4.DataMigrationCompleted
				migrationProgress.SchemaProgress.EndTime = time.Now()
			} else if migrationProgress.CurrentPhase == PhaseData {
				migrationProgress.DataProgress.Status = schemasv1alpha4.DataMigrationCompleted
				migrationProgress.DataProgress.EndTime = time.Now()
				migrationProgress.DataProgress.RowsAffected = progress.RowsProcessed
			}
		case StageFailed:
			if migrationProgress.CurrentPhase == PhaseSchema {
				migrationProgress.SchemaProgress.Status = schemasv1alpha4.DataMigrationFailed
				migrationProgress.SchemaProgress.ErrorMessage = progress.Error.Error()
			} else if migrationProgress.CurrentPhase == PhaseData {
				migrationProgress.DataProgress.Status = schemasv1alpha4.DataMigrationFailed
				migrationProgress.DataProgress.ErrorMessage = progress.Error.Error()
			}
		}
	}
}

// UpdateMigrationStatus updates the migration status in Kubernetes
func (sr *StatusReporter) UpdateMigrationStatus(ctx context.Context, migration *schemasv1alpha4.Migration) error {
	return sr.client.Status().Update(ctx, migration)
}

// AttemptRollback attempts to rollback a failed migration
func (rm *RollbackManager) AttemptRollback(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database, client client.Client) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	migrationName := migration.Name

	// Check rollback history to prevent infinite rollback loops
	if attempts, exists := rm.rollbackHistory[migrationName]; exists {
		if len(attempts) >= rm.maxRollbackDepth {
			return fmt.Errorf("maximum rollback attempts (%d) exceeded for migration %s", rm.maxRollbackDepth, migrationName)
		}
	}

	// Determine what to rollback based on current status
	rollbackAttempts := []RollbackAttempt{}

	// Rollback data phase if it was executed/failed
	if migration.Status.DataMigrationStatus == schemasv1alpha4.DataMigrationFailed ||
		migration.Status.DataMigrationStatus == schemasv1alpha4.DataMigrationCompleted {

		attempt := RollbackAttempt{
			MigrationName: migrationName,
			Phase:         "data",
			Timestamp:     time.Now(),
		}

		if err := rm.rollbackDataPhase(ctx, migration, db); err != nil {
			attempt.Success = false
			attempt.ErrorMessage = err.Error()
			logger.Error(errors.Wrapf(err, "failed to rollback data phase for migration %s", migrationName))
		} else {
			attempt.Success = true
			logger.Info("successfully rolled back data phase",
				zap.String("migration", migrationName))
		}

		rollbackAttempts = append(rollbackAttempts, attempt)
	}

	// Note: Schema rollback is more complex and database-dependent
	// For now, we don't automatically rollback schema changes
	// as they often involve structural changes that are harder to reverse

	// Record rollback attempts
	if rm.rollbackHistory[migrationName] == nil {
		rm.rollbackHistory[migrationName] = []RollbackAttempt{}
	}
	rm.rollbackHistory[migrationName] = append(rm.rollbackHistory[migrationName], rollbackAttempts...)

	// Check if any rollback failed
	for _, attempt := range rollbackAttempts {
		if !attempt.Success {
			return fmt.Errorf("rollback failed for %s phase: %s", attempt.Phase, attempt.ErrorMessage)
		}
	}

	return nil
}

// rollbackDataPhase attempts to rollback data migrations
func (rm *RollbackManager) rollbackDataPhase(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database) error {
	// Parse the original data migrations to find reversible ones
	dataMigrations, err := parseDataMigrationsFromDML(migration.Spec.GeneratedDML)
	if err != nil {
		return errors.Wrap(err, "failed to parse data migrations for rollback")
	}

	// Execute rollback for each reversible migration
	for _, dataMigration := range dataMigrations {
		if dataMigration.Reversible && dataMigration.ReverseSQL != "" {
			logger.Info("rolling back data migration",
				zap.String("migration", dataMigration.Name))

			// Create rollback migration
			rollbackMigration := schemasv1alpha4.DataMigration{
				Name: dataMigration.Name + "-rollback",
				SQL:  dataMigration.ReverseSQL,
				Type: schemasv1alpha4.DataMigrationTypeCustom,
			}

			// Execute rollback
			controllerLogger := &ControllerLogger{}
			executor := NewMigrationExecutor(db.Driver, db.URI, nil, controllerLogger)

			if err := executor.ExecuteSingleDataMigration(ctx, migration.Spec.TableName, &rollbackMigration); err != nil {
				return errors.Wrapf(err, "failed to rollback data migration %s", dataMigration.Name)
			}
		}
	}

	return nil
}

// GetActiveMigrations returns currently active migrations
func (c *MigrationCoordinator) GetActiveMigrations() map[string]*MigrationProgress {
	c.progressTracker.mutex.RLock()
	defer c.progressTracker.mutex.RUnlock()

	result := make(map[string]*MigrationProgress)
	for k, v := range c.progressTracker.activeMigrations {
		result[k] = v
	}
	return result
}

// GetRollbackHistory returns the rollback history for debugging
func (c *MigrationCoordinator) GetRollbackHistory() map[string][]RollbackAttempt {
	c.rollbackManager.mutex.RLock()
	defer c.rollbackManager.mutex.RUnlock()

	result := make(map[string][]RollbackAttempt)
	for k, v := range c.rollbackManager.rollbackHistory {
		result[k] = make([]RollbackAttempt, len(v))
		copy(result[k], v)
	}
	return result
}

// Helper functions for phase determination
func shouldExecuteSchemaPhase(migration *schemasv1alpha4.Migration) bool {
	return migration.Spec.GeneratedDDL != "" &&
		migration.Status.SchemaMigrationStatus != schemasv1alpha4.DataMigrationCompleted &&
		migration.Status.SchemaMigrationStatus != schemasv1alpha4.DataMigrationFailed
}

func shouldExecuteDataPhase(migration *schemasv1alpha4.Migration) bool {
	hasSchema := migration.Spec.GeneratedDDL != ""
	schemaComplete := !hasSchema || migration.Status.SchemaMigrationStatus == schemasv1alpha4.DataMigrationCompleted

	return migration.Spec.GeneratedDML != "" &&
		schemaComplete &&
		migration.Status.DataMigrationStatus != schemasv1alpha4.DataMigrationCompleted &&
		migration.Status.DataMigrationStatus != schemasv1alpha4.DataMigrationFailed
}

// parseDataMigrationsFromDML parses DML string into DataMigration objects (shared function)
func parseDataMigrationsFromDML(generatedDML string) ([]schemasv1alpha4.DataMigration, error) {
	if generatedDML == "" {
		return []schemasv1alpha4.DataMigration{}, nil
	}

	// Simple parsing - create one migration from all the DML
	lines := strings.Split(generatedDML, "\n")
	var sqlLines []string
	migrationName := "generated-data-migration"

	// Extract migration name from comments if present
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-- Migration:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				migrationName = strings.TrimSpace(parts[1])
			}
		} else if line != "" && !strings.HasPrefix(line, "--") {
			sqlLines = append(sqlLines, line)
		}
	}

	if len(sqlLines) == 0 {
		return []schemasv1alpha4.DataMigration{}, nil
	}

	migration := schemasv1alpha4.DataMigration{
		Name: migrationName,
		SQL:  strings.Join(sqlLines, "\n"),
		Type: schemasv1alpha4.DataMigrationTypeCustom,
	}

	return []schemasv1alpha4.DataMigration{migration}, nil
}
