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

package mysql

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

func TestMysqlDataMigrationPlanning(t *testing.T) {
	t.Run("simple SQL migration", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name:        "update-status",
			Description: "Set default status",
			SQL:         "UPDATE users SET status = 'active' WHERE status IS NULL",
		}

		// Mock the planner without database connection
		planner := &MysqlDataMigrationPlanner{}
		sql, err := planner.PlanSingleDataMigration("users", migration)

		require.NoError(t, err)
		assert.Contains(t, sql, "UPDATE users SET status = 'active'")
	})

	t.Run("MySQL-specific SQL adaptation", func(t *testing.T) {
		planner := &MysqlDataMigrationPlanner{}

		// Test PostgreSQL concatenation converted to MySQL
		sql, err := planner.GetDatabaseSpecificSQL("UPDATE users SET name = first_name || ' ' || last_name")
		require.NoError(t, err)
		assert.Contains(t, sql, "CONCAT(") // PostgreSQL || should become MySQL CONCAT

		// Test timestamp function conversion
		sql, err = planner.GetDatabaseSpecificSQL("UPDATE users SET created_at = CURRENT_DATE")
		require.NoError(t, err)
		assert.Contains(t, sql, "CURDATE()") // PostgreSQL CURRENT_DATE should become MySQL CURDATE()

		// Test identifier quoting
		sql, err = planner.GetDatabaseSpecificSQL(`UPDATE "users" SET "email" = 'test'`)
		require.NoError(t, err)
		assert.Contains(t, sql, "`users`") // Double quotes should become backticks
		assert.Contains(t, sql, "`email`")
	})

	t.Run("template-based migration", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name: "template-update",
			Template: &schemasv1alpha4.DataMigrationTemplate{
				Template: "UPDATE {{.table}} SET {{.column}} = {{quote .value}}",
				Parameters: []schemasv1alpha4.TemplateParameter{
					{Name: "table", Type: schemasv1alpha4.ParameterTypeTable, Default: "users"},
					{Name: "column", Type: schemasv1alpha4.ParameterTypeColumn, Default: "email"},
					{Name: "value", Type: schemasv1alpha4.ParameterTypeString, Default: "test@mysql.com"},
				},
			},
		}

		planner := &MysqlDataMigrationPlanner{}
		sql, err := planner.PlanSingleDataMigration("users", migration)

		require.NoError(t, err)
		assert.Contains(t, sql, "UPDATE users SET")
		assert.Contains(t, sql, "'test@mysql.com'")
	})

	t.Run("batching support", func(t *testing.T) {
		migration := &schemasv1alpha4.DataMigration{
			Name:      "batch-update",
			SQL:       "UPDATE users SET processed = true WHERE processed = false",
			BatchSize: 2000,
		}

		planner := &MysqlDataMigrationPlanner{}
		sql, err := planner.PlanSingleDataMigration("users", migration)

		require.NoError(t, err)
		assert.Contains(t, sql, "LIMIT 2000")
	})

	t.Run("MySQL interval syntax conversion", func(t *testing.T) {
		planner := &MysqlDataMigrationPlanner{}

		// Test PostgreSQL interval syntax converted to MySQL
		sql, err := planner.GetDatabaseSpecificSQL("DELETE FROM logs WHERE created_at < NOW() - INTERVAL '30 days'")
		require.NoError(t, err)
		assert.Contains(t, sql, "INTERVAL 30 DAY") // PostgreSQL format converted to MySQL
	})

	t.Run("dependency resolution", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{
				Name:      "final",
				SQL:       "UPDATE users SET migration_complete = true",
				DependsOn: []string{"middle"},
			},
			{
				Name: "start",
				SQL:  "UPDATE users SET migration_started = true",
			},
			{
				Name:      "middle",
				SQL:       "UPDATE users SET migration_progress = 50",
				DependsOn: []string{"start"},
			},
		}

		planner := &MysqlDataMigrationPlanner{}
		statements, err := planner.PlanDataMigrations("users", migrations)

		require.NoError(t, err)

		// Verify correct ordering in the generated statements
		sqlContent := strings.Join(statements, "\n")
		startPos := strings.Index(sqlContent, "migration_started = true")
		middlePos := strings.Index(sqlContent, "migration_progress = 50")
		finalPos := strings.Index(sqlContent, "migration_complete = true")

		assert.Greater(t, middlePos, startPos, "middle should come after start")
		assert.Greater(t, finalPos, middlePos, "final should come after middle")
	})
}

func TestMysqlTemplateProcessing(t *testing.T) {
	t.Run("MySQL-specific template values", func(t *testing.T) {
		template := &schemasv1alpha4.DataMigrationTemplate{
			Template: "UPDATE users SET created_at = {{.currentTimestamp}}, date_only = {{.currentDate}}, db_type = {{quote .databaseType}}",
		}

		planner := &MysqlDataMigrationPlanner{}
		sql, err := planner.renderTemplate(template)

		require.NoError(t, err)
		assert.Contains(t, sql, "NOW()")
		assert.Contains(t, sql, "CURDATE()")
		assert.Contains(t, sql, "'mysql'")
	})

	t.Run("parameter type handling", func(t *testing.T) {
		template := &schemasv1alpha4.DataMigrationTemplate{
			Template: "UPDATE products SET active = {{.active}}, price = {{.price}}, name = {{quote .name}}",
			Parameters: []schemasv1alpha4.TemplateParameter{
				{Name: "active", Type: schemasv1alpha4.ParameterTypeBoolean, Default: "false"},
				{Name: "price", Type: schemasv1alpha4.ParameterTypeInteger, Default: "99"},
				{Name: "name", Type: schemasv1alpha4.ParameterTypeString, Default: "Test Product"},
			},
		}

		planner := &MysqlDataMigrationPlanner{}
		sql, err := planner.renderTemplate(template)

		require.NoError(t, err)
		assert.Contains(t, sql, "active = false")
		assert.Contains(t, sql, "price = 99")
		assert.Contains(t, sql, "'Test Product'")
	})

	t.Run("complex template with MySQL syntax", func(t *testing.T) {
		template := &schemasv1alpha4.DataMigrationTemplate{
			Template: "UPDATE orders SET total = price * {{.multiplier}}, updated_at = {{.currentTimestamp}} WHERE status = {{quote .status}}",
			Parameters: []schemasv1alpha4.TemplateParameter{
				{Name: "multiplier", Type: schemasv1alpha4.ParameterTypeInteger, Default: "1"},
				{Name: "status", Type: schemasv1alpha4.ParameterTypeString, Default: "pending"},
			},
		}

		planner := &MysqlDataMigrationPlanner{}
		sql, err := planner.renderTemplate(template)

		require.NoError(t, err)
		assert.Contains(t, sql, "price * 1")
		assert.Contains(t, sql, "NOW()")
		assert.Contains(t, sql, "'pending'")
	})
}
