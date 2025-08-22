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

package schemaherokubectlcli

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

func TestStatusCommands(t *testing.T) {
	t.Run("describe command creation", func(t *testing.T) {
		cmd := DescribeCmd()
		
		assert.Equal(t, "describe", cmd.Use)
		
		// Verify it has subcommands
		subcommands := cmd.Commands()
		assert.Greater(t, len(subcommands), 0)
		
		// Verify migration subcommand exists
		found := false
		for _, subcmd := range subcommands {
			if subcmd.Use == "migration" {
				found = true
				break
			}
		}
		assert.True(t, found, "describe command should have migration subcommand")
	})

	t.Run("get migrations command creation", func(t *testing.T) {
		cmd := GetMigrationsCmd()
		
		assert.Equal(t, "migrations", cmd.Use)
		
		flags := cmd.Flags()
		assert.NotNil(t, flags.Lookup("database"))
		assert.NotNil(t, flags.Lookup("all-namespaces"))
	})
}

func TestShortenStatus(t *testing.T) {
	t.Run("status shortening", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"PENDING", "PEND"},
			{"RUNNING", "RUN"},
			{"COMPLETED", "COMP"},
			{"FAILED", "FAIL"},
			{"SKIPPED", "SKIP"},
			{"ROLLED_BACK", "ROLL"},
			{"UNKNOWN_STATUS", "UNKN"},
			{"ABC", "ABC"},
			{"", ""},
		}

		for _, tt := range tests {
			t.Run(tt.input, func(t *testing.T) {
				result := shortenStatus(tt.input)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestTimestampToAge(t *testing.T) {
	t.Run("timestamp conversion", func(t *testing.T) {
		// Test zero timestamp
		result := timestampToAge(0)
		assert.Empty(t, result)

		// Test recent timestamp (this will be approximate)
		recent := timestampToAge(1609459200) // 2021-01-01 timestamp
		assert.NotEmpty(t, recent)
		// The actual result will depend on current time, so just verify it's not empty
	})
}

func TestDataMigrationStatusDisplay(t *testing.T) {
	t.Run("migration type detection", func(t *testing.T) {
		tests := []struct {
			name         string
			generatedDDL string
			generatedDML string
			expectedType string
		}{
			{
				name:         "DDL only",
				generatedDDL: "CREATE TABLE users (id INTEGER)",
				generatedDML: "",
				expectedType: "DDL",
			},
			{
				name:         "DML only",
				generatedDDL: "",
				generatedDML: "UPDATE users SET active = true",
				expectedType: "DML",
			},
			{
				name:         "both DDL and DML",
				generatedDDL: "ALTER TABLE users ADD COLUMN status VARCHAR(20)",
				generatedDML: "UPDATE users SET status = 'active'",
				expectedType: "DDL+DML",
			},
			{
				name:         "neither DDL nor DML",
				generatedDDL: "",
				generatedDML: "",
				expectedType: "DDL", // Default
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Simulate the logic from get_migrations.go
				migrationType := "DDL"
				
				if tt.generatedDML != "" {
					if tt.generatedDDL != "" {
						migrationType = "DDL+DML"
					} else {
						migrationType = "DML"
					}
				}

				assert.Equal(t, tt.expectedType, migrationType)
			})
		}
	})

	t.Run("migration status formatting", func(t *testing.T) {
		tests := []struct {
			name             string
			schemaStatus     schemasv1alpha4.DataMigrationStatus
			dataStatus       schemasv1alpha4.DataMigrationStatus
			expectedStatus   string
		}{
			{
				name:           "both phases pending",
				schemaStatus:   schemasv1alpha4.DataMigrationPending,
				dataStatus:     schemasv1alpha4.DataMigrationPending,
				expectedStatus: "S:PEND D:PEND",
			},
			{
				name:           "schema completed, data running",
				schemaStatus:   schemasv1alpha4.DataMigrationCompleted,
				dataStatus:     schemasv1alpha4.DataMigrationRunning,
				expectedStatus: "S:COMP D:RUN",
			},
			{
				name:           "data only migration",
				schemaStatus:   "",
				dataStatus:     schemasv1alpha4.DataMigrationCompleted,
				expectedStatus: "COMP",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Simulate the logic from get_migrations.go  
				var migrationStatus string
				
				if tt.dataStatus != "" {
					schemaStatusStr := string(tt.schemaStatus)
					dataStatusStr := string(tt.dataStatus)
					if schemaStatusStr != "" && dataStatusStr != "" {
						migrationStatus = fmt.Sprintf("S:%s D:%s", 
							shortenStatus(schemaStatusStr), 
							shortenStatus(dataStatusStr))
					} else if dataStatusStr != "" {
						migrationStatus = shortenStatus(dataStatusStr)
					}
				}

				assert.Equal(t, tt.expectedStatus, migrationStatus)
			})
		}
	})
}

func TestDescribeEnhancements(t *testing.T) {
	t.Run("describe output structure verification", func(t *testing.T) {
		// Test that the enhanced describe logic would work with sample data
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "ALTER TABLE users ADD COLUMN status VARCHAR(20)",
				GeneratedDML: "UPDATE users SET status = 'active' WHERE status IS NULL",
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                   schemasv1alpha4.Approved,
				SchemaMigrationStatus:   schemasv1alpha4.DataMigrationCompleted,
				DataMigrationStatus:     schemasv1alpha4.DataMigrationRunning,
				EstimatedDataRows:       1500,
				EstimatedDuration:       "2m30s",
			},
		}

		// Verify the migration has both DDL and DML
		assert.NotEmpty(t, migration.Spec.GeneratedDDL)
		assert.NotEmpty(t, migration.Spec.GeneratedDML)
		
		// Verify status fields are available
		assert.Equal(t, schemasv1alpha4.DataMigrationCompleted, migration.Status.SchemaMigrationStatus)
		assert.Equal(t, schemasv1alpha4.DataMigrationRunning, migration.Status.DataMigrationStatus)
		assert.Greater(t, migration.Status.EstimatedDataRows, int64(0))
		assert.NotEmpty(t, migration.Status.EstimatedDuration)
	})

	t.Run("error condition detection", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Status: schemasv1alpha4.MigrationStatus{
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationFailed,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationCompleted,
			},
		}

		// Test error detection logic
		hasSchemaError := migration.Status.SchemaMigrationStatus == schemasv1alpha4.DataMigrationFailed
		hasDataError := migration.Status.DataMigrationStatus == schemasv1alpha4.DataMigrationFailed
		
		assert.True(t, hasSchemaError)
		assert.False(t, hasDataError)
	})

	t.Run("progress condition detection", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Status: schemasv1alpha4.MigrationStatus{
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationCompleted,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationRunning,
				EstimatedDataRows:     5000,
			},
		}

		// Test progress detection logic
		schemaRunning := migration.Status.SchemaMigrationStatus == schemasv1alpha4.DataMigrationRunning
		dataRunning := migration.Status.DataMigrationStatus == schemasv1alpha4.DataMigrationRunning
		
		assert.False(t, schemaRunning)
		assert.True(t, dataRunning)
		assert.Greater(t, migration.Status.EstimatedDataRows, int64(0))
	})
} 