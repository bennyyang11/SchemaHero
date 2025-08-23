package migration

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	databasesv1alpha4 "github.com/schemahero/schemahero/pkg/apis/databases/v1alpha4"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database"
	"github.com/schemahero/schemahero/pkg/logger"
	"go.uber.org/zap"
	kuberneteserrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *ReconcileMigration) reconcileMigration(ctx context.Context, migration *schemasv1alpha4.Migration) (reconcile.Result, error) {
	logger.Debug("checking migration",
		zap.String("name", migration.Name),
		zap.String("tableName", migration.Spec.TableName))

	if !shouldApplyMigration(migration) {
		logger.Debug("migration not yet approved or already executed",
			zap.String("name", migration.Name),
			zap.String("tableName", migration.Spec.TableName))
		return reconcile.Result{}, nil
	}

	databaseInstance, err := getDatabaseFromMigration(ctx, migration)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to get database from migration %s", migration.Name)
	}

	driver, connectionURI, err := databaseInstance.GetConnection(ctx)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to get connection details for database")
	}

	db := database.Database{
		Driver: driver,
		URI:    connectionURI,
	}

	// Enhanced execution: Use coordinator for orchestrated execution
	coordinator := NewMigrationCoordinator(r.Client)
	if err := coordinator.ExecuteMigration(ctx, migration, &db); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// executeCompleteМigration handles the complete execution of both schema and data migrations
func (r *ReconcileMigration) executeCompleteМigration(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database) (reconcile.Result, error) {
	// Phase 1: Execute schema changes (DDL)
	if err := r.executeSchemaPhase(ctx, migration, db); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to execute schema phase")
	}

	// Phase 2: Execute data migrations (DML)
	if err := r.executeDataPhase(ctx, migration, db); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to execute data phase")
	}

	// Mark migration as fully executed
	migration.Status.ExecutedAt = time.Now().Unix()
	migration.Status.Phase = schemasv1alpha4.Executed

	if err := r.updateMigrationStatus(ctx, migration); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to update migration status")
	}

	logger.Info("migration completed successfully",
		zap.String("name", migration.Name),
		zap.String("tableName", migration.Spec.TableName))

	return reconcile.Result{}, nil
}

// executeSchemaPhase executes DDL statements (schema changes + seed data)
func (r *ReconcileMigration) executeSchemaPhase(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database) error {
	// Check if schema phase is already complete
	if migration.Status.SchemaMigrationStatus == schemasv1alpha4.DataMigrationCompleted {
		logger.Debug("schema phase already completed, skipping",
			zap.String("migration", migration.Name))
		return nil
	}

	// Skip if no DDL to execute
	if migration.Spec.GeneratedDDL == "" {
		logger.Debug("no schema changes to execute",
			zap.String("migration", migration.Name))
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		return r.updateMigrationStatus(ctx, migration)
	}

	logger.Info("executing schema phase",
		zap.String("migration", migration.Name))

	// Start metrics collection
	metrics := GetGlobalMetrics()
	metrics.StartSchemaMigration(migration.Name)

	// Update status to running
	migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationRunning
	if err := r.updateMigrationStatus(ctx, migration); err != nil {
		metrics.CompleteSchemaMigration(migration.Name, schemasv1alpha4.DataMigrationFailed, err.Error())
		return errors.Wrap(err, "failed to update schema status to running")
	}

	// Execute DDL statements
	statements := db.GetStatementsFromDDL(migration.Spec.GeneratedDDL)
	if err := db.ApplySync(statements); err != nil {
		migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationFailed
		metrics.CompleteSchemaMigration(migration.Name, schemasv1alpha4.DataMigrationFailed, err.Error())
		r.updateMigrationStatus(ctx, migration)
		return errors.Wrap(err, "failed to apply schema statements")
	}

	// Mark schema phase as completed
	migration.Status.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted
	metrics.CompleteSchemaMigration(migration.Name, schemasv1alpha4.DataMigrationCompleted, "")

	return r.updateMigrationStatus(ctx, migration)
}

