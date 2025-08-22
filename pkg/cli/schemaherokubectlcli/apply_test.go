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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"
)

func TestApplyCommand(t *testing.T) {
	t.Run("enhanced apply command creation", func(t *testing.T) {
		cmd := ApplyCmd()
		
		assert.Equal(t, "apply", cmd.Use)
		assert.Contains(t, cmd.Short, "apply a spec to a database")
		assert.Contains(t, cmd.Long, "Execute DDL and DML statements")

		// Verify new flags are present
		flags := cmd.Flags()
		assert.NotNil(t, flags.Lookup("spec-file"))
		assert.NotNil(t, flags.Lookup("dry-run"))
		assert.NotNil(t, flags.Lookup("data-migrations-only"))
		assert.NotNil(t, flags.Lookup("schema-only"))
		assert.NotNil(t, flags.Lookup("skip-confirmation"))
		assert.NotNil(t, flags.Lookup("show-progress"))
		assert.NotNil(t, flags.Lookup("verbose"))

		// Verify legacy DDL flag is still there
		assert.NotNil(t, flags.Lookup("ddl"))

		// Verify flag defaults
		dryRunFlag := flags.Lookup("dry-run")
		assert.Equal(t, "false", dryRunFlag.DefValue)

		progressFlag := flags.Lookup("show-progress")
		assert.Equal(t, "true", progressFlag.DefValue)
	})
}

func TestCheckDestructiveOperations(t *testing.T) {
	t.Run("safe operations", func(t *testing.T) {
		spec := &schemasv1alpha4.TableSpec{
			DataMigrations: []schemasv1alpha4.DataMigration{
				{
					Name: "safe-update",
					SQL:  "UPDATE users SET status = 'active' WHERE id = 123",
				},
				{
					Name: "safe-insert",
					SQL:  "INSERT INTO users (name, email) VALUES ('test', 'test@example.com')",
				},
			},
		}

		needsConfirmation, reason := checkDestructiveOperations(spec)
		assert.False(t, needsConfirmation)
		assert.Empty(t, reason)
	})

	t.Run("destructive operations", func(t *testing.T) {
		tests := []struct {
			name     string
			sql      string
			expected string
		}{
			{
				name:     "drop table",
				sql:      "DROP TABLE old_users",
				expected: "DROP TABLE operation detected",
			},
			{
				name:     "truncate",
				sql:      "TRUNCATE TABLE logs",
				expected: "TRUNCATE operation detected",
			},
			{
				name:     "mass delete",
				sql:      "DELETE FROM users",
				expected: "Mass DELETE without WHERE clause detected",
			},
			{
				name:     "mass update",
				sql:      "UPDATE users SET deleted = true",
				expected: "Mass UPDATE without WHERE clause detected",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				spec := &schemasv1alpha4.TableSpec{
					DataMigrations: []schemasv1alpha4.DataMigration{
						{
							Name: "destructive-op",
							SQL:  tt.sql,
						},
					},
				}

				needsConfirmation, reason := checkDestructiveOperations(spec)
				assert.True(t, needsConfirmation)
				assert.Equal(t, tt.expected, reason)
			})
		}
	})

	t.Run("no data migrations", func(t *testing.T) {
		spec := &schemasv1alpha4.TableSpec{}

		needsConfirmation, reason := checkDestructiveOperations(spec)
		assert.False(t, needsConfirmation)
		assert.Empty(t, reason)
	})

	t.Run("case insensitive detection", func(t *testing.T) {
		spec := &schemasv1alpha4.TableSpec{
			DataMigrations: []schemasv1alpha4.DataMigration{
				{
					Name: "case-test",
					SQL:  "drop table users",
				},
			},
		}

		needsConfirmation, reason := checkDestructiveOperations(spec)
		assert.True(t, needsConfirmation)
		assert.Equal(t, "DROP TABLE operation detected", reason)
	})
}

