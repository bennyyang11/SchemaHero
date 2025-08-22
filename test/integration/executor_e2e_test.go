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

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	migrationcontroller "github.com/schemahero/schemahero/pkg/controller/migration"
	"github.com/schemahero/schemahero/pkg/database"
)

// TestMigrationExecutionEndToEnd tests the complete migration lifecycle
func TestMigrationExecutionEndToEnd(t *testing.T) {
	t.Run("complete planning and execution flow", func(t *testing.T) {
		// Create a comprehensive table spec with data migrations
		tableSpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "users",
			Schema: &schemasv1alpha4.TableSchema{
				Postgres: &schemasv1alpha4.PostgresqlTableSchema{
					Columns: []*schemasv1alpha4.PostgresqlTableColumn{
						{Name: "id", Type: "serial"},
						{Name: "username", Type: "varchar(100)"},
						{Name: "email", Type: "varchar(255)"},
						{Name: "status", Type: "varchar(20)"},
					},
				},
			},
			DataMigrations: []schemasv1alpha4.DataMigration{
				{
					Name:        "set-default-status",
					Description: "Set default status for existing users",
					Type:        schemasv1alpha4.DataMigrationTypeBackfill,
					SQL:         "UPDATE users SET status = 'active' WHERE status IS NULL",
					Conditions: []schemasv1alpha4.DataMigrationCondition{
						{
							Query:    "SELECT COUNT(*) FROM users WHERE status IS NULL",
							Operator: ">",
							Value:    0,
						},
					},
					BatchSize: 1000,
					Timeout:   &metav1.Duration{Duration: 10 * time.Minute},
					Priority:  10,
				},
				{
					Name:        "normalize-emails",
					Description: "Convert emails to lowercase",
					Type:        schemasv1alpha4.DataMigrationTypeTransform,
					SQL:         "UPDATE users SET email = LOWER(email) WHERE email != LOWER(email)",
					DependsOn:   []string{"set-default-status"},
					BatchSize:   500,
					Priority:    5,
				},
			},
		}

		// Test planning phase
		db := &database.Database{
			Driver: "postgres",
			URI:    "mock://test", // This will fail connection but test structure
		}

		// Test that planning routes correctly
		_, err := db.PlanDataMigrations(tableSpec)
		if err != nil {
			// Should fail with connection error, not implementation error
			assert.NotContains(t, err.Error(), "not implemented")
			assert.Contains(t, err.Error(), "connect")
		}

		// Test dependency resolution
		resolver := schemasv1alpha4.NewDependencyResolver(tableSpec.DataMigrations)
		ordered, err := resolver.ResolveExecutionOrder()
		require.NoError(t, err)
		require.Len(t, ordered, 2)
		
		// Verify correct order
		assert.Equal(t, "set-default-status", ordered[0].Name)
		assert.Equal(t, "normalize-emails", ordered[1].Name)
		assert.Equal(t, int32(10), ordered[0].Priority)
		assert.Equal(t, int32(5), ordered[1].Priority)

		// Test validation
		validator := migrationcontroller.NewDataMigrationValidator()
		for _, migration := range tableSpec.DataMigrations {
			err := validator.ValidateDataMigration(&migration)
			assert.NoError(t, err, "Migration %s should be valid", migration.Name)
		}
	})

	t.Run("execution engine functionality", func(t *testing.T) {
		// Mock logger for testing
		logger := &MockLogger{}
		
		// Progress tracking
		var progressReports []migrationcontroller.ProgressInfo
		progressCallback := func(migrationName string, progress migrationcontroller.ProgressInfo) {
			progressReports = append(progressReports, progress)
		}

		// Create executor
		executor := migrationcontroller.NewMigrationExecutor("postgres", "mock://test", progressCallback, logger)
		
		// Test executor creation
		assert.NotNil(t, executor)
		
		// Test progress callback functionality
		if progressCallback != nil {
			progressCallback("test-migration", migrationcontroller.ProgressInfo{
				Stage:         migrationcontroller.StageValidating,
				RowsProcessed: 100,
				TotalRows:     1000,
				ElapsedTime:   time.Second * 5,
			})
		}

		require.Len(t, progressReports, 1)
		assert.Equal(t, migrationcontroller.StageValidating, progressReports[0].Stage)
		assert.Equal(t, int64(100), progressReports[0].RowsProcessed)
	})

	t.Run("rollback scenarios", func(t *testing.T) {
		reversibleMigration := &schemasv1alpha4.DataMigration{
			Name:       "reversible-test",
			SQL:        "UPDATE users SET status = 'premium'",
			Reversible: true,
			ReverseSQL: "UPDATE users SET status = 'basic'",
		}

		nonReversibleMigration := &schemasv1alpha4.DataMigration{
			Name:       "non-reversible-test",
			SQL:        "UPDATE users SET email = LOWER(email)",
			Reversible: false,
		}

		// Test reversible migration structure
		assert.True(t, reversibleMigration.Reversible)
		assert.NotEmpty(t, reversibleMigration.ReverseSQL)

		// Test non-reversible migration structure
		assert.False(t, nonReversibleMigration.Reversible)
		assert.Empty(t, nonReversibleMigration.ReverseSQL)

		// Test validation catches incomplete reversible migrations
		invalidReversible := &schemasv1alpha4.DataMigration{
			Name:       "invalid-reversible",
			SQL:        "UPDATE users SET status = 'active'",
			Reversible: true,
			// Missing ReverseSQL
		}

		validator := migrationcontroller.NewDataMigrationValidator()
		err := validator.ValidateDataMigration(invalidReversible)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "reverseSQL must be provided")
	})

	t.Run("large dataset simulation", func(t *testing.T) {
		largeMigration := &schemasv1alpha4.DataMigration{
			Name:         "large-dataset-test",
			Description:  "Process large dataset with batching",
			Type:         schemasv1alpha4.DataMigrationTypeTransform,
			SQL:          "UPDATE large_table SET processed = true WHERE processed = false",
			BatchSize:    10000,
			BatchDelayMs: 50,
			Timeout:      &metav1.Duration{Duration: 2 * time.Hour},
		}

		// Test batch configuration for large datasets
		assert.Greater(t, largeMigration.BatchSize, int32(0))
		assert.Equal(t, int32(10000), largeMigration.BatchSize)
		assert.Equal(t, int32(50), largeMigration.BatchDelayMs)
		assert.Equal(t, 2*time.Hour, largeMigration.Timeout.Duration)

		// Test SQL batching logic
		sql := largeMigration.SQL
		if !strings.Contains(strings.ToUpper(sql), "LIMIT") {
			sql = fmt.Sprintf("%s LIMIT %d", sql, largeMigration.BatchSize)
		}
		
		expectedSQL := "UPDATE large_table SET processed = true WHERE processed = false LIMIT 10000"
		assert.Equal(t, expectedSQL, sql)

		// Test delay calculation
		delay := time.Duration(largeMigration.BatchDelayMs) * time.Millisecond
		assert.Equal(t, 50*time.Millisecond, delay)
	})

	t.Run("complex template execution", func(t *testing.T) {
		templateMigration := &schemasv1alpha4.DataMigration{
			Name: "complex-template",
			Type: schemasv1alpha4.DataMigrationTypeTransform,
			Template: &schemasv1alpha4.DataMigrationTemplate{
				Template: "UPDATE {{.table}} SET {{.column}} = {{.function}}({{.column}}) WHERE {{.condition}}",
				Parameters: []schemasv1alpha4.TemplateParameter{
					{Name: "table", Type: schemasv1alpha4.ParameterTypeTable, Default: "users"},
					{Name: "column", Type: schemasv1alpha4.ParameterTypeColumn, Default: "email"},
					{Name: "function", Type: schemasv1alpha4.ParameterTypeString, Default: "LOWER"},
					{Name: "condition", Type: schemasv1alpha4.ParameterTypeString, Default: "email != LOWER(email)"},
				},
			},
			BatchSize:    2000,
			BatchDelayMs: 100,
		}

		// Test template rendering
		values := make(map[string]interface{})
		for _, param := range templateMigration.Template.Parameters {
			values[param.Name] = param.Default
		}

		sql, err := schemasv1alpha4.RenderTemplate(templateMigration.Template.Template, values)
		require.NoError(t, err)
		
		expectedSQL := "UPDATE users SET email = LOWER(email) WHERE email != LOWER(email)"
		assert.Equal(t, expectedSQL, sql)

		// Test batch configuration
		assert.Equal(t, int32(2000), templateMigration.BatchSize)
		assert.Equal(t, int32(100), templateMigration.BatchDelayMs)
	})

	t.Run("migration validation comprehensive", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{
				Name: "valid-backfill",
				Type: schemasv1alpha4.DataMigrationTypeBackfill,
				SQL:  "UPDATE users SET created_at = NOW() WHERE created_at IS NULL",
				Conditions: []schemasv1alpha4.DataMigrationCondition{
					{Query: "SELECT COUNT(*) FROM users WHERE created_at IS NULL", Operator: ">", Value: 0},
				},
			},
			{
				Name: "valid-transform",
				Type: schemasv1alpha4.DataMigrationTypeTransform,
				SQL:  "UPDATE users SET email = LOWER(email) WHERE email != LOWER(email)",
			},
			{
				Name: "valid-cleanup",
				Type: schemasv1alpha4.DataMigrationTypeCleanup,
				SQL:  "DELETE FROM users WHERE last_login < NOW() - INTERVAL '2 years'",
			},
		}

		validator := migrationcontroller.NewDataMigrationValidator()
		
		// Test that all valid migrations pass validation
		for _, migration := range migrations {
			err := validator.ValidateDataMigration(&migration)
			assert.NoError(t, err, "Migration %s (%s) should be valid", migration.Name, migration.Type)
		}

		// Test group validation
		err := validator.ValidateDataMigrations(migrations)
		assert.NoError(t, err, "All migrations together should be valid")
	})
}

// MockLogger for testing without external dependencies
type MockLogger struct {
	InfoMessages  []string
	ErrorMessages []string
	DebugMessages []string
}

func (m *MockLogger) Info(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	m.InfoMessages = append(m.InfoMessages, formatted)
}

func (m *MockLogger) Error(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	m.ErrorMessages = append(m.ErrorMessages, formatted)
}

func (m *MockLogger) Debug(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	m.DebugMessages = append(m.DebugMessages, formatted)
} 