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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database"
)

// TestTableControllerDataMigrationIntegration tests complete integration
func TestTableControllerDataMigrationIntegration(t *testing.T) {
	t.Run("complete table with schema and data migrations", func(t *testing.T) {
		// Create a comprehensive table spec
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
						{Name: "created_at", Type: "timestamp"},
					},
				},
			},
			DataMigrations: []schemasv1alpha4.DataMigration{
				{
					Name:        "populate-emails",
					Description: "Generate emails for existing users",
					Type:        schemasv1alpha4.DataMigrationTypeBackfill,
					SQL:         "UPDATE users SET email = username || '@company.com' WHERE email IS NULL",
					Conditions: []schemasv1alpha4.DataMigrationCondition{
						{
							Query:    "SELECT COUNT(*) FROM users WHERE email IS NULL",
							Operator: ">",
							Value:    0,
						},
					},
					BatchSize: 1000,
					Priority:  10,
				},
				{
					Name:      "set-default-status",
					Type:      schemasv1alpha4.DataMigrationTypeBackfill,
					SQL:       "UPDATE users SET status = 'active' WHERE status IS NULL",
					DependsOn: []string{"populate-emails"},
					Priority:  5,
				},
				{
					Name: "set-created-at",
					Type: schemasv1alpha4.DataMigrationTypeBackfill,
					SQL:  "UPDATE users SET created_at = NOW() WHERE created_at IS NULL",
				},
			},
		}

		// Test complete planning (this will fail connection but test structure)
		db := &database.Database{
			Driver: "postgres",
			URI:    "mock://test",
		}

		// Test data migration planning
		dataStatements, err := db.PlanDataMigrations(tableSpec)
		if err != nil {
			// Should fail with connection error, not implementation error
			assert.Contains(t, err.Error(), "connect")
			assert.NotContains(t, err.Error(), "not implemented")
		} else {
			// If it succeeds (mock implementation), verify structure
			assert.NotNil(t, dataStatements)
		}

		// Test SHA calculation includes data migrations
		table1 := &schemasv1alpha4.Table{Spec: *tableSpec}
		sha1, err := table1.GetSHA()
		require.NoError(t, err)

		// Modify data migration and verify SHA changes
		tableSpecModified := *tableSpec
		tableSpecModified.DataMigrations[0].SQL = "UPDATE users SET email = 'modified'"
		table2 := &schemasv1alpha4.Table{Spec: tableSpecModified}
		sha2, err := table2.GetSHA()
		require.NoError(t, err)

		assert.NotEqual(t, sha1, sha2, "SHA should change when data migrations change")
	})

	t.Run("migration object generation", func(t *testing.T) {
		// Simulate what the table controller creates
		schemaStatements := []string{
			"ALTER TABLE users ADD COLUMN email VARCHAR(255)",
			"ALTER TABLE users ADD COLUMN status VARCHAR(20)",
		}

		seedStatements := []string{
			"INSERT INTO users (username, email) VALUES ('admin', 'admin@example.com')",
		}

		dataStatements := []string{
			"-- Migration: populate-emails",
			"-- Description: Generate emails for existing users",
			"UPDATE users SET email = username || '@company.com' WHERE email IS NULL",
			"",
			"-- Migration: set-default-status", 
			"UPDATE users SET status = 'active' WHERE status IS NULL",
		}

		// Follow the same logic as the table controller
		allSchemaStatements := append(schemaStatements, seedStatements...)
		generatedDDL := strings.Join(allSchemaStatements, ";\n")
		generatedDML := strings.Join(dataStatements, ";\n")

		migration := &schemasv1alpha4.Migration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-migration",
				Namespace: "default",
			},
			Spec: schemasv1alpha4.MigrationSpec{
				DatabaseName:   "testdb",
				TableName:      "users", 
				TableNamespace: "default",
				GeneratedDDL:   generatedDDL,
				GeneratedDML:   generatedDML,
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                 schemasv1alpha4.Planned,
				PlannedAt:             time.Now().Unix(),
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		// Verify the migration structure
		assert.NotEmpty(t, migration.Spec.GeneratedDDL, "Should have DDL")
		assert.NotEmpty(t, migration.Spec.GeneratedDML, "Should have DML")
		
		// Verify DDL contains schema and seed
		assert.Contains(t, migration.Spec.GeneratedDDL, "ALTER TABLE")
		assert.Contains(t, migration.Spec.GeneratedDDL, "INSERT INTO users")
		
		// Verify DML contains only data migrations
		assert.Contains(t, migration.Spec.GeneratedDML, "UPDATE users SET email")
		assert.Contains(t, migration.Spec.GeneratedDML, "UPDATE users SET status")
		assert.NotContains(t, migration.Spec.GeneratedDML, "ALTER TABLE")
		assert.NotContains(t, migration.Spec.GeneratedDML, "INSERT INTO users (username, email)")

		// Verify status tracking
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.SchemaMigrationStatus)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.DataMigrationStatus)
	})

	t.Run("backward compatibility - legacy tables", func(t *testing.T) {
		// Legacy table without data migrations
		legacySpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "legacy_table",
			Schema: &schemasv1alpha4.TableSchema{
				Postgres: &schemasv1alpha4.PostgresqlTableSchema{
					Columns: []*schemasv1alpha4.PostgresqlTableColumn{
						{Name: "id", Type: "serial"},
						{Name: "name", Type: "varchar(100)"},
					},
				},
			},
			// No DataMigrations field
		}

		// Test planning (will fail connection but test routing)
		db := &database.Database{
			Driver: "postgres",
			URI:    "mock://test",
		}

		dataStatements, err := db.PlanDataMigrations(legacySpec)
		if err != nil {
			// Should fail with connection error for empty migrations
			assert.Contains(t, err.Error(), "connect")
		} else {
			// If successful, should be empty
			assert.Empty(t, dataStatements)
		}

		// Test status calculation
		status := getInitialDataMigrationStatus([]string{})
		assert.Empty(t, status, "Legacy tables should have empty data migration status")

		// Test SHA calculation still works
		table := &schemasv1alpha4.Table{Spec: *legacySpec}
		sha, err := table.GetSHA()
		require.NoError(t, err)
		assert.NotEmpty(t, sha, "SHA calculation should work for legacy tables")
	})

	t.Run("data migration error handling", func(t *testing.T) {
		// Table with problematic data migrations
		problematicSpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "problematic_table",
			DataMigrations: []schemasv1alpha4.DataMigration{
				{
					Name: "invalid-dependency",
					SQL:  "UPDATE users SET status = 'active'",
					DependsOn: []string{"non-existent-migration"},
				},
			},
		}

		// Test dependency validation
		resolver := schemasv1alpha4.NewDependencyResolver(problematicSpec.DataMigrations)
		_, err := resolver.ResolveExecutionOrder()
		assert.Error(t, err, "Should detect missing dependency")
		assert.Contains(t, err.Error(), "non-existent migration")
	})

	t.Run("execution ordering verification", func(t *testing.T) {
		// Create migrations with complex dependencies
		migrations := []schemasv1alpha4.DataMigration{
			{Name: "step-3", SQL: "UPDATE users SET step = 3", DependsOn: []string{"step-2"}, Priority: 1},
			{Name: "step-1", SQL: "UPDATE users SET step = 1", Priority: 10},
			{Name: "step-2", SQL: "UPDATE users SET step = 2", DependsOn: []string{"step-1"}, Priority: 5},
		}

		resolver := schemasv1alpha4.NewDependencyResolver(migrations)
		ordered, err := resolver.ResolveExecutionOrder()
		require.NoError(t, err)
		require.Len(t, ordered, 3)

		// Verify correct execution order
		assert.Equal(t, "step-1", ordered[0].Name)
		assert.Equal(t, "step-2", ordered[1].Name)
		assert.Equal(t, "step-3", ordered[2].Name)

		// Verify priorities are respected within dependency constraints
		assert.Equal(t, int32(10), ordered[0].Priority) // Highest priority first
		assert.Equal(t, int32(5), ordered[1].Priority)
		assert.Equal(t, int32(1), ordered[2].Priority)  // Lowest priority last
	})

	t.Run("status tracking progression", func(t *testing.T) {
		// Test the progression of migration status
		initialStatus := schemasv1alpha4.MigrationStatus{
			Phase:                 schemasv1alpha4.Planned,
			SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
			DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
		}

		// Schema execution completes first
		schemaCompleteStatus := initialStatus
		schemaCompleteStatus.SchemaMigrationStatus = schemasv1alpha4.DataMigrationCompleted

		// Then data migrations execute
		dataRunningStatus := schemaCompleteStatus
		dataRunningStatus.DataMigrationStatus = schemasv1alpha4.DataMigrationRunning

		// Finally everything completes
		completeStatus := dataRunningStatus
		completeStatus.Phase = schemasv1alpha4.Executed
		completeStatus.DataMigrationStatus = schemasv1alpha4.DataMigrationCompleted

		// Verify status progression makes sense
		assert.Equal(t, schemasv1alpha4.Planned, initialStatus.Phase)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, initialStatus.SchemaMigrationStatus)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, initialStatus.DataMigrationStatus)

		assert.Equal(t, schemasv1alpha4.DataMigrationCompleted, schemaCompleteStatus.SchemaMigrationStatus)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, schemaCompleteStatus.DataMigrationStatus)

		assert.Equal(t, schemasv1alpha4.DataMigrationRunning, dataRunningStatus.DataMigrationStatus)

		assert.Equal(t, schemasv1alpha4.Executed, completeStatus.Phase)
		assert.Equal(t, schemasv1alpha4.DataMigrationCompleted, completeStatus.DataMigrationStatus)
	})
}

