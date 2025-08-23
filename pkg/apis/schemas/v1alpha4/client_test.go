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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestGeneratedClientCode verifies that generated client code works correctly
func TestGeneratedClientCode(t *testing.T) {
	t.Run("deepcopy for DataMigration", func(t *testing.T) {
		// Create a complex DataMigration with all fields
		original := &DataMigration{
			Name:        "test-migration",
			Description: "Test migration",
			SQL:         "UPDATE test SET value = 1",
			Template: &DataMigrationTemplate{
				Template: "UPDATE {{.table}} SET {{.column}} = {{.value}}",
				Parameters: []TemplateParameter{
					{
						Name:        "table",
						Type:        ParameterTypeTable,
						Default:     "users",
						Required:    true,
						Description: "Target table",
					},
				},
			},
			Conditions: []DataMigrationCondition{
				{
					Query:    "SELECT COUNT(*) FROM test",
					Operator: ">",
					Value:    0,
				},
			},
			DependsOn:    []string{"previous-migration"},
			BatchSize:    1000,
			BatchDelayMs: 100,
			Timeout:      &metav1.Duration{Duration: 30 * time.Minute},
			Type:         DataMigrationTypeTransform,
			Reversible:   true,
			ReverseSQL:   "UPDATE test SET value = 0",
			Priority:     10,
			Tags:         []string{"test", "migration"},
		}

		// Test DeepCopy
		copied := original.DeepCopy()

		// Verify it's a different object
		assert.NotSame(t, original, copied)
		assert.NotSame(t, original.Template, copied.Template)
		if len(original.Conditions) > 0 && len(copied.Conditions) > 0 {
			assert.NotSame(t, &original.Conditions[0], &copied.Conditions[0])
		}
		if len(original.DependsOn) > 0 && len(copied.DependsOn) > 0 {
			assert.NotSame(t, &original.DependsOn[0], &copied.DependsOn[0])
		}
		if len(original.Tags) > 0 && len(copied.Tags) > 0 {
			assert.NotSame(t, &original.Tags[0], &copied.Tags[0])
		}

		// Verify content is the same
		assert.Equal(t, original.Name, copied.Name)
		assert.Equal(t, original.Description, copied.Description)
		assert.Equal(t, original.SQL, copied.SQL)
		assert.Equal(t, original.Type, copied.Type)
		assert.Equal(t, original.Priority, copied.Priority)

		// Modify the copy and verify original is unchanged
		copied.Name = "modified"
		copied.Template.Template = "modified template"
		copied.Conditions[0].Value = 999
		copied.DependsOn[0] = "modified-dep"
		copied.Tags[0] = "modified-tag"

		assert.Equal(t, "test-migration", original.Name)
		assert.Equal(t, "UPDATE {{.table}} SET {{.column}} = {{.value}}", original.Template.Template)
		assert.Equal(t, int64(0), original.Conditions[0].Value)
		assert.Equal(t, "previous-migration", original.DependsOn[0])
		assert.Equal(t, "test", original.Tags[0])
	})

	t.Run("deepcopy for TableSpec with DataMigrations", func(t *testing.T) {
		// Create a TableSpec with DataMigrations
		original := &TableSpec{
			Database: "testdb",
			Name:     "testtable",
			Requires: []string{"dep1", "dep2"},
			DataMigrations: []DataMigration{
				{
					Name: "migration1",
					SQL:  "UPDATE test1",
					Conditions: []DataMigrationCondition{
						{Query: "SELECT 1", Operator: "EXISTS"},
					},
				},
				{
					Name: "migration2",
					SQL:  "UPDATE test2",
					Tags: []string{"tag1", "tag2"},
				},
			},
		}

		// Test DeepCopy
		copied := original.DeepCopy()

		// Verify it's a different object
		assert.NotSame(t, original, copied)
		if len(original.Requires) > 0 && len(copied.Requires) > 0 {
			assert.NotSame(t, &original.Requires[0], &copied.Requires[0])
		}
		if len(original.DataMigrations) > 0 && len(copied.DataMigrations) > 0 {
			assert.NotSame(t, &original.DataMigrations[0], &copied.DataMigrations[0])
		}

		// Verify content is the same
		assert.Equal(t, original.Database, copied.Database)
		assert.Equal(t, original.Name, copied.Name)
		assert.Len(t, copied.DataMigrations, 2)
		assert.Equal(t, "migration1", copied.DataMigrations[0].Name)
		assert.Equal(t, "migration2", copied.DataMigrations[1].Name)

		// Modify the copy and verify original is unchanged
		copied.DataMigrations[0].Name = "modified"
		copied.DataMigrations[0].Conditions[0].Query = "SELECT 2"
		copied.DataMigrations[1].Tags[0] = "modified-tag"

		assert.Equal(t, "migration1", original.DataMigrations[0].Name)
		assert.Equal(t, "SELECT 1", original.DataMigrations[0].Conditions[0].Query)
		assert.Equal(t, "tag1", original.DataMigrations[1].Tags[0])
	})

	t.Run("deepcopy for MigrationSpec with DML fields", func(t *testing.T) {
		// Create a MigrationSpec with new DML fields
		original := &MigrationSpec{
			DatabaseName:   "testdb",
			TableName:      "testtable",
			TableNamespace: "default",
			GeneratedDDL:   "ALTER TABLE test ADD COLUMN new_col",
			EditedDDL:      "-- edited DDL",
			GeneratedDML:   "UPDATE test SET new_col = 'value'",
			EditedDML:      "-- edited DML",
		}

		// Test DeepCopy
		copied := original.DeepCopy()

		// Verify it's a different object
		assert.NotSame(t, original, copied)

		// Verify content is the same
		assert.Equal(t, original.GeneratedDML, copied.GeneratedDML)
		assert.Equal(t, original.EditedDML, copied.EditedDML)

		// Modify the copy and verify original is unchanged
		copied.GeneratedDML = "modified DML"
		assert.Equal(t, "UPDATE test SET new_col = 'value'", original.GeneratedDML)
	})

	t.Run("deepcopy for MigrationStatus with data migration fields", func(t *testing.T) {
		// Create a MigrationStatus with new fields
		original := &MigrationStatus{
			Phase:                 "PLANNED",
			PlannedAt:             123456789,
			SchemaMigrationStatus: DataMigrationPending,
			DataMigrationStatus:   DataMigrationRunning,
			EstimatedDataRows:     1000000,
			EstimatedDuration:     "5m30s",
		}

		// Test DeepCopy
		copied := original.DeepCopy()

		// Verify it's a different object
		assert.NotSame(t, original, copied)

		// Verify content is the same
		assert.Equal(t, original.SchemaMigrationStatus, copied.SchemaMigrationStatus)
		assert.Equal(t, original.DataMigrationStatus, copied.DataMigrationStatus)
		assert.Equal(t, original.EstimatedDataRows, copied.EstimatedDataRows)
		assert.Equal(t, original.EstimatedDuration, copied.EstimatedDuration)

		// Modify the copy and verify original is unchanged
		copied.DataMigrationStatus = DataMigrationCompleted
		assert.Equal(t, DataMigrationRunning, original.DataMigrationStatus)
	})
}

