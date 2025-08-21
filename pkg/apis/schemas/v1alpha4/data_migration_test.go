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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestDataMigrationSerialization(t *testing.T) {
	timeout := metav1.Duration{Duration: 30 * time.Minute}
	dm := DataMigration{
		Name:        "test-migration",
		Description: "Test data migration",
		SQL:         "UPDATE users SET status = 'active' WHERE status IS NULL",
		Conditions: []DataMigrationCondition{
			{
				Query:    "SELECT COUNT(*) FROM users WHERE status IS NULL",
				Operator: ">",
				Value:    0,
			},
		},
		DependsOn: []string{"previous-migration"},
		BatchSize: 1000,
		Timeout:   &timeout,
	}

	t.Run("JSON serialization", func(t *testing.T) {
		data, err := json.Marshal(dm)
		require.NoError(t, err)

		var decoded DataMigration
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, dm.Name, decoded.Name)
		assert.Equal(t, dm.Description, decoded.Description)
		assert.Equal(t, dm.SQL, decoded.SQL)
		assert.Equal(t, dm.BatchSize, decoded.BatchSize)
		assert.Equal(t, dm.Timeout.Duration, decoded.Timeout.Duration)
		assert.Equal(t, len(dm.Conditions), len(decoded.Conditions))
		assert.Equal(t, len(dm.DependsOn), len(decoded.DependsOn))
	})

	t.Run("YAML serialization", func(t *testing.T) {
		data, err := yaml.Marshal(dm)
		require.NoError(t, err)

		var decoded DataMigration
		err = yaml.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, dm.Name, decoded.Name)
		assert.Equal(t, dm.Description, decoded.Description)
		assert.Equal(t, dm.SQL, decoded.SQL)
		assert.Equal(t, dm.BatchSize, decoded.BatchSize)
		assert.Equal(t, dm.Timeout.Duration, decoded.Timeout.Duration)
	})
}

func TestTableSpecWithDataMigrations(t *testing.T) {
	table := Table{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "schemas.schemahero.io/v1alpha4",
			Kind:       "Table",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "users-table",
			Namespace: "default",
		},
		Spec: TableSpec{
			Database: "myapp",
			Name:     "users",
			Schema: &TableSchema{
				Postgres: &PostgresqlTableSchema{
					Columns: []*PostgresqlTableColumn{
						{
							Name: "id",
							Type: "integer",
						},
						{
							Name: "status",
							Type: "varchar(20)",
						},
					},
				},
			},
			DataMigrations: []DataMigration{
				{
					Name:        "set-default-status",
					Description: "Set default status for existing users",
					SQL:         "UPDATE users SET status = 'active' WHERE status IS NULL",
					BatchSize:   1000,
				},
			},
		},
	}

	t.Run("TableSpec with DataMigrations serialization", func(t *testing.T) {
		data, err := json.Marshal(table)
		require.NoError(t, err)

		var decoded Table
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, table.Spec.Database, decoded.Spec.Database)
		assert.Equal(t, table.Spec.Name, decoded.Spec.Name)
		assert.Equal(t, len(table.Spec.DataMigrations), len(decoded.Spec.DataMigrations))
		assert.Equal(t, table.Spec.DataMigrations[0].Name, decoded.Spec.DataMigrations[0].Name)
		assert.Equal(t, table.Spec.DataMigrations[0].SQL, decoded.Spec.DataMigrations[0].SQL)
	})
}

func TestMigrationSpecWithDataFields(t *testing.T) {
	migration := Migration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "schemas.schemahero.io/v1alpha4",
			Kind:       "Migration",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "users-migration-v2",
			Namespace: "default",
		},
		Spec: MigrationSpec{
			DatabaseName:   "myapp",
			TableName:      "users",
			TableNamespace: "default",
			GeneratedDDL:   "ALTER TABLE users ADD COLUMN status varchar(20);",
			GeneratedDML:   "UPDATE users SET status = 'active' WHERE status IS NULL;",
			EditedDDL:      "",
			EditedDML:      "",
		},
		Status: MigrationStatus{
			Phase:                 Planned,
			SchemaMigrationStatus: DataMigrationPending,
			DataMigrationStatus:   DataMigrationPending,
			EstimatedDataRows:     50000,
			EstimatedDuration:     "2m30s",
		},
	}

	t.Run("MigrationSpec with data fields serialization", func(t *testing.T) {
		data, err := json.Marshal(migration)
		require.NoError(t, err)

		var decoded Migration
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, migration.Spec.GeneratedDDL, decoded.Spec.GeneratedDDL)
		assert.Equal(t, migration.Spec.GeneratedDML, decoded.Spec.GeneratedDML)
		assert.Equal(t, migration.Status.SchemaMigrationStatus, decoded.Status.SchemaMigrationStatus)
		assert.Equal(t, migration.Status.DataMigrationStatus, decoded.Status.DataMigrationStatus)
		assert.Equal(t, migration.Status.EstimatedDataRows, decoded.Status.EstimatedDataRows)
		assert.Equal(t, migration.Status.EstimatedDuration, decoded.Status.EstimatedDuration)
	})
}