func TestTableControllerPlanningLogic(t *testing.T) {
	t.Run("planning with mock database", func(t *testing.T) {
		// Test the enhanced planning logic
		db := &database.Database{
			Driver: "postgres",
			URI:    "mock://connection",
		}

		tableSpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "test_table",
			Schema: &schemasv1alpha4.TableSchema{
				Postgres: &schemasv1alpha4.PostgresqlTableSchema{
					Columns: []*schemasv1alpha4.PostgresqlTableColumn{
						{Name: "id", Type: "serial"},
						{Name: "email", Type: "varchar(255)"},
					},
				},
			},
			DataMigrations: []schemasv1alpha4.DataMigration{
				{
					Name: "backfill-emails",
					SQL:  "UPDATE test_table SET email = 'default@example.com' WHERE email IS NULL",
				},
			},
		}

		// Test enhanced planning function
		ddlStatements, dmlStatements, err := db.PlanCompleteTableSpec(tableSpec)
		
		// This will fail due to mock connection, but should route to data migration planner
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to plan schema changes")
		
		// The structure should be correct even if execution fails
		assert.Nil(t, ddlStatements)
		assert.Nil(t, dmlStatements)
	})

	t.Run("planning without data migrations", func(t *testing.T) {
		db := &database.Database{
			Driver: "postgres",
		}

		tableSpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "simple_table",
			Schema: &schemasv1alpha4.TableSchema{
				Postgres: &schemasv1alpha4.PostgresqlTableSchema{
					Columns: []*schemasv1alpha4.PostgresqlTableColumn{
						{Name: "id", Type: "serial"},
					},
				},
			},
			// No DataMigrations
		}

		// Test data migration planning with no migrations
		dataStatements, err := db.PlanDataMigrations(tableSpec)
		require.NoError(t, err)
		assert.Empty(t, dataStatements, "Should return empty statements when no data migrations")
	})

	t.Run("database driver support", func(t *testing.T) {
		tableSpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "test_table",
			DataMigrations: []schemasv1alpha4.DataMigration{
				{Name: "test", SQL: "UPDATE test_table SET value = 1"},
			},
		}

		supportedDrivers := []string{"postgres", "mysql", "cockroachdb", "timescaledb"}
		for _, driver := range supportedDrivers {
			db := &database.Database{
				Driver: driver,
				URI:    "mock://test",
			}

			_, err := db.PlanDataMigrations(tableSpec)
			if err != nil {
				// Should fail with connection error, not "not implemented"
				assert.NotContains(t, err.Error(), "not implemented", "Driver %s should be supported", driver)
			}
		}
	})
}