// TestClientCompatibility ensures no breaking changes to existing client code
func TestClientCompatibility(t *testing.T) {
	t.Run("can create Table without DataMigrations", func(t *testing.T) {
		// This ensures backward compatibility - existing code should still work
		table := &Table{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "legacy-table",
				Namespace: "default",
			},
			Spec: TableSpec{
				Database: "mydb",
				Name:     "users",
			},
		}

		// Should be able to create without DataMigrations field
		assert.NotNil(t, table)
		assert.Empty(t, table.Spec.DataMigrations)
	})

	t.Run("can create Migration without DML fields", func(t *testing.T) {
		// This ensures backward compatibility
		migration := &Migration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "legacy-migration",
				Namespace: "default",
			},
			Spec: MigrationSpec{
				DatabaseName: "mydb",
				TableName:    "users",
				GeneratedDDL: "ALTER TABLE users ADD COLUMN email",
			},
		}

		// Should be able to create without new DML fields
		assert.NotNil(t, migration)
		assert.Empty(t, migration.Spec.GeneratedDML)
		assert.Empty(t, migration.Spec.EditedDML)
	})
}

// TestNewTypesIntegration verifies all new types work together
func TestNewTypesIntegration(t *testing.T) {
	// Create a complete example using all new features
	table := &Table{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "user-table-v2",
			Namespace: "default",
		},
		Spec: TableSpec{
			Database: "myapp",
			Name:     "users",
			DataMigrations: []DataMigration{
				{
					Name:        "backfill-emails",
					Description: "Backfill email addresses",
					Type:        DataMigrationTypeBackfill,
					Template: &DataMigrationTemplate{
						Template: "UPDATE users SET email = CONCAT(username, '@example.com') WHERE email IS NULL",
						Parameters: []TemplateParameter{
							{
								Name:     "domain",
								Type:     ParameterTypeString,
								Default:  "example.com",
								Required: false,
							},
						},
					},
					Conditions: []DataMigrationCondition{
						{
							Query:    "SELECT COUNT(*) FROM users WHERE email IS NULL",
							Operator: ">",
							Value:    0,
						},
					},
					BatchSize: 5000,
					Timeout:   &metav1.Duration{Duration: 30 * time.Minute},
					Priority:  10,
					Tags:      []string{"email", "backfill"},
				},
			},
		},
	}

	// Verify the table can be created and copied
	tableCopy := table.DeepCopy()
	require.NotNil(t, tableCopy)
	assert.Equal(t, table.Spec.DataMigrations[0].Name, tableCopy.Spec.DataMigrations[0].Name)

	// Create a migration from this table
	migration := &Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "user-table-v2-migration",
			Namespace: "default",
		},
		Spec: MigrationSpec{
			DatabaseName:   table.Spec.Database,
			TableName:      table.Spec.Name,
			TableNamespace: table.Namespace,
			GeneratedDDL:   "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
			GeneratedDML:   "UPDATE users SET email = CONCAT(username, '@example.com') WHERE email IS NULL",
		},
		Status: MigrationStatus{
			Phase:                 "PLANNED",
			SchemaMigrationStatus: DataMigrationPending,
			DataMigrationStatus:   DataMigrationPending,
			EstimatedDataRows:     50000,
			EstimatedDuration:     "2m30s",
		},
	}

	// Verify the migration can be created and copied
	migrationCopy := migration.DeepCopy()
	require.NotNil(t, migrationCopy)
	assert.Equal(t, migration.Spec.GeneratedDML, migrationCopy.Spec.GeneratedDML)
	assert.Equal(t, migration.Status.DataMigrationStatus, migrationCopy.Status.DataMigrationStatus)
}