func TestBackwardCompatibility(t *testing.T) {
	t.Run("Table without DataMigrations", func(t *testing.T) {
		// Existing table JSON without data migrations
		oldTableJSON := `{
			"apiVersion": "schemas.schemahero.io/v1alpha4",
			"kind": "Table",
			"metadata": {
				"name": "legacy-table",
				"namespace": "default"
			},
			"spec": {
				"database": "myapp",
				"name": "legacy",
				"schema": {
					"postgres": {
						"columns": [
							{"name": "id", "type": "integer"}
						]
					}
				}
			}
		}`

		var table Table
		err := json.Unmarshal([]byte(oldTableJSON), &table)
		require.NoError(t, err)

		assert.Equal(t, "legacy-table", table.Name)
		assert.Equal(t, "myapp", table.Spec.Database)
		assert.Nil(t, table.Spec.DataMigrations) // Should be nil, not empty slice
	})

	t.Run("Migration without data fields", func(t *testing.T) {
		// Existing migration JSON without data fields
		oldMigrationJSON := `{
			"apiVersion": "schemas.schemahero.io/v1alpha4",
			"kind": "Migration",
			"metadata": {
				"name": "legacy-migration",
				"namespace": "default"
			},
			"spec": {
				"databaseName": "myapp",
				"tableName": "legacy",
				"tableNamespace": "default",
				"generatedDDL": "CREATE TABLE legacy (id integer);"
			},
			"status": {
				"phase": "PLANNED"
			}
		}`

		var migration Migration
		err := json.Unmarshal([]byte(oldMigrationJSON), &migration)
		require.NoError(t, err)

		assert.Equal(t, "legacy-migration", migration.Name)
		assert.Equal(t, "myapp", migration.Spec.DatabaseName)
		assert.Empty(t, migration.Spec.GeneratedDML)
		assert.Empty(t, migration.Spec.EditedDML)
		assert.Empty(t, migration.Status.SchemaMigrationStatus)
		assert.Empty(t, migration.Status.DataMigrationStatus)
	})
}

func TestDataMigrationDeepCopy(t *testing.T) {
	timeout := metav1.Duration{Duration: 30 * time.Minute}
	original := &DataMigration{
		Name:        "test-migration",
		Description: "Test data migration",
		SQL:         "UPDATE users SET status = 'active'",
		Conditions: []DataMigrationCondition{
			{
				Query:    "SELECT COUNT(*) FROM users",
				Operator: ">",
				Value:    0,
			},
		},
		DependsOn: []string{"previous-migration"},
		BatchSize: 1000,
		Timeout:   &timeout,
	}

	t.Run("DeepCopy creates independent copy", func(t *testing.T) {
		copy := original.DeepCopy()

		// Verify the copy is equal
		assert.Equal(t, original.Name, copy.Name)
		assert.Equal(t, original.Description, copy.Description)
		assert.Equal(t, original.SQL, copy.SQL)
		assert.Equal(t, original.BatchSize, copy.BatchSize)
		assert.Equal(t, original.Timeout.Duration, copy.Timeout.Duration)

		// Modify the copy
		copy.Name = "modified"
		copy.Conditions[0].Value = 100
		copy.DependsOn[0] = "modified-dependency"

		// Verify original is unchanged
		assert.Equal(t, "test-migration", original.Name)
		assert.Equal(t, int64(0), original.Conditions[0].Value)
		assert.Equal(t, "previous-migration", original.DependsOn[0])
	})
} 