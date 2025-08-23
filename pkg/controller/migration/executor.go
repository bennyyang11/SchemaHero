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
	"time"

	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/interfaces"
	"github.com/schemahero/schemahero/pkg/database/mysql"
	"github.com/schemahero/schemahero/pkg/database/postgres"
)

// MigrationExecutor handles the safe execution of data migrations
type MigrationExecutor struct {
	driver           string
	uri              string
	progressCallback ProgressCallback
	logger           Logger
}

// ProgressCallback is called to report migration progress
type ProgressCallback func(migrationName string, progress ProgressInfo)

// ProgressInfo contains information about migration execution progress
type ProgressInfo struct {
	Stage             ExecutionStage
	CurrentBatch      int32
	TotalBatches      int32
	RowsProcessed     int64
	TotalRows         int64
	ElapsedTime       time.Duration
	EstimatedTimeLeft time.Duration
	Error             error
}

// ExecutionStage represents the current stage of migration execution
type ExecutionStage string

const (
	StageValidating  ExecutionStage = "VALIDATING"
	StageExecuting   ExecutionStage = "EXECUTING"
	StageBatching    ExecutionStage = "BATCHING"
	StageCompleted   ExecutionStage = "COMPLETED"
	StageFailed      ExecutionStage = "FAILED"
	StageRollingBack ExecutionStage = "ROLLING_BACK"
	StageRolledBack  ExecutionStage = "ROLLED_BACK"
)

// Logger interface for migration execution logging
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
}

// NewMigrationExecutor creates a new migration executor
func NewMigrationExecutor(driver, uri string, progressCallback ProgressCallback, logger Logger) *MigrationExecutor {
	return &MigrationExecutor{
		driver:           driver,
		uri:              uri,
		progressCallback: progressCallback,
		logger:           logger,
	}
}

// ExecuteDataMigrations executes a list of data migrations in dependency order
func (e *MigrationExecutor) ExecuteDataMigrations(ctx context.Context, tableName string, migrations []schemasv1alpha4.DataMigration) error {
	if len(migrations) == 0 {
		return nil
	}

	// Resolve execution order
	resolver := schemasv1alpha4.NewDependencyResolver(migrations)
	orderedMigrations, err := resolver.ResolveExecutionOrder()
	if err != nil {
		return errors.Wrap(err, "failed to resolve migration dependencies")
	}

	e.logger.Info("Starting execution of %d data migrations for table %s", len(orderedMigrations), tableName)

	// Execute each migration in order
	for i, migration := range orderedMigrations {
		e.logger.Info("Executing migration %d/%d: %s", i+1, len(orderedMigrations), migration.Name)

		if err := e.ExecuteSingleDataMigration(ctx, tableName, migration); err != nil {
			return errors.Wrapf(err, "failed to execute migration %s", migration.Name)
		}
	}

	e.logger.Info("Successfully completed all %d data migrations for table %s", len(orderedMigrations), tableName)
	return nil
}

// ExecuteSingleDataMigration executes a single data migration with all safety features
func (e *MigrationExecutor) ExecuteSingleDataMigration(ctx context.Context, tableName string, migration *schemasv1alpha4.DataMigration) error {
	startTime := time.Now()

	// Report progress: Starting validation
	if e.progressCallback != nil {
		e.progressCallback(migration.Name, ProgressInfo{
			Stage:       StageValidating,
			ElapsedTime: time.Since(startTime),
		})
	}

	// Create database connection based on driver
	var executor interfaces.DataMigrationExecutor
	var err error

	switch e.driver {
	case "postgres", "cockroachdb", "timescaledb":
		executor, err = e.createPostgresExecutor()
	case "mysql":
		executor, err = e.createMysqlExecutor()
	default:
		return fmt.Errorf("data migration execution not supported for driver: %s", e.driver)
	}

	if err != nil {
		return errors.Wrap(err, "failed to create migration executor")
	}

	// Set up timeout context
	executionCtx := ctx
	if migration.Timeout != nil && migration.Timeout.Duration > 0 {
		var cancel context.CancelFunc
		executionCtx, cancel = context.WithTimeout(ctx, migration.Timeout.Duration)
		defer cancel()
	}

	// Validate conditions before execution
	if len(migration.Conditions) > 0 {
		shouldExecute, err := executor.CheckMigrationConditions(migration.Conditions)
		if err != nil {
			return errors.Wrap(err, "failed to validate migration conditions")
		}
		if !shouldExecute {
			e.logger.Info("Skipping migration %s - conditions not met", migration.Name)
			if e.progressCallback != nil {
				e.progressCallback(migration.Name, ProgressInfo{
					Stage:       StageCompleted,
					ElapsedTime: time.Since(startTime),
				})
			}
			return nil
		}
	}

	// Report progress: Starting execution
	if e.progressCallback != nil {
		e.progressCallback(migration.Name, ProgressInfo{
			Stage:       StageExecuting,
			ElapsedTime: time.Since(startTime),
		})
	}

	// Execute the migration
	err = e.executeMigrationWithSafety(executionCtx, tableName, migration, executor, startTime)
	if err != nil {
		// Report failure
		if e.progressCallback != nil {
			e.progressCallback(migration.Name, ProgressInfo{
				Stage:       StageFailed,
				ElapsedTime: time.Since(startTime),
				Error:       err,
			})
		}

		// Attempt rollback if the migration is reversible
		if migration.Reversible && migration.ReverseSQL != "" {
			e.logger.Info("Attempting to rollback migration %s", migration.Name)
			if rollbackErr := e.attemptRollback(ctx, tableName, migration); rollbackErr != nil {
				e.logger.Error("Rollback failed for migration %s: %v", migration.Name, rollbackErr)
				return errors.Wrapf(err, "migration failed and rollback also failed: %v", rollbackErr)
			}
			e.logger.Info("Successfully rolled back migration %s", migration.Name)
		}

		return err
	}

	// Report success
	if e.progressCallback != nil {
		e.progressCallback(migration.Name, ProgressInfo{
			Stage:       StageCompleted,
			ElapsedTime: time.Since(startTime),
		})
	}

	return nil
}

