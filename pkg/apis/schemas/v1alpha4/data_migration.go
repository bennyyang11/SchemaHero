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

package v1alpha4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DataMigrationCondition defines conditions for executing a data migration
type DataMigrationCondition struct {
	// SQL query that should return a single row with a single numeric column
	// The migration will execute if the result matches the operator and value
	Query    string `json:"query" yaml:"query"`
	
	// Operator for comparison: >, <, >=, <=, =, !=, EXISTS, NOT EXISTS
	// +kubebuilder:validation:Enum=">","<",">=","<=","=","!=","EXISTS","NOT EXISTS"
	Operator string `json:"operator" yaml:"operator"`
	
	// Value to compare against (ignored for EXISTS/NOT EXISTS)
	Value    int64  `json:"value,omitempty" yaml:"value,omitempty"`
}

// DataMigration defines a data transformation to be applied to a table
type DataMigration struct {
	// Name is a unique identifier for this migration step
	Name string `json:"name" yaml:"name"`

	// Description provides human-readable context for this migration
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// SQL contains the DML statements to execute
	SQL string `json:"sql,omitempty" yaml:"sql,omitempty"`

	// Template defines a parameterized SQL template
	Template *DataMigrationTemplate `json:"template,omitempty" yaml:"template,omitempty"`

	// Conditions that must be met for this migration to execute
	// All conditions must evaluate to true
	Conditions []DataMigrationCondition `json:"conditions,omitempty" yaml:"conditions,omitempty"`

	// DependsOn lists migration names that must complete before this one
	DependsOn []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`

	// BatchSize for operations that should be processed in chunks
	// 0 means no batching (execute as single statement)
	BatchSize int32 `json:"batchSize,omitempty" yaml:"batchSize,omitempty"`

	// BatchDelayMs is the delay in milliseconds between batches
	BatchDelayMs int32 `json:"batchDelayMs,omitempty" yaml:"batchDelayMs,omitempty"`

	// Timeout duration for this migration (e.g., "30m", "1h")
	// Uses Kubernetes duration format
	Timeout *metav1.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// Type indicates the type of data migration
	Type DataMigrationType `json:"type,omitempty" yaml:"type,omitempty"`

	// Reversible indicates if this migration can be rolled back
	Reversible bool `json:"reversible,omitempty" yaml:"reversible,omitempty"`

	// ReverseSQL contains the SQL to reverse this migration
	ReverseSQL string `json:"reverseSQL,omitempty" yaml:"reverseSQL,omitempty"`

	// Priority for execution ordering (higher executes first)
	// Migrations with same priority are ordered by dependency
	Priority int32 `json:"priority,omitempty" yaml:"priority,omitempty"`

	// Tags for categorizing migrations
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// DataMigrationType categorizes the type of data migration
type DataMigrationType string

const (
	// DataMigrationTypeBackfill fills new columns with data
	DataMigrationTypeBackfill DataMigrationType = "BACKFILL"
	
	// DataMigrationTypeTransform modifies existing data
	DataMigrationTypeTransform DataMigrationType = "TRANSFORM"
	
	// DataMigrationTypeCleanup removes obsolete data
	DataMigrationTypeCleanup DataMigrationType = "CLEANUP"
	
	// DataMigrationTypeCopy copies data between tables
	DataMigrationTypeCopy DataMigrationType = "COPY"
	
	// DataMigrationTypeCustom for custom migration logic
	DataMigrationTypeCustom DataMigrationType = "CUSTOM"
)

// DataMigrationStatus tracks the execution state of a data migration
type DataMigrationStatus string

const (
	DataMigrationPending   DataMigrationStatus = "PENDING"
	DataMigrationRunning   DataMigrationStatus = "RUNNING"
	DataMigrationCompleted DataMigrationStatus = "COMPLETED"
	DataMigrationFailed    DataMigrationStatus = "FAILED"
	DataMigrationSkipped   DataMigrationStatus = "SKIPPED"
	DataMigrationRolledBack DataMigrationStatus = "ROLLED_BACK"
)

// BatchConfiguration defines how to process data in batches
type BatchConfiguration struct {
	// Size of each batch
	Size int32 `json:"size" yaml:"size"`
	
	// Delay between batches in milliseconds
	DelayMs int32 `json:"delayMs,omitempty" yaml:"delayMs,omitempty"`
	
	// Column to use for ordering batches (usually primary key)
	OrderBy string `json:"orderBy,omitempty" yaml:"orderBy,omitempty"`
	
	// Whether to process batches in parallel
	Parallel bool `json:"parallel,omitempty" yaml:"parallel,omitempty"`
	
	// Maximum parallel batches
	MaxParallel int32 `json:"maxParallel,omitempty" yaml:"maxParallel,omitempty"`
}

// DependencyConfiguration defines how migrations depend on each other
type DependencyConfiguration struct {
	// Hard dependencies that must complete successfully
	Required []string `json:"required,omitempty" yaml:"required,omitempty"`
	
	// Soft dependencies that should complete but won't block
	Optional []string `json:"optional,omitempty" yaml:"optional,omitempty"`
	
	// Dependencies that must fail for this to run
	FailedOnly []string `json:"failedOnly,omitempty" yaml:"failedOnly,omitempty"`
} 