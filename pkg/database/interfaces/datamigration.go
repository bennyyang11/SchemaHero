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

package interfaces

import (
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// DataMigrationPlanner defines the interface for planning data migrations
type DataMigrationPlanner interface {
	// PlanDataMigrations generates DML statements from data migration specifications
	PlanDataMigrations(tableName string, migrations []schemasv1alpha4.DataMigration) ([]string, error)
	
	// PlanSingleDataMigration generates DML statements for a single data migration
	PlanSingleDataMigration(tableName string, migration *schemasv1alpha4.DataMigration) (string, error)
	
	// ValidateCondition evaluates whether a migration condition is met
	ValidateCondition(condition schemasv1alpha4.DataMigrationCondition) (bool, error)
	
	// EstimateAffectedRows estimates how many rows a migration will affect
	EstimateAffectedRows(tableName string, migration *schemasv1alpha4.DataMigration) (int64, error)
	
	// GetDatabaseSpecificSQL adapts generic SQL for the specific database
	GetDatabaseSpecificSQL(genericSQL string) (string, error)
}

// DataMigrationExecutor defines the interface for executing data migrations
type DataMigrationExecutor interface {
	// ExecuteDataMigration executes a single data migration with batching support
	ExecuteDataMigration(tableName string, migration *schemasv1alpha4.DataMigration) error
	
	// ExecuteInBatches executes a migration in batches for large datasets
	ExecuteInBatches(sql string, batchSize int32, batchDelayMs int32) error
	
	// CheckMigrationConditions verifies all conditions before execution
	CheckMigrationConditions(conditions []schemasv1alpha4.DataMigrationCondition) (bool, error)
}

// DataMigrationResult contains the result of a data migration operation
type DataMigrationResult struct {
	// SQL that was executed
	ExecutedSQL string
	
	// Number of rows affected
	RowsAffected int64
	
	// Duration of execution
	Duration string
	
	// Any warnings or non-fatal errors
	Warnings []string
	
	// Whether the migration was executed in batches
	Batched bool
	
	// Number of batches if batched
	BatchCount int32
} 