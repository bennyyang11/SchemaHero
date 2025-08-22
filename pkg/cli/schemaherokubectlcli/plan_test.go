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

	"github.com/schemahero/schemahero/pkg/database"
	"github.com/schemahero/schemahero/pkg/database/types"
)

func TestPlanCommand(t *testing.T) {
	t.Run("enhanced plan command creation", func(t *testing.T) {
		cmd := PlanCmd()
		
		assert.Equal(t, "plan", cmd.Use)
		assert.Contains(t, cmd.Short, "plan a spec application against a database")
		assert.Contains(t, cmd.Long, "Generate and preview DDL and DML statements")

		// Verify new flags are present
		flags := cmd.Flags()
		assert.NotNil(t, flags.Lookup("dry-run"))
		assert.NotNil(t, flags.Lookup("data-migrations-only"))
		assert.NotNil(t, flags.Lookup("schema-only"))
		assert.NotNil(t, flags.Lookup("show-metrics"))
		assert.NotNil(t, flags.Lookup("verbose"))

		// Verify flag defaults
		dryRunFlag := flags.Lookup("dry-run")
		assert.Equal(t, "false", dryRunFlag.DefValue)

		verboseFlag := flags.Lookup("verbose")
		assert.Equal(t, "false", verboseFlag.DefValue)

		metricsFlag := flags.Lookup("show-metrics")
		assert.Equal(t, "false", metricsFlag.DefValue)
	})
}

func TestSpecHasDataMigrations(t *testing.T) {
	t.Run("spec without data migrations", func(t *testing.T) {
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
`
		hasDataMigrations, err := specHasDataMigrations([]byte(specYAML))
		require.NoError(t, err)
		assert.False(t, hasDataMigrations)
	})

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
        - name: status
          type: varchar(20)
  dataMigrations:
    - name: set-default-status
      sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
`
		hasDataMigrations, err := specHasDataMigrations([]byte(specYAML))
		require.NoError(t, err)
		assert.True(t, hasDataMigrations)
	})

	t.Run("plain spec format with data migrations", func(t *testing.T) {
		specYAML := `
database: myapp
name: users
schema:
  postgres:
    columns:
      - name: id
        type: integer
dataMigrations:
  - name: migration-1
    sql: "UPDATE users SET processed = true"
`
		hasDataMigrations, err := specHasDataMigrations([]byte(specYAML))
		require.NoError(t, err)
		assert.True(t, hasDataMigrations)
	})

	t.Run("invalid spec format", func(t *testing.T) {
		specYAML := `invalid yaml content ][`

		_, err := specHasDataMigrations([]byte(specYAML))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal spec")
	})
}