func TestApplyResult(t *testing.T) {
	t.Run("apply result structure", func(t *testing.T) {
		result := ApplyResult{
			SourceFile:     "test.yaml",
			SchemaApplied:  true,
			DataApplied:    true,
			RowsAffected:   1500,
			Duration:       time.Second * 5,
			Warnings:       []string{"warning1", "warning2"},
			Errors:         []string{},
		}

		assert.Equal(t, "test.yaml", result.SourceFile)
		assert.True(t, result.SchemaApplied)
		assert.True(t, result.DataApplied)
		assert.Equal(t, int64(1500), result.RowsAffected)
		assert.Equal(t, time.Second*5, result.Duration)
		assert.Len(t, result.Warnings, 2)
		assert.Empty(t, result.Errors)
	})

	t.Run("empty apply result", func(t *testing.T) {
		result := ApplyResult{}
		
		assert.Empty(t, result.SourceFile)
		assert.False(t, result.SchemaApplied)
		assert.False(t, result.DataApplied)
		assert.Equal(t, int64(0), result.RowsAffected)
		assert.Equal(t, time.Duration(0), result.Duration)
		assert.Empty(t, result.Warnings)
		assert.Empty(t, result.Errors)
	})
}

func TestProgressTracker(t *testing.T) {
	t.Run("progress tracker initialization", func(t *testing.T) {
		tracker := ProgressTracker{
			currentMigration: "test-migration",
			startTime:        time.Now(),
			totalRows:        1000,
			processedRows:    500,
		}

		assert.Equal(t, "test-migration", tracker.currentMigration)
		assert.False(t, tracker.startTime.IsZero())
		assert.Equal(t, int64(1000), tracker.totalRows)
		assert.Equal(t, int64(500), tracker.processedRows)
	})
}

func TestApplySpecParsing(t *testing.T) {
	t.Run("spec with data migrations", func(t *testing.T) {
		specYAML := `
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: users
spec:
  database: myapp
  name: users
  schema:
    postgres:
      columns:
        - name: id
          type: integer
  dataMigrations:
    - name: set-status
      sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
`
		spec := types.Spec{
			SourceFilename: "users.yaml",
			Spec:           []byte(specYAML),
		}

		// Test that we can detect data migrations in the spec
		hasDataMigrations, err := specHasDataMigrations(spec.Spec)
		require.NoError(t, err)
		assert.True(t, hasDataMigrations)
	})

	t.Run("spec without data migrations", func(t *testing.T) {
		specYAML := `
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: simple_table
spec:
  database: myapp
  name: simple_table
  schema:
    postgres:
      columns:
        - name: id
          type: integer
`
		spec := types.Spec{
			SourceFilename: "simple.yaml",
			Spec:           []byte(specYAML),
		}

		hasDataMigrations, err := specHasDataMigrations(spec.Spec)
		require.NoError(t, err)
		assert.False(t, hasDataMigrations)
	})
}

func TestOutputApplyResults(t *testing.T) {
	t.Run("successful results summary", func(t *testing.T) {
		results := []ApplyResult{
			{
				SourceFile:    "table1.yaml",
				SchemaApplied: true,
				DataApplied:   false,
				Duration:      time.Second * 2,
				Warnings:      []string{},
				Errors:        []string{},
			},
			{
				SourceFile:    "table2.yaml",
				SchemaApplied: true,
				DataApplied:   true,
				RowsAffected:  1500,
				Duration:      time.Second * 3,
				Warnings:      []string{},
				Errors:        []string{},
			},
		}

		// This would output to stdout in real usage, but we can test the structure
		err := outputApplyResults(results, false)
		assert.NoError(t, err)
	})

	t.Run("results with errors", func(t *testing.T) {
		results := []ApplyResult{
			{
				SourceFile: "failed.yaml",
				Errors:     []string{"execution failed"},
			},
		}

		err := outputApplyResults(results, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "encountered 1 errors")
	})

	t.Run("results with warnings", func(t *testing.T) {
		results := []ApplyResult{
			{
				SourceFile:    "warned.yaml",
				SchemaApplied: true,
				Warnings:      []string{"non-critical warning"},
			},
		}

		err := outputApplyResults(results, true)
		assert.NoError(t, err)
	})
}