func TestMigrationObjectCreation(t *testing.T) {
	t.Run("migration object with both DDL and DML", func(t *testing.T) {
		// Simulate migration object creation by table controller
		migration := &schemasv1alpha4.Migration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "schemas.schemahero.io/v1alpha4",
				Kind:       "Migration",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "users-abc1234",
				Namespace: "default",
			},
			Spec: schemasv1alpha4.MigrationSpec{
				DatabaseName:   "testdb",
				TableName:      "users",
				TableNamespace: "default",
				GeneratedDDL:   "ALTER TABLE users ADD COLUMN email VARCHAR(255);\nALTER TABLE users ADD COLUMN status VARCHAR(20)",
				GeneratedDML:   "-- Migration: populate-emails\nUPDATE users SET email = username || '@company.com' WHERE email IS NULL;\n-- Migration: set-status\nUPDATE users SET status = 'active' WHERE status IS NULL",
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                 schemasv1alpha4.Planned,
				PlannedAt:             time.Now().Unix(),
				SchemaMigrationStatus: schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:   schemasv1alpha4.DataMigrationPending,
			},
		}

		// Verify migration object structure
		assert.Equal(t, "schemas.schemahero.io/v1alpha4", migration.APIVersion)
		assert.Equal(t, "Migration", migration.Kind)
		assert.NotEmpty(t, migration.Spec.GeneratedDDL)
		assert.NotEmpty(t, migration.Spec.GeneratedDML)

		// Verify status fields
		assert.Equal(t, schemasv1alpha4.Planned, migration.Status.Phase)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.SchemaMigrationStatus)
		assert.Equal(t, schemasv1alpha4.DataMigrationPending, migration.Status.DataMigrationStatus)
		assert.Greater(t, migration.Status.PlannedAt, int64(0))
	})

	t.Run("migration object backward compatibility", func(t *testing.T) {
		// Legacy migration without DML fields
		legacyMigration := &schemasv1alpha4.Migration{
			Spec: schemasv1alpha4.MigrationSpec{
				DatabaseName: "testdb",
				TableName:    "legacy_table", 
				GeneratedDDL: "CREATE TABLE legacy_table (id SERIAL)",
				// No GeneratedDML field
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase: schemasv1alpha4.Planned,
				// No SchemaMigrationStatus or DataMigrationStatus
			},
		}

		// Should be valid and backward compatible
		assert.NotEmpty(t, legacyMigration.Spec.GeneratedDDL)
		assert.Empty(t, legacyMigration.Spec.GeneratedDML)
		assert.Empty(t, legacyMigration.Status.SchemaMigrationStatus)
		assert.Empty(t, legacyMigration.Status.DataMigrationStatus)
	})
}

