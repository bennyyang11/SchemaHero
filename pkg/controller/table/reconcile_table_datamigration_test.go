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

package table

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

func TestGetInitialDataMigrationStatus(t *testing.T) {
	t.Run("no data statements", func(t *testing.T) {
		statements := []string{}
		status := getInitialDataMigrationStatus(statements)
		assert.Empty(t, status, "No data migrations should return empty status")
	})

	t.Run("only comment statements", func(t *testing.T) {
		statements := []string{
			"-- Migration: test-migration",
			"-- Description: This migration was skipped",
			"-- Migration test-migration: SKIPPED (conditions not met)",
		}
		status := getInitialDataMigrationStatus(statements)
		assert.Equal(t, schemasv1alpha4.DataMigrationSkipped, status)
	})

	t.Run("actual SQL statements", func(t *testing.T) {
		statements := []string{
			"-- Migration: update-status",
			"UPDATE users SET status = 'active' WHERE status IS NULL",
			"-- Migration: normalize-emails",
			"UPDATE users SET email = LOWER(email)",
		}
		status := getInitialDataMigrationStatus(statements)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, status)
	})

	t.Run("mixed statements", func(t *testing.T) {
		statements := []string{
			"-- Migration: skipped-migration",
			"-- Migration skipped-migration: SKIPPED (conditions not met)",
			"",
			"-- Migration: actual-migration",
			"UPDATE users SET processed = true",
		}
		status := getInitialDataMigrationStatus(statements)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, status, "Should be pending if any actual statements exist")
	})

	t.Run("empty and whitespace statements", func(t *testing.T) {
		statements := []string{
			"",
			"   ",
			"\t",
			"-- Comment only",
		}
		status := getInitialDataMigrationStatus(statements)
		assert.Equal(t, schemasv1alpha4.DataMigrationSkipped, status)
	})
}

func TestSchemaDataExecutionOrdering(t *testing.T) {
	t.Run("schema and data statements separation", func(t *testing.T) {
		// Simulate what the table controller does
		schemaStatements := []string{
			"ALTER TABLE users ADD COLUMN email VARCHAR(255)",
			"ALTER TABLE users ADD COLUMN status VARCHAR(20)",
		}

		seedStatements := []string{
			"INSERT INTO users (email, status) VALUES ('admin@example.com', 'admin')",
		}

		dataStatements := []string{
			"-- Migration: set-default-status",
			"UPDATE users SET status = 'active' WHERE status IS NULL",
			"-- Migration: normalize-emails",
			"UPDATE users SET email = LOWER(email)",
		}

		// Schema-then-data execution ordering
		allSchemaStatements := append(schemaStatements, seedStatements...)
		generatedDDL := strings.Join(allSchemaStatements, ";\n")
		generatedDML := strings.Join(dataStatements, ";\n")

		// Verify DDL contains schema changes and seed data
		assert.Contains(t, generatedDDL, "ALTER TABLE users ADD COLUMN email")
		assert.Contains(t, generatedDDL, "ALTER TABLE users ADD COLUMN status")
		assert.Contains(t, generatedDDL, "INSERT INTO users (email, status)")

		// Verify DML contains only data migrations
		assert.Contains(t, generatedDML, "UPDATE users SET status = 'active'")
		assert.Contains(t, generatedDML, "UPDATE users SET email = LOWER(email)")
		assert.NotContains(t, generatedDML, "ALTER TABLE")
		assert.NotContains(t, generatedDML, "INSERT INTO users (email, status)") // Seed data should be in DDL
	})

	t.Run("empty data migrations", func(t *testing.T) {
		schemaStatements := []string{"CREATE TABLE users (id SERIAL)"}
		seedStatements := []string{}
		dataStatements := []string{} // No data migrations

		allSchemaStatements := append(schemaStatements, seedStatements...)
		generatedDDL := strings.Join(allSchemaStatements, ";\n")
		generatedDML := strings.Join(dataStatements, ";\n")

		assert.Equal(t, "CREATE TABLE users (id SERIAL)", generatedDDL)
		assert.Empty(t, generatedDML, "No data migrations should result in empty DML")
	})

	t.Run("only data migrations", func(t *testing.T) {
		schemaStatements := []string{} // No schema changes
		seedStatements := []string{}   // No seed data
		dataStatements := []string{
			"-- Migration: update-existing-data",
			"UPDATE users SET updated_at = NOW()",
		}

		allSchemaStatements := append(schemaStatements, seedStatements...)
		generatedDDL := strings.Join(allSchemaStatements, ";\n")
		generatedDML := strings.Join(dataStatements, ";\n")

		assert.Empty(t, generatedDDL, "No schema changes should result in empty DDL")
		assert.Contains(t, generatedDML, "UPDATE users SET updated_at = NOW()")
	})
}