func TestApplyCommandValidation(t *testing.T) {
	t.Run("command line validation logic", func(t *testing.T) {
		// Test the validation logic by examining the command structure
		cmd := ApplyCmd()
		
		// The command should accept either --ddl or --spec-file but not both
		flags := cmd.Flags()
		
		// DDL mode flags
		ddlFlag := flags.Lookup("ddl")
		assert.NotNil(t, ddlFlag)
		assert.Equal(t, "", ddlFlag.DefValue)

		// Spec mode flags  
		specFileFlag := flags.Lookup("spec-file")
		assert.NotNil(t, specFileFlag)
		assert.Equal(t, "", specFileFlag.DefValue)

		// Enhanced options
		dryRunFlag := flags.Lookup("dry-run")
		assert.NotNil(t, dryRunFlag)
		assert.Equal(t, "false", dryRunFlag.DefValue)

		dataOnlyFlag := flags.Lookup("data-migrations-only")
		assert.NotNil(t, dataOnlyFlag)
		assert.Equal(t, "false", dataOnlyFlag.DefValue)

		schemaOnlyFlag := flags.Lookup("schema-only")
		assert.NotNil(t, schemaOnlyFlag)
		assert.Equal(t, "false", schemaOnlyFlag.DefValue)
	})
}

func TestDestructiveOperationDetection(t *testing.T) {
	t.Run("edge cases in destructive operation detection", func(t *testing.T) {
		tests := []struct {
			name        string
			sql         string
			destructive bool
			reason      string
		}{
			{
				name:        "delete with where clause",
				sql:         "DELETE FROM users WHERE active = false",
				destructive: false,
			},
			{
				name:        "update with where clause",
				sql:         "UPDATE users SET last_login = NOW() WHERE id = 123",
				destructive: false,
			},
			{
				name:        "select statement",
				sql:         "SELECT * FROM users",
				destructive: false,
			},
			{
				name:        "insert statement",
				sql:         "INSERT INTO users (name) VALUES ('test')",
				destructive: false,
			},
			{
				name:        "drop table mixed case",
				sql:         "Drop Table users",
				destructive: true,
				reason:      "DROP TABLE operation detected",
			},
			{
				name:        "truncate with whitespace",
				sql:         "  TRUNCATE TABLE   logs  ",
				destructive: true,
				reason:      "TRUNCATE operation detected",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				spec := &schemasv1alpha4.TableSpec{
					DataMigrations: []schemasv1alpha4.DataMigration{
						{
							Name: "test-migration",
							SQL:  tt.sql,
						},
					},
				}

				needsConfirmation, reason := checkDestructiveOperations(spec)
				assert.Equal(t, tt.destructive, needsConfirmation, "destructive detection mismatch for SQL: %s", tt.sql)
				if tt.destructive {
					assert.Equal(t, tt.reason, reason)
				} else {
					assert.Empty(t, reason)
				}
			})
		}
	})
}

func TestApplyModeSelection(t *testing.T) {
	t.Run("apply mode flags interaction", func(t *testing.T) {
		cmd := ApplyCmd()
		flags := cmd.Flags()

		// Verify mutually exclusive flags exist
		dataOnlyFlag := flags.Lookup("data-migrations-only")
		schemaOnlyFlag := flags.Lookup("schema-only")
		
		assert.NotNil(t, dataOnlyFlag)
		assert.NotNil(t, schemaOnlyFlag)
		
		// Both should default to false
		assert.Equal(t, "false", dataOnlyFlag.DefValue)
		assert.Equal(t, "false", schemaOnlyFlag.DefValue)
	})
}

func TestApplySpecModeFeatures(t *testing.T) {
	t.Run("spec mode features", func(t *testing.T) {
		// Test the structures and functions that would be used in spec mode
		
		// ApplyResult tracking
		result := ApplyResult{
			SourceFile:    "test.yaml",
			SchemaApplied: true,
			DataApplied:   true,
			Duration:      time.Minute,
		}
		
		assert.True(t, result.SchemaApplied)
		assert.True(t, result.DataApplied)
		assert.Equal(t, time.Minute, result.Duration)

		// Progress tracking
		tracker := ProgressTracker{
			currentMigration: "data-migration-1",
			startTime:        time.Now(),
			totalRows:        10000,
			processedRows:    5000,
		}
		
		assert.Equal(t, "data-migration-1", tracker.currentMigration)
		assert.Equal(t, int64(10000), tracker.totalRows)
		assert.Equal(t, int64(5000), tracker.processedRows)
	})
} 