// executeDataPhase executes DML statements (data migrations)
func (r *ReconcileMigration) executeDataPhase(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database) error {
	// Check if data phase is already complete
	if migration.Status.DataMigrationStatus == schemasv1alpha4.DataMigrationCompleted {
		logger.Debug("data phase already completed, skipping",
			zap.String("migration", migration.Name))
		return nil
	}

	// Skip if no DML to execute
	if migration.Spec.GeneratedDML == "" {
		logger.Debug("no data migrations to execute",
			zap.String("migration", migration.Name))
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationCompleted
		return r.updateMigrationStatus(ctx, migration)
	}

	logger.Info("executing data phase",
		zap.String("migration", migration.Name))

	// Start metrics collection
	metrics := GetGlobalMetrics()
	metrics.StartDataMigration(migration.Name)

	// Update status to running
	migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationRunning
	if err := r.updateMigrationStatus(ctx, migration); err != nil {
		metrics.CompleteDataMigration(migration.Name, schemasv1alpha4.DataMigrationFailed, 0, err.Error())
		return errors.Wrap(err, "failed to update data status to running")
	}

	// Execute data migrations with retry logic
	rowsAffected, err := r.executeDataMigrationsWithRetry(ctx, migration, db)
	if err != nil {
		migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationFailed
		metrics.CompleteDataMigration(migration.Name, schemasv1alpha4.DataMigrationFailed, rowsAffected, err.Error())
		r.updateMigrationStatus(ctx, migration)
		return errors.Wrap(err, "failed to execute data migrations")
	}

	// Mark data phase as completed
	migration.Status.DataMigrationStatus = schemasv1alpha4.DataMigrationCompleted
	metrics.CompleteDataMigration(migration.Name, schemasv1alpha4.DataMigrationCompleted, rowsAffected, "")

	return r.updateMigrationStatus(ctx, migration)
}

// executeDataMigrationsWithRetry executes data migrations with retry logic and returns rows affected
func (r *ReconcileMigration) executeDataMigrationsWithRetry(ctx context.Context, migration *schemasv1alpha4.Migration, db *database.Database) (int64, error) {
	maxRetries := 3
	baseDelay := time.Second * 10
	metrics := GetGlobalMetrics()

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			logger.Info("retrying data migration execution",
				zap.String("migration", migration.Name),
				zap.Int("attempt", attempt+1),
				zap.Int("maxRetries", maxRetries))

			// Record retry attempt
			metrics.RecordRetry(migration.Name)

			// Exponential backoff
			delay := time.Duration(attempt) * baseDelay
			time.Sleep(delay)
		}

		// Create migration executor with progress tracking
		var totalRowsAffected int64
		progressCallback := func(migrationName string, progress ProgressInfo) {
			logger.Debug("data migration progress",
				zap.String("migration", migrationName),
				zap.String("stage", string(progress.Stage)),
				zap.Int64("rowsProcessed", progress.RowsProcessed))
			totalRowsAffected = progress.RowsProcessed
		}

		controllerLogger := &ControllerLogger{}
		executor := NewMigrationExecutor(db.Driver, db.URI, progressCallback, controllerLogger)

		// Parse data migrations from DML
		dataMigrations, err := r.parseDataMigrationsFromDML(migration.Spec.GeneratedDML)
		if err != nil {
			return 0, errors.Wrap(err, "failed to parse data migrations from DML")
		}

		// Execute data migrations
		if err := executor.ExecuteDataMigrations(ctx, migration.Spec.TableName, dataMigrations); err != nil {
			logger.Error(errors.Wrapf(err, "data migration execution failed for %s (attempt %d)", migration.Name, attempt+1))

			if attempt == maxRetries-1 {
				return totalRowsAffected, errors.Wrapf(err, "data migration failed after %d attempts", maxRetries)
			}
			continue
		}

		// Success
		logger.Info("data migration execution succeeded",
			zap.String("migration", migration.Name),
			zap.Int("attempt", attempt+1),
			zap.Int64("rowsAffected", totalRowsAffected))
		return totalRowsAffected, nil
	}

	return 0, errors.New("should not reach here")
}