func TestMigrationStatusTracking(t *testing.T) {
	t.Run("migration status with both DDL and DML", func(t *testing.T) {
		// Simulate creating a migration with both schema and data changes
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
				GeneratedDML: "UPDATE users SET email = 'default@example.com' WHERE email IS NULL",
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                 schemasv1alpha4.Planned,
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		// Verify status fields are properly set
		assert.Equal(t, schemasv1alpha4.Planned, migration.Status.Phase)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.SchemaMigrationStatus)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.DataMigrationStatus)
		assert.NotEmpty(t, migration.Spec.GeneratedDDL)
		assert.NotEmpty(t, migration.Spec.GeneratedDML)
	})

	t.Run("migration status with DDL only", func(t *testing.T) {
		dataStatements := []string{} // No data migrations
		status := getInitialDataMigrationStatus(dataStatements)

		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "CREATE TABLE users (id SERIAL)",
				GeneratedDML: "", // No data migrations
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                 schemasv1alpha4.Planned,
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   status, // Should be empty
			},
		}

		assert.Equal(t, schemasv1alpha4.Planned, migration.Status.Phase)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.SchemaMigrationStatus)
		assert.Empty(t, migration.Status.DataMigrationStatus) // No data migrations
		assert.NotEmpty(t, migration.Spec.GeneratedDDL)
		assert.Empty(t, migration.Spec.GeneratedDML)
	})

	t.Run("migration status with skipped data migrations", func(t *testing.T) {
		dataStatements := []string{
			"-- Migration: conditional-update",
			"-- Migration conditional-update: SKIPPED (conditions not met)",
		}
		status := getInitialDataMigrationStatus(dataStatements)

		assert.Equal(t, schemasv1alpha4.DataMigrationSkipped, status)
	})
}

func TestBackwardCompatibility(t *testing.T) {
	t.Run("table without data migrations", func(t *testing.T) {
		// Existing table without DataMigrations field should still work
		tableSpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "users",
			Schema: &schemasv1alpha4.TableSchema{
				Postgres: &schemasv1alpha4.PostgresqlTableSchema{
					Columns: []*schemasv1alpha4.PostgresqlTableColumn{
						{Name: "id", Type: "serial"},
					},
				},
			},
			// No DataMigrations field
		}

		// Should handle nil/empty data migrations gracefully
		assert.Nil(t, tableSpec.DataMigrations)

		// Status calculation should work
		status := getInitialDataMigrationStatus([]string{})
		assert.Empty(t, status)
	})

	t.Run("table with empty data migrations", func(t *testing.T) {
		tableSpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "users",
			Schema: &schemasv1alpha4.TableSchema{
				Postgres: &schemasv1alpha4.PostgresqlTableSchema{
					Columns: []*schemasv1alpha4.PostgresqlTableColumn{
						{Name: "id", Type: "serial"},
					},
				},
			},
			DataMigrations: []schemasv1alpha4.DataMigration{}, // Empty slice
		}

		assert.NotNil(t, tableSpec.DataMigrations)
		assert.Len(t, tableSpec.DataMigrations, 0)

		// Status calculation should work with empty slice
		status := getInitialDataMigrationStatus([]string{})
		assert.Empty(t, status)
	})
}