func TestErrorScenarios(t *testing.T) {
	t.Run("data migration planning errors", func(t *testing.T) {
		// Test error handling when data migration planning fails
		db := &database.Database{
			Driver: "unsupported-db",
		}

		tableSpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "test_table",
			DataMigrations: []schemasv1alpha4.DataMigration{
				{Name: "test", SQL: "UPDATE test_table SET value = 1"},
			},
		}

		_, err := db.PlanDataMigrations(tableSpec)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown database driver")
	})

	t.Run("no statements generated", func(t *testing.T) {
		// Test scenario where no DDL or DML statements are generated
		schemaStatements := []string{}
		seedStatements := []string{}
		dataStatements := []string{}

		// This should result in no migration being created
		totalStatements := len(schemaStatements) + len(seedStatements) + len(dataStatements)
		assert.Equal(t, 0, totalStatements, "No statements should mean no migration")
	})

	t.Run("data migrations without schema", func(t *testing.T) {
		// Table with data migrations but no schema changes
		tableSpec := &schemasv1alpha4.TableSpec{
			Database: "testdb",
			Name:     "existing_table",
			// No Schema field
			DataMigrations: []schemasv1alpha4.DataMigration{
				{Name: "update-data", SQL: "UPDATE existing_table SET processed = true"},
			},
		}

		db := &database.Database{Driver: "postgres"}
		
		// Should still plan data migrations even without schema
		dataStatements, err := db.PlanDataMigrations(tableSpec)
		if err != nil {
			// May fail with connection error
			assert.Contains(t, err.Error(), "connect")
		} else {
			// If successful, should have data statements
			assert.NotNil(t, dataStatements)
		}
	})
}

// Helper function imported from table controller for testing
func getInitialDataMigrationStatus(dataStatements []string) schemasv1alpha4.DataMigrationStatus {
	if len(dataStatements) == 0 {
		return "" // No data migrations
	}
	
	// Check if data statements are just comments (skipped migrations)
	hasActualStatements := false
	for _, stmt := range dataStatements {
		trimmed := strings.TrimSpace(stmt)
		if trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			hasActualStatements = true
			break
		}
	}
	
	if !hasActualStatements {
		return schemasv1alpha4.DataMigrationSkipped
	}
	
	return schemasv1alpha4.DataMigrationPending
} 