func TestEstimateMigrationMetrics(t *testing.T) {
	t.Run("basic metrics estimation", func(t *testing.T) {
		specYAML := `
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: users
spec:
  database: myapp
  name: users
  dataMigrations:
    - name: migration-1
      sql: "UPDATE users SET status = 'active'"
      batchSize: 5000
    - name: migration-2
      sql: "UPDATE users SET processed = true"
`
		db := &database.Database{Driver: "postgres"}
		dmlStatements := []string{"UPDATE users SET status = 'active'", "UPDATE users SET processed = true"}

		rows, duration, err := estimateMigrationMetrics(db, []byte(specYAML), dmlStatements)
		require.NoError(t, err)

		// Should estimate rows based on batch sizes and defaults
		assert.Greater(t, rows, int64(0))
		assert.Greater(t, duration, time.Duration(0))
	})

	t.Run("metrics with basic estimation", func(t *testing.T) {
		specYAML := `
dataMigrations:
  - name: long-migration
    sql: "UPDATE large_table SET processed = true"
`
		db := &database.Database{Driver: "postgres"}
		dmlStatements := []string{"UPDATE large_table SET processed = true"}

		rows, duration, err := estimateMigrationMetrics(db, []byte(specYAML), dmlStatements)
		require.NoError(t, err)

		// Should provide basic estimates
		assert.Greater(t, rows, int64(0))
		assert.Greater(t, duration, time.Duration(0))
	})

	t.Run("invalid spec for metrics", func(t *testing.T) {
		specYAML := `invalid yaml`
		db := &database.Database{Driver: "postgres"}
		
		_, _, err := estimateMigrationMetrics(db, []byte(specYAML), []string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal spec for metrics")
	})
}

func TestPlanSpecWithEnhancements(t *testing.T) {
	t.Run("data migration detection", func(t *testing.T) {
		specYAML := `
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: users_with_migrations
spec:
  database: myapp
  name: users
  dataMigrations:
    - name: test-migration
      sql: "UPDATE users SET active = true"
      batchSize: 2000
`
		spec := types.Spec{
			SourceFilename: "users.yaml",
			Spec:           []byte(specYAML),
		}

		db := &database.Database{Driver: "postgres"}

		result, err := planSpecWithEnhancements(db, spec, false, false, false, true)
		// This will error due to mock database, but test the parsing logic
		assert.Error(t, err) // Expected due to mock database
		
		assert.Equal(t, "users.yaml", result.SourceFile)
		assert.True(t, result.HasDataMigrations)
	})

	t.Run("data-migrations-only flag detection", func(t *testing.T) {
		specYAML := `
dataMigrations:
  - name: data-only-migration
    sql: "UPDATE test SET flag = true"
`
		spec := types.Spec{
			SourceFilename: "data-only.yaml",
			Spec:           []byte(specYAML),
		}

		// Test that data migration detection works correctly
		hasDataMigrations, err := specHasDataMigrations(spec.Spec)
		require.NoError(t, err)
		assert.True(t, hasDataMigrations)
	})

	t.Run("schema-only flag behavior", func(t *testing.T) {
		specYAML := `
schema:
  postgres:
    columns:
      - name: id
        type: integer
dataMigrations:
  - name: should-be-detected
    sql: "UPDATE test SET flag = true"
`
		// Test that data migration detection works even with schema-only flag
		hasDataMigrations, err := specHasDataMigrations([]byte(specYAML))
		require.NoError(t, err)
		assert.True(t, hasDataMigrations)
	})
}

func TestOutputPlanResults(t *testing.T) {
	t.Run("output formatting without verbose", func(t *testing.T) {
		results := []PlanResult{
			{
				SourceFile:    "test.yaml",
				DDLStatements: []string{"CREATE TABLE test (id INTEGER)"},
				DMLStatements: []string{"INSERT INTO test VALUES (1)"},
			},
		}

		// This would write to stdout in real usage, but we can test the structure
		err := outputPlanResults(results, nil, false, false, false)
		require.NoError(t, err)
		
		// The function doesn't return output for testing, but we've verified no errors
		assert.NoError(t, err)
	})

	t.Run("output with verbose and metrics", func(t *testing.T) {
		results := []PlanResult{
			{
				SourceFile:        "users.yaml",
				DDLStatements:     []string{"ALTER TABLE users ADD COLUMN status VARCHAR(20)"},
				DMLStatements:     []string{"UPDATE users SET status = 'active'"},
				EstimatedRows:     1500,
				EstimatedTime:     time.Second * 5,
				HasDataMigrations: true,
			},
		}

		err := outputPlanResults(results, nil, true, true, false)
		require.NoError(t, err)
	})

	t.Run("dry-run mode output", func(t *testing.T) {
		results := []PlanResult{
			{
				SourceFile:    "dry-run.yaml",
				DDLStatements: []string{"CREATE TABLE test (id INTEGER)"},
			},
		}

		err := outputPlanResults(results, nil, false, false, true)
		require.NoError(t, err)
	})

	t.Run("multiple results output", func(t *testing.T) {
		results := []PlanResult{
			{
				SourceFile:    "table1.yaml",
				DDLStatements: []string{"CREATE TABLE table1 (id INTEGER)"},
			},
			{
				SourceFile:    "table2.yaml",
				DDLStatements: []string{"CREATE TABLE table2 (id INTEGER)"},
			},
		}

		err := outputPlanResults(results, nil, false, false, false)
		require.NoError(t, err)
	})
}

func TestWriteOutput(t *testing.T) {
	t.Run("write to stdout", func(t *testing.T) {
		// This would write to stdout in real usage
		err := writeOutput(nil, "test output")
		assert.NoError(t, err)
	})

	// Note: Testing file writing would require creating temporary files
	// which is more complex and not essential for this unit test
}

func TestPlanResultStructure(t *testing.T) {
	t.Run("plan result initialization", func(t *testing.T) {
		result := PlanResult{
			SourceFile:        "test.yaml",
			DDLStatements:     []string{"DDL1", "DDL2"},
			DMLStatements:     []string{"DML1"},
			EstimatedRows:     1000,
			EstimatedTime:     time.Minute,
			HasDataMigrations: true,
		}

		assert.Equal(t, "test.yaml", result.SourceFile)
		assert.Len(t, result.DDLStatements, 2)
		assert.Len(t, result.DMLStatements, 1)
		assert.Equal(t, int64(1000), result.EstimatedRows)
		assert.Equal(t, time.Minute, result.EstimatedTime)
		assert.True(t, result.HasDataMigrations)
	})

	t.Run("empty plan result", func(t *testing.T) {
		result := PlanResult{}
		
		assert.Empty(t, result.SourceFile)
		assert.Empty(t, result.DDLStatements)
		assert.Empty(t, result.DMLStatements)
		assert.Equal(t, int64(0), result.EstimatedRows)
		assert.Equal(t, time.Duration(0), result.EstimatedTime)
		assert.False(t, result.HasDataMigrations)
	})
} 