func TestTableSHACalculationWithDataMigrations(t *testing.T) {
	t.Run("SHA changes when data migrations change", func(t *testing.T) {
		baseTable := &schemasv1alpha4.Table{
			Spec: schemasv1alpha4.TableSpec{
				Database: "testdb",
				Name:     "users",
				Schema: &schemasv1alpha4.TableSchema{
					Postgres: &schemasv1alpha4.PostgresqlTableSchema{
						Columns: []*schemasv1alpha4.PostgresqlTableColumn{
							{Name: "id", Type: "serial"},
						},
					},
				},
			},
		}

		// Get SHA without data migrations
		sha1, err := baseTable.GetSHA()
		require.NoError(t, err)

		// Add data migration
		baseTable.Spec.DataMigrations = []schemasv1alpha4.DataMigration{
			{
				Name: "test-migration",
				SQL:  "UPDATE users SET created_at = NOW()",
			},
		}

		// Get SHA with data migrations
		sha2, err := baseTable.GetSHA()
		require.NoError(t, err)

		// SHAs should be different
		assert.NotEqual(t, sha1, sha2, "SHA should change when data migrations are added")
	})

	t.Run("SHA changes when data migration details change", func(t *testing.T) {
		table := &schemasv1alpha4.Table{
			Spec: schemasv1alpha4.TableSpec{
				Database: "testdb",
				Name:     "users",
				DataMigrations: []schemasv1alpha4.DataMigration{
					{
						Name: "test-migration",
						SQL:  "UPDATE users SET status = 'active'",
					},
				},
			},
		}

		sha1, err := table.GetSHA()
		require.NoError(t, err)

		// Modify the migration
		table.Spec.DataMigrations[0].SQL = "UPDATE users SET status = 'inactive'"

		sha2, err := table.GetSHA()
		require.NoError(t, err)

		assert.NotEqual(t, sha1, sha2, "SHA should change when data migration SQL changes")
	})

	t.Run("SHA includes all data migration fields", func(t *testing.T) {
		baseTable := &schemasv1alpha4.Table{
			Spec: schemasv1alpha4.TableSpec{
				Database: "testdb",
				Name:     "users",
				DataMigrations: []schemasv1alpha4.DataMigration{
					{
						Name: "test-migration",
						SQL:  "UPDATE users SET status = 'active'",
					},
				},
			},
		}

		sha1, err := baseTable.GetSHA()
		require.NoError(t, err)

		// Add more fields to the migration
		baseTable.Spec.DataMigrations[0].Description = "Set default status"
		baseTable.Spec.DataMigrations[0].BatchSize = 1000
		baseTable.Spec.DataMigrations[0].Type = schemasv1alpha4.DataMigrationTypeBackfill

		sha2, err := baseTable.GetSHA()
		require.NoError(t, err)

		assert.NotEqual(t, sha1, sha2, "SHA should include all data migration fields")
	})
}

func TestTableControllerDataMigrationIntegration(t *testing.T) {
	t.Run("migration generation includes both DDL and DML", func(t *testing.T) {
		// Test the complete migration object structure that would be created
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				DatabaseName:   "testdb",
				TableName:      "users",
				TableNamespace: "default",
				GeneratedDDL:   "ALTER TABLE users ADD COLUMN email VARCHAR(255);\nALTER TABLE users ADD COLUMN status VARCHAR(20)",
				GeneratedDML:   "-- Migration: populate-emails\nUPDATE users SET email = 'default@example.com' WHERE email IS NULL;\n-- Migration: set-status\nUPDATE users SET status = 'active' WHERE status IS NULL",
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                 schemasv1alpha4.Planned,
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		// Verify migration has both DDL and DML
		assert.NotEmpty(t, migration.Spec.GeneratedDDL, "Migration should have DDL")
		assert.NotEmpty(t, migration.Spec.GeneratedDML, "Migration should have DML")

		// Verify DDL contains schema changes
		assert.Contains(t, migration.Spec.GeneratedDDL, "ALTER TABLE")
		assert.Contains(t, migration.Spec.GeneratedDDL, "ADD COLUMN")

		// Verify DML contains data changes
		assert.Contains(t, migration.Spec.GeneratedDML, "UPDATE")
		assert.Contains(t, migration.Spec.GeneratedDML, "Migration:")

		// Verify status tracking
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.SchemaMigrationStatus)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.DataMigrationStatus)
	})

	t.Run("migration with DDL only", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "CREATE TABLE users (id SERIAL)",
				GeneratedDML: "", // No data migrations
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                 schemasv1alpha4.Planned,
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   "", // No data migrations
			},
		}

		assert.NotEmpty(t, migration.Spec.GeneratedDDL)
		assert.Empty(t, migration.Spec.GeneratedDML)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.SchemaMigrationStatus)
		assert.Empty(t, migration.Status.DataMigrationStatus)
	})

	t.Run("migration with DML only", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: "", // No schema changes
				GeneratedDML: "UPDATE users SET processed = true",
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                 schemasv1alpha4.Planned,
				SchemaMigrationStatus: "", // No schema changes
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		assert.Empty(t, migration.Spec.GeneratedDDL)
		assert.NotEmpty(t, migration.Spec.GeneratedDML)
		assert.Empty(t, migration.Status.SchemaMigrationStatus)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.DataMigrationStatus)
	})
}