// executeMigrationWithSafety executes a migration with batching and safety features
func (e *MigrationExecutor) executeMigrationWithSafety(ctx context.Context, tableName string, migration *schemasv1alpha4.DataMigration, executor interfaces.DataMigrationExecutor, startTime time.Time) error {
	// Get the SQL to execute
	sql, err := e.getMigrationSQL(tableName, migration)
	if err != nil {
		return errors.Wrap(err, "failed to generate migration SQL")
	}

	// Execute with or without batching
	if migration.BatchSize > 0 {
		e.logger.Info("Executing migration %s with batching (batch size: %d)", migration.Name, migration.BatchSize)

		if e.progressCallback != nil {
			e.progressCallback(migration.Name, ProgressInfo{
				Stage:       StageBatching,
				ElapsedTime: time.Since(startTime),
			})
		}

		return executor.ExecuteInBatches(sql, migration.BatchSize, migration.BatchDelayMs)
	} else {
		e.logger.Info("Executing migration %s as single transaction", migration.Name)
		return executor.ExecuteDataMigration(tableName, migration)
	}
}

// getMigrationSQL generates the SQL for a migration
func (e *MigrationExecutor) getMigrationSQL(tableName string, migration *schemasv1alpha4.DataMigration) (string, error) {
	if migration.SQL != "" {
		return migration.SQL, nil
	}

	if migration.Template != nil {
		// Render template - this should use the planner's template rendering
		values := make(map[string]interface{})
		for _, param := range migration.Template.Parameters {
			if param.Default != "" {
				values[param.Name] = param.Default
			}
		}
		return schemasv1alpha4.RenderTemplate(migration.Template.Template, values)
	}

	return "", fmt.Errorf("migration has neither SQL nor template")
}

// attemptRollback attempts to rollback a failed migration
func (e *MigrationExecutor) attemptRollback(ctx context.Context, tableName string, migration *schemasv1alpha4.DataMigration) error {
	if !migration.Reversible || migration.ReverseSQL == "" {
		return fmt.Errorf("migration %s is not reversible", migration.Name)
	}

	if e.progressCallback != nil {
		e.progressCallback(migration.Name, ProgressInfo{
			Stage: StageRollingBack,
		})
	}

	// Create executor for rollback
	var executor interfaces.DataMigrationExecutor
	var err error

	switch e.driver {
	case "postgres", "cockroachdb", "timescaledb":
		executor, err = e.createPostgresExecutor()
	case "mysql":
		executor, err = e.createMysqlExecutor()
	default:
		return fmt.Errorf("rollback not supported for driver: %s", e.driver)
	}

	if err != nil {
		return errors.Wrap(err, "failed to create executor for rollback")
	}

	// Create a rollback migration
	rollbackMigration := &schemasv1alpha4.DataMigration{
		Name: migration.Name + "-rollback",
		SQL:  migration.ReverseSQL,
	}

	// Execute rollback
	err = executor.ExecuteDataMigration(tableName, rollbackMigration)
	if err != nil {
		return errors.Wrap(err, "failed to execute rollback SQL")
	}

	if e.progressCallback != nil {
		e.progressCallback(migration.Name, ProgressInfo{
			Stage: StageRolledBack,
		})
	}

	return nil
}

