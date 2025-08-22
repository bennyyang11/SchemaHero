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

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

func TestDatabaseDataMigrationPlanning(t *testing.T) {
	t.Run("no data migrations", func(t *testing.T) {
		db := &Database{Driver: "postgres"}
		spec := &schemasv1alpha4.TableSpec{
			Name: "users",
			// No DataMigrations field
		}

		statements, err := db.PlanDataMigrations(spec)
		
		require.NoError(t, err)
		assert.Empty(t, statements)
	})

	t.Run("empty data migrations", func(t *testing.T) {
		db := &Database{Driver: "postgres"}
		spec := &schemasv1alpha4.TableSpec{
			Name:           "users",
			DataMigrations: []schemasv1alpha4.DataMigration{}, // Empty slice
		}

		statements, err := db.PlanDataMigrations(spec)
		
		require.NoError(t, err)
		assert.Empty(t, statements)
	})

	t.Run("all database types are now supported", func(t *testing.T) {
		spec := &schemasv1alpha4.TableSpec{
			Name: "users",
			DataMigrations: []schemasv1alpha4.DataMigration{
				{Name: "test", SQL: "UPDATE users SET active = true"},
			},
		}

		// All database drivers now have data migration support
		allDrivers := []string{"postgres", "mysql", "cockroachdb", "timescaledb", "sqlite", "rqlite", "cassandra"}
		
		for _, driver := range allDrivers {
			db := &Database{
				Driver: driver,
				URI:    "mock://connection", // Will fail connection but test routing
			}
			_, err := db.PlanDataMigrations(spec)
			
			// Should fail with connection error, not "not implemented" error
			if err != nil {
				assert.NotContains(t, err.Error(), "not yet implemented", "driver %s should be implemented", driver)
				assert.NotContains(t, err.Error(), "unknown database driver", "driver %s should be recognized", driver)
			}
		}
	})

	t.Run("supported database types", func(t *testing.T) {
		spec := &schemasv1alpha4.TableSpec{
			Name: "users",
			DataMigrations: []schemasv1alpha4.DataMigration{
				{Name: "test", SQL: "UPDATE users SET active = true"},
			},
		}

		supportedDrivers := []struct {
			driver string
			note   string
		}{
			{"postgres", "native PostgreSQL"},
			{"mysql", "native MySQL"},
			{"cockroachdb", "uses PostgreSQL syntax"},
			{"timescaledb", "uses PostgreSQL syntax"},
			{"sqlite", "native SQLite"},
			{"rqlite", "distributed SQLite"},
			{"cassandra", "limited CQL support"},
		}
		
		for _, item := range supportedDrivers {
			t.Run(item.driver, func(t *testing.T) {
				db := &Database{
					Driver: item.driver,
					URI:    "mock://connection", // This will fail connection but test routing
				}
				
				// This will fail due to mock connection, but should route to correct implementation
				_, err := db.PlanDataMigrations(spec)
				
				// Should fail with connection error, not "not implemented" error
				if err != nil {
					assert.NotContains(t, err.Error(), "not yet implemented")
					assert.NotContains(t, err.Error(), "unknown database driver")
				}
			})
		}
	})

	t.Run("unknown driver", func(t *testing.T) {
		db := &Database{Driver: "unknown"}
		spec := &schemasv1alpha4.TableSpec{
			Name: "users",
			DataMigrations: []schemasv1alpha4.DataMigration{
				{Name: "test", SQL: "UPDATE users SET active = true"},
			},
		}

		_, err := db.PlanDataMigrations(spec)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown database driver")
	})
}

func TestCompleteTableSpecPlanning(t *testing.T) {
	t.Run("table with both schema and data migrations", func(t *testing.T) {
		db := &Database{
			Driver: "postgres", 
			URI:    "mock://connection",
		}
		
		spec := &schemasv1alpha4.TableSpec{
			Name: "users",
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
					Name: "populate-emails",
					SQL:  "UPDATE users SET email = 'test@example.com' WHERE email IS NULL",
				},
			},
		}

		// This will fail due to mock connection, but should route correctly
		ddl, dml, err := db.PlanCompleteTableSpec(spec)
		
		// Should fail with connection error from DDL planning
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to plan schema changes")
		
		// But the structure should be correct
		assert.Nil(t, ddl)
		assert.Nil(t, dml)
	})

	t.Run("table with schema only", func(t *testing.T) {
		db := &Database{
			Driver: "postgres",
			URI:    "mock://connection", 
		}
		
		spec := &schemasv1alpha4.TableSpec{
			Name: "users",
			Schema: &schemasv1alpha4.TableSchema{
				Postgres: &schemasv1alpha4.PostgresqlTableSchema{
					Columns: []*schemasv1alpha4.PostgresqlTableColumn{
						{Name: "id", Type: "serial"},
					},
				},
			},
			// No DataMigrations
		}

		// This will fail due to mock connection
		_, _, err := db.PlanCompleteTableSpec(spec)
		assert.Error(t, err) // Should fail on DDL planning
	})

	t.Run("table with data migrations only", func(t *testing.T) {
		db := &Database{
			Driver: "postgres",
			URI:    "mock://connection", // This will cause connection error
		}
		
		spec := &schemasv1alpha4.TableSpec{
			Name: "users",
			// No Schema
			DataMigrations: []schemasv1alpha4.DataMigration{
				{Name: "test", SQL: "UPDATE users SET active = true"},
			},
		}

		ddl, dml, err := db.PlanCompleteTableSpec(spec)
		
		// This should fail because data migrations require database connection
		// but DDL planning succeeds (no schema means no DDL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to plan data migrations")
		assert.Nil(t, ddl)
		assert.Nil(t, dml)
	})
}

func TestDatabaseDriverRouting(t *testing.T) {
	t.Run("driver routing for data migrations", func(t *testing.T) {
		spec := &schemasv1alpha4.TableSpec{
			Name: "test_table",
			DataMigrations: []schemasv1alpha4.DataMigration{
				{Name: "test", SQL: "UPDATE test_table SET value = 1"},
			},
		}

		testCases := []struct {
			driver   string
			shouldImplement bool
			expectedError   string
		}{
			{"postgres", true, ""},
			{"mysql", true, ""},
			{"cockroachdb", true, ""}, // Uses postgres
			{"timescaledb", true, ""},  // Uses postgres
			{"sqlite", true, ""},       // Implemented in Phase 5
			{"rqlite", true, ""},       // Implemented in Phase 5
			{"cassandra", true, ""},    // Implemented in Phase 5 (limited)
			{"unknown", false, "unknown database driver"},
		}

		for _, tc := range testCases {
			t.Run(tc.driver, func(t *testing.T) {
				db := &Database{
					Driver: tc.driver,
					URI:    "mock://test",
				}

				_, err := db.PlanDataMigrations(spec)

				if tc.shouldImplement {
					// Should route to implementation (may fail with connection error)
					if err != nil {
						assert.NotContains(t, err.Error(), "not yet implemented")
						assert.NotContains(t, err.Error(), "unknown database driver")
					}
				} else {
					// Should return specific error
					require.Error(t, err)
					assert.Contains(t, err.Error(), tc.expectedError)
				}
			})
		}
	})
} 