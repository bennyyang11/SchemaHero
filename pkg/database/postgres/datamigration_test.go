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

package postgres

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

func TestPostgresDataMigrationPlanning(t *testing.T) {
	t.Run("simple SQL migration", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{
				Name:        "update-status",
				Description: "Set default status",
				SQL:         "UPDATE users SET status = 'active' WHERE status IS NULL",
			},
		}

		// Mock the planner without database connection
		planner := &PostgresDataMigrationPlanner{}
		sql, err := planner.PlanSingleDataMigration("users", &migrations[0])

		require.NoError(t, err)
		assert.Contains(t, sql, "UPDATE users SET status = 'active'")
	})

	t.Run("template-based migration", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name: "template-update",
			Template: &schemasv1alpha4.DataMigrationTemplate{
				Template: "UPDATE users SET {{.column}} = {{quote .value}} WHERE id = {{.userId}}",
				Parameters: []schemasv1alpha4.TemplateParameter{
					{
						Name:     "column",
						Type:     schemasv1alpha4.ParameterTypeColumn,
						Default:  "email",
						Required: false,
					},
					{
						Name:     "value",
						Type:     schemasv1alpha4.ParameterTypeString,
						Default:  "test@example.com",
						Required: false,
					},
					{
						Name:     "userId",
						Type:     schemasv1alpha4.ParameterTypeInteger,
						Default:  "123",
						Required: false,
					},
				},
			},
		}

		// Mock the planner
		planner := &PostgresDataMigrationPlanner{}
		sql, err := planner.PlanSingleDataMigration("users", migration)

		require.NoError(t, err)
		assert.Contains(t, sql, "UPDATE users SET")
		assert.Contains(t, sql, "'test@example.com'")
		assert.Contains(t, sql, "123")
	})

	t.Run("PostgreSQL-specific SQL adaptation", func(t *testing.T) {
		planner := &PostgresDataMigrationPlanner{}

		// Test that PostgreSQL syntax is preserved
		sql, err := planner.GetDatabaseSpecificSQL("UPDATE users SET name = first_name || ' ' || last_name")
		require.NoError(t, err)
		assert.Contains(t, sql, "||") // PostgreSQL concatenation should be preserved

		sql, err = planner.GetDatabaseSpecificSQL("SELECT * FROM users WHERE created_at > NOW()")
		require.NoError(t, err)
		assert.Contains(t, sql, "NOW()") // PostgreSQL function should be preserved
	})

	t.Run("batching support", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name:      "batch-update",
			SQL:       "UPDATE users SET processed = true WHERE processed = false",
			BatchSize: 1000,
		}

		planner := &PostgresDataMigrationPlanner{}
		sql, err := planner.PlanSingleDataMigration("users", migration)

		require.NoError(t, err)
		assert.Contains(t, sql, "LIMIT 1000")
	})

	t.Run("dependency ordering", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{
				Name:      "third",
				SQL:       "UPDATE users SET step = 3",
				DependsOn: []string{"second"},
			},
			{
				Name: "first",
				SQL:  "UPDATE users SET step = 1",
			},
			{
				Name:      "second",
				SQL:       "UPDATE users SET step = 2",
				DependsOn: []string{"first"},
			},
		}

		planner := &PostgresDataMigrationPlanner{}
		statements, err := planner.PlanDataMigrations("users", migrations)

		require.NoError(t, err)
		require.Len(t, statements, 9) // 3 migrations * 3 lines each (comment + description + sql)

		// Check that they appear in the correct order
		sqlContent := strings.Join(statements, "\n")
		firstPos := strings.Index(sqlContent, "step = 1")
		secondPos := strings.Index(sqlContent, "step = 2")
		thirdPos := strings.Index(sqlContent, "step = 3")

		assert.Greater(t, secondPos, firstPos, "second should come after first")
		assert.Greater(t, thirdPos, secondPos, "third should come after second")
	})

	t.Run("migration types", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{
				Name: "backfill",
				Type: schemasv1alpha4.DataMigrationTypeBackfill,
				SQL:  "UPDATE users SET created_at = NOW() WHERE created_at IS NULL",
			},
			{
				Name: "transform",
				Type: schemasv1alpha4.DataMigrationTypeTransform,
				SQL:  "UPDATE users SET email = LOWER(email)",
			},
			{
				Name: "cleanup",
				Type: schemasv1alpha4.DataMigrationTypeCleanup,
				SQL:  "DELETE FROM users WHERE last_login < NOW() - INTERVAL '2 years'",
			},
		}

		planner := &PostgresDataMigrationPlanner{}

		for _, migration := range migrations {
			sql, err := planner.PlanSingleDataMigration("users", &migration)
			require.NoError(t, err)
			assert.NotEmpty(t, sql)

			// Each type should produce valid SQL
			switch migration.Type {
			case schemasv1alpha4.DataMigrationTypeBackfill:
				assert.Contains(t, sql, "UPDATE")
			case schemasv1alpha4.DataMigrationTypeTransform:
				assert.Contains(t, sql, "UPDATE")
			case schemasv1alpha4.DataMigrationTypeCleanup:
				assert.Contains(t, sql, "DELETE")
			}
		}
	})
}

func TestPostgresTemplateProcessing(t *testing.T) {
	t.Run("parameter substitution", func(t *testing.T) {
		template := &schemasv1alpha4.DataMigrationTemplate{
			Template: "UPDATE {{.table}} SET {{.column}} = {{quote .value}}",
			Parameters: []schemasv1alpha4.TemplateParameter{
				{Name: "table", Type: schemasv1alpha4.ParameterTypeTable, Default: "users"},
				{Name: "column", Type: schemasv1alpha4.ParameterTypeColumn, Default: "email"},
				{Name: "value", Type: schemasv1alpha4.ParameterTypeString, Default: "test@example.com"},
			},
		}

		planner := &PostgresDataMigrationPlanner{}
		sql, err := planner.renderTemplate(template)

		require.NoError(t, err)
		assert.Equal(t, "UPDATE users SET email = 'test@example.com'", sql)
	})

	t.Run("PostgreSQL-specific template values", func(t *testing.T) {
		template := &schemasv1alpha4.DataMigrationTemplate{
			Template: "UPDATE users SET created_at = {{.currentTimestamp}}, updated_at = {{.currentDate}}",
		}

		planner := &PostgresDataMigrationPlanner{}
		sql, err := planner.renderTemplate(template)

		require.NoError(t, err)
		assert.Contains(t, sql, "NOW()")
		assert.Contains(t, sql, "CURRENT_DATE")
	})

	t.Run("complex template with multiple parameter types", func(t *testing.T) {
		template := &schemasv1alpha4.DataMigrationTemplate{
			Template: "UPDATE users SET active = {{.isActive}}, score = {{.score}}, updated_at = {{.currentTimestamp}}",
			Parameters: []schemasv1alpha4.TemplateParameter{
				{Name: "isActive", Type: schemasv1alpha4.ParameterTypeBoolean, Default: "true"},
				{Name: "score", Type: schemasv1alpha4.ParameterTypeInteger, Default: "100"},
			},
		}

		planner := &PostgresDataMigrationPlanner{}
		sql, err := planner.renderTemplate(template)

		require.NoError(t, err)
		assert.Contains(t, sql, "active = true")
		assert.Contains(t, sql, "score = 100")
		assert.Contains(t, sql, "NOW()")
	})
}