// parseDataMigrationsFromDML parses the GeneratedDML field into executable migrations
func (r *ReconcileMigration) parseDataMigrationsFromDML(generatedDML string) ([]schemasv1alpha4.DataMigration, error) {
	if generatedDML == "" {
		return []schemasv1alpha4.DataMigration{}, nil
	}

	// For now, create a single migration from the generated DML
	// In the future, this could parse multiple migrations from comments
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

// updateMigrationStatus updates the migration status with conflict handling
func (r *ReconcileMigration) updateMigrationStatus(ctx context.Context, migration *schemasv1alpha4.Migration) error {
	err := r.Update(ctx, migration)
	if err != nil {
		if kuberneteserrors.IsConflict(err) {
			// Handle conflict by getting the latest version
			updatedMigration := &schemasv1alpha4.Migration{}
			err := r.Get(ctx, types.NamespacedName{
				Name:      migration.Name,
				Namespace: migration.Namespace,
			}, updatedMigration)
			if err != nil {
				return errors.Wrap(err, "failed to get updated migration instance")
			}

			// Copy our status updates to the latest version
			updatedMigration.Status.SchemaMigrationStatus = migration.Status.SchemaMigrationStatus
			updatedMigration.Status.DataMigrationStatus = migration.Status.DataMigrationStatus
			updatedMigration.Status.ExecutedAt = migration.Status.ExecutedAt
			updatedMigration.Status.Phase = migration.Status.Phase

			if err := r.Update(ctx, updatedMigration); err != nil {
				return errors.Wrap(err, "failed to update migration status after conflict resolution")
			}
		} else {
			return errors.Wrap(err, "failed to update migration status")
		}
	}
	return nil
}

// shouldApplyMigration checks if a migration should be executed
func shouldApplyMigration(migration *schemasv1alpha4.Migration) bool {
	if migration.Status.ApprovedAt > 0 && migration.Status.ExecutedAt == 0 {
		return true
	}
	return false
}

// shouldApplySchemaPhase checks if schema phase should be executed
func shouldApplySchemaPhase(migration *schemasv1alpha4.Migration) bool {
	// No schema phase needed if no DDL to execute
	if migration.Spec.GeneratedDDL == "" {
		return false
	}

	return migration.Status.SchemaMigrationStatus != schemasv1alpha4.DataMigrationCompleted &&
		migration.Status.SchemaMigrationStatus != schemasv1alpha4.DataMigrationFailed
}

// shouldApplyDataPhase checks if data phase should be executed
func shouldApplyDataPhase(migration *schemasv1alpha4.Migration) bool {
	// No data phase needed if no data migration status is set or no DML to execute
	if migration.Status.DataMigrationStatus == "" || migration.Spec.GeneratedDML == "" {
		return false
	}

	// Data phase can only execute after schema phase is complete (or no schema phase needed)
	hasSchema := migration.Spec.GeneratedDDL != ""
	schemaComplete := !hasSchema || migration.Status.SchemaMigrationStatus == schemasv1alpha4.DataMigrationCompleted

	dataNeeded := migration.Status.DataMigrationStatus != schemasv1alpha4.DataMigrationCompleted &&
		migration.Status.DataMigrationStatus != schemasv1alpha4.DataMigrationFailed

	return schemaComplete && dataNeeded
}

// ControllerLogger adapts the controller's logger interface to the executor's Logger interface
type ControllerLogger struct{}

func (c *ControllerLogger) Info(msg string, args ...interface{}) {
	logger.Info(msg, zap.Any("args", args))
}

func (c *ControllerLogger) Error(msg string, args ...interface{}) {
	logger.Error(errors.New(msg))
}

func (c *ControllerLogger) Debug(msg string, args ...interface{}) {
	logger.Debug(msg, zap.Any("args", args))
}

func getDatabaseFromMigration(ctx context.Context, migration *schemasv1alpha4.Migration) (*databasesv1alpha4.Database, error) {
	table, err := TableFromMigration(ctx, migration)
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return nil, errors.Wrap(err, "failed to get table")
		}
	} else {
		database, err := DatabaseFromTable(ctx, table)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get database from table %s", table.Name)
		}
		return database, nil
	}

	view, err := ViewFromMigration(ctx, migration)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get view")
	}
	database, err := DatabaseFromView(ctx, view)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get database from view %s", view.Name)
	}
	return database, nil
}