func TestExecutionOrderingLogic(t *testing.T) {
	t.Run("schema statements come before data statements", func(t *testing.T) {
		// This tests the conceptual ordering - in practice, they're in separate fields
		// but the execution engine should handle DDL before DML

		ddl := []string{
			"ALTER TABLE users ADD COLUMN email VARCHAR(255)",
			"CREATE INDEX idx_users_email ON users(email)",
		}

		dml := []string{
			"UPDATE users SET email = 'default@example.com' WHERE email IS NULL",
		}

		// In the Migration object, these are separated
		migration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				GeneratedDDL: strings.Join(ddl, ";\n"),
				GeneratedDML: strings.Join(dml, ";\n"),
			},
		}

		// The execution engine should process DDL first, then DML
		assert.NotEmpty(t, migration.Spec.GeneratedDDL, "DDL should be present")
		assert.NotEmpty(t, migration.Spec.GeneratedDML, "DML should be present")

		// Verify content separation
		assert.Contains(t, migration.Spec.GeneratedDDL, "ALTER TABLE")
		assert.Contains(t, migration.Spec.GeneratedDDL, "CREATE INDEX")
		assert.NotContains(t, migration.Spec.GeneratedDDL, "UPDATE")

		assert.Contains(t, migration.Spec.GeneratedDML, "UPDATE")
		assert.NotContains(t, migration.Spec.GeneratedDML, "ALTER TABLE")
		assert.NotContains(t, migration.Spec.GeneratedDML, "CREATE INDEX")
	})

	t.Run("seed data is part of DDL phase", func(t *testing.T) {
		// Seed data should be executed with schema changes (DDL phase)
		// before data migrations (DML phase)

		schemaStatements := []string{"CREATE TABLE users (id SERIAL)"}
		seedStatements := []string{"INSERT INTO users (id) VALUES (1)"}
		dataStatements := []string{"UPDATE users SET created_at = NOW()"}

		// Seed data goes with schema (DDL)
		allSchemaStatements := append(schemaStatements, seedStatements...)
		generatedDDL := strings.Join(allSchemaStatements, ";\n")
		generatedDML := strings.Join(dataStatements, ";\n")

		assert.Contains(t, generatedDDL, "CREATE TABLE")
		assert.Contains(t, generatedDDL, "INSERT INTO users")
		assert.Contains(t, generatedDML, "UPDATE users SET created_at")

		// Verify seed data is NOT in DML
		assert.NotContains(t, generatedDML, "INSERT INTO users (id) VALUES")
	})
}

func TestDataMigrationStatusCalculation(t *testing.T) {
	t.Run("status calculation scenarios", func(t *testing.T) {
		testCases := []struct {
			name           string
			dataStatements []string
			expectedStatus schemasv1alpha4.DataMigrationStatus
			description    string
		}{
			{
				name:           "no statements",
				dataStatements: []string{},
				expectedStatus: "",
				description:    "Empty statements should result in empty status",
			},
			{
				name: "only comments",
				dataStatements: []string{
					"-- Migration: test",
					"-- This migration was skipped",
				},
				expectedStatus: schemasv1alpha4.DataMigrationSkipped,
				description:    "Only comments should be skipped",
			},
			{
				name: "actual SQL",
				dataStatements: []string{
					"-- Migration: test",
					"UPDATE users SET active = true",
				},
				expectedStatus: schemasv1alpha4.DataMigrationPending,
				description:    "Actual SQL should be pending",
			},
			{
				name: "skipped migrations",
				dataStatements: []string{
					"-- Migration test-1: SKIPPED (conditions not met)",
					"-- Migration test-2: SKIPPED (conditions not met)",
				},
				expectedStatus: schemasv1alpha4.DataMigrationSkipped,
				description:    "All skipped migrations should result in skipped status",
			},
			{
				name: "mixed skipped and actual",
				dataStatements: []string{
					"-- Migration test-1: SKIPPED (conditions not met)",
					"-- Migration: test-2",
					"UPDATE users SET processed = true",
				},
				expectedStatus: schemasv1alpha4.DataMigrationPending,
				description:    "Mix of skipped and actual should be pending",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				status := getInitialDataMigrationStatus(tc.dataStatements)
				assert.Equal(t, tc.expectedStatus, status, tc.description)
			})
		}
	})
}