// createPostgresExecutor creates a PostgreSQL data migration executor
func (e *MigrationExecutor) createPostgresExecutor() (interfaces.DataMigrationExecutor, error) {
	conn, err := postgres.Connect(e.uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to postgres")
	}
	return NewPostgresDataMigrationExecutor(conn), nil
}

// createMysqlExecutor creates a MySQL data migration executor
func (e *MigrationExecutor) createMysqlExecutor() (interfaces.DataMigrationExecutor, error) {
	conn, err := mysql.Connect(e.uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to mysql")
	}
	return NewMysqlDataMigrationExecutor(conn), nil
}

// PostgresDataMigrationExecutor implements DataMigrationExecutor for PostgreSQL
type PostgresDataMigrationExecutor struct {
	connection *postgres.PostgresConnection
}

// NewPostgresDataMigrationExecutor creates a new PostgreSQL executor
func NewPostgresDataMigrationExecutor(conn *postgres.PostgresConnection) interfaces.DataMigrationExecutor {
	return &PostgresDataMigrationExecutor{
		connection: conn,
	}
}

// ExecuteDataMigration executes a single data migration
func (e *PostgresDataMigrationExecutor) ExecuteDataMigration(tableName string, migration *schemasv1alpha4.DataMigration) error {
	sql, err := e.getMigrationSQL(migration)
	if err != nil {
		return err
	}

	// Execute in a transaction using pgx
	tx, err := e.connection.GetConnection().Begin(context.Background())
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback(context.Background()) // This will be a no-op if we commit

	// Execute the SQL
	result, err := tx.Exec(context.Background(), sql)
	if err != nil {
		return errors.Wrap(err, "failed to execute migration SQL")
	}

	// Log affected rows
	fmt.Printf("Migration %s affected %d rows\n", migration.Name, result.RowsAffected())

	// Commit the transaction
	if err := tx.Commit(context.Background()); err != nil {
		return errors.Wrap(err, "failed to commit migration transaction")
	}

	return nil
}

// ExecuteInBatches executes a migration in batches for large datasets
func (e *PostgresDataMigrationExecutor) ExecuteInBatches(sql string, batchSize int32, batchDelayMs int32) error {
	totalRowsProcessed := int64(0)
	batchCount := int32(0)

	for {
		batchCount++

		// Add LIMIT to SQL if not present
		batchSQL := sql
		if !strings.Contains(strings.ToUpper(sql), "LIMIT") {
			batchSQL = fmt.Sprintf("%s LIMIT %d", sql, batchSize)
		}

		// Execute batch in transaction using pgx
		tx, err := e.connection.GetConnection().Begin(context.Background())
		if err != nil {
			return errors.Wrapf(err, "failed to begin transaction for batch %d", batchCount)
		}

		result, err := tx.Exec(context.Background(), batchSQL)
		if err != nil {
			tx.Rollback(context.Background())
			return errors.Wrapf(err, "failed to execute batch %d", batchCount)
		}

		rowsAffected := result.RowsAffected()

		if err := tx.Commit(context.Background()); err != nil {
			return errors.Wrapf(err, "failed to commit batch %d", batchCount)
		}

		totalRowsProcessed += rowsAffected
		fmt.Printf("Batch %d: processed %d rows (total: %d)\n", batchCount, rowsAffected, totalRowsProcessed)

		// If no rows were affected, we're done
		if rowsAffected == 0 {
			break
		}

		// If less than batch size affected, we're likely done
		if rowsAffected < int64(batchSize) {
			break
		}

		// Delay between batches if specified
		if batchDelayMs > 0 {
			time.Sleep(time.Duration(batchDelayMs) * time.Millisecond)
		}
	}

	fmt.Printf("Batched execution completed: %d batches, %d total rows processed\n", batchCount, totalRowsProcessed)
	return nil
}

// CheckMigrationConditions verifies all conditions before execution
func (e *PostgresDataMigrationExecutor) CheckMigrationConditions(conditions []schemasv1alpha4.DataMigrationCondition) (bool, error) {
	planner := postgres.NewPostgresDataMigrationPlanner(e.connection)

	for _, condition := range conditions {
		result, err := planner.ValidateCondition(condition)
		if err != nil {
			return false, errors.Wrapf(err, "failed to validate condition: %s", condition.Query)
		}
		if !result {
			return false, nil // Condition not met
		}
	}

	return true, nil
}

// getMigrationSQL gets the SQL for a migration
func (e *PostgresDataMigrationExecutor) getMigrationSQL(migration *schemasv1alpha4.DataMigration) (string, error) {
	if migration.SQL != "" {
		return migration.SQL, nil
	}

	if migration.Template != nil {
		values := make(map[string]interface{})
		for _, param := range migration.Template.Parameters {
			if param.Default != "" {
				values[param.Name] = param.Default
			}
		}
		return schemasv1alpha4.RenderTemplate(migration.Template.Template, values)
	}

	return "", fmt.Errorf("migration has neither SQL nor template")
}

// MysqlDataMigrationExecutor implements DataMigrationExecutor for MySQL
type MysqlDataMigrationExecutor struct {
	connection *mysql.MysqlConnection
}

// NewMysqlDataMigrationExecutor creates a new MySQL executor
func NewMysqlDataMigrationExecutor(conn *mysql.MysqlConnection) interfaces.DataMigrationExecutor {
	return &MysqlDataMigrationExecutor{
		connection: conn,
	}
}

// ExecuteDataMigration executes a single data migration
func (e *MysqlDataMigrationExecutor) ExecuteDataMigration(tableName string, migration *schemasv1alpha4.DataMigration) error {
	sql, err := e.getMigrationSQL(migration)
	if err != nil {
		return err
	}

	// Execute in a transaction
	tx, err := e.connection.GetDB().BeginTx(context.Background(), nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback() // This will be a no-op if we commit

	// Execute the SQL
	result, err := tx.ExecContext(context.Background(), sql)
	if err != nil {
		return errors.Wrap(err, "failed to execute migration SQL")
	}

	// Log affected rows
	if rowsAffected, err := result.RowsAffected(); err == nil {
		fmt.Printf("Migration %s affected %d rows\n", migration.Name, rowsAffected)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit migration transaction")
	}

	return nil
}

// ExecuteInBatches executes a migration in batches for large datasets
func (e *MysqlDataMigrationExecutor) ExecuteInBatches(sql string, batchSize int32, batchDelayMs int32) error {
	totalRowsProcessed := int64(0)
	batchCount := int32(0)

	for {
		batchCount++

		// Add LIMIT to SQL if not present (MySQL syntax)
		batchSQL := sql
		if !strings.Contains(strings.ToUpper(sql), "LIMIT") {
			batchSQL = fmt.Sprintf("%s LIMIT %d", sql, batchSize)
		}

		// Execute batch in transaction
		tx, err := e.connection.GetDB().BeginTx(context.Background(), nil)
		if err != nil {
			return errors.Wrapf(err, "failed to begin transaction for batch %d", batchCount)
		}

		result, err := tx.ExecContext(context.Background(), batchSQL)
		if err != nil {
			tx.Rollback()
			return errors.Wrapf(err, "failed to execute batch %d", batchCount)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			tx.Rollback()
			return errors.Wrapf(err, "failed to get rows affected for batch %d", batchCount)
		}

		if err := tx.Commit(); err != nil {
			return errors.Wrapf(err, "failed to commit batch %d", batchCount)
		}

		totalRowsProcessed += rowsAffected
		fmt.Printf("Batch %d: processed %d rows (total: %d)\n", batchCount, rowsAffected, totalRowsProcessed)

		// If no rows were affected, we're done
		if rowsAffected == 0 {
			break
		}

		// If less than batch size affected, we're likely done
		if rowsAffected < int64(batchSize) {
			break
		}

		// Delay between batches if specified
		if batchDelayMs > 0 {
			time.Sleep(time.Duration(batchDelayMs) * time.Millisecond)
		}
	}

	fmt.Printf("Batched execution completed: %d batches, %d total rows processed\n", batchCount, totalRowsProcessed)
	return nil
}

// CheckMigrationConditions verifies all conditions before execution
func (e *MysqlDataMigrationExecutor) CheckMigrationConditions(conditions []schemasv1alpha4.DataMigrationCondition) (bool, error) {
	planner := mysql.NewMysqlDataMigrationPlanner(e.connection)

	for _, condition := range conditions {
		result, err := planner.ValidateCondition(condition)
		if err != nil {
			return false, errors.Wrapf(err, "failed to validate condition: %s", condition.Query)
		}
		if !result {
			return false, nil // Condition not met
		}
	}

	return true, nil
}

// getMigrationSQL gets the SQL for a migration
func (e *MysqlDataMigrationExecutor) getMigrationSQL(migration *schemasv1alpha4.DataMigration) (string, error) {
	if migration.SQL != "" {
		return migration.SQL, nil
	}

	if migration.Template != nil {
		values := make(map[string]interface{})
		for _, param := range migration.Template.Parameters {
			if param.Default != "" {
				values[param.Name] = param.Default
			}
		}
		return schemasv1alpha4.RenderTemplate(migration.Template.Template, values)
	}

	return "", fmt.Errorf("migration has neither SQL nor template")
}
