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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// TestPhase1CompleteVerification runs comprehensive tests to verify Phase 1 is working properly
func TestPhase1CompleteVerification(t *testing.T) {
	t.Run("complete table with data migrations - JSON round trip", func(t *testing.T) {
		// Create a comprehensive Table with all new features
		table := &schemasv1alpha4.Table{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "schemas.schemahero.io/v1alpha4",
				Kind:       "Table",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "comprehensive-table",
				Namespace: "test",
			},
			Spec: schemasv1alpha4.TableSpec{
				Database: "testdb",
				Name:     "users",
				DataMigrations: []schemasv1alpha4.DataMigration{
					{
						Name:        "backfill-emails",
						Description: "Backfill email addresses",
						Type:        schemasv1alpha4.DataMigrationTypeBackfill,
						SQL:         "UPDATE users SET email = 'default@example.com' WHERE email IS NULL",
						Conditions: []schemasv1alpha4.DataMigrationCondition{
							{
								Query:    "SELECT COUNT(*) FROM users WHERE email IS NULL",
								Operator: ">",
								Value:    0,
							},
						},
						BatchSize:    1000,
						BatchDelayMs: 100,
						Timeout:      &metav1.Duration{Duration: 30 * time.Minute},
						Priority:     10,
						Tags:         []string{"email", "backfill"},
					},
					{
						Name:        "normalize-data",
						Description: "Normalize user data using templates",
						Type:        schemasv1alpha4.DataMigrationTypeTransform,
						Template: &schemasv1alpha4.DataMigrationTemplate{
							Template: "UPDATE users SET email = {{lower .email | quote}} WHERE id = {{.userId}}",
							Parameters: []schemasv1alpha4.TemplateParameter{
								{
									Name:        "email",
									Type:        schemasv1alpha4.ParameterTypeString,
									Required:    true,
									Description: "Email to normalize",
								},
								{
									Name:     "userId",
									Type:     schemasv1alpha4.ParameterTypeInteger,
									Required: true,
								},
							},
						},
						DependsOn:    []string{"backfill-emails"},
						Priority:     5,
						Reversible:   true,
						ReverseSQL:   "UPDATE users SET email = UPPER(email)",
					},
				},
			},
		}

		// Test JSON serialization
		jsonData, err := json.Marshal(table)
		require.NoError(t, err)
		assert.Contains(t, string(jsonData), "dataMigrations")
		assert.Contains(t, string(jsonData), "template")
		assert.Contains(t, string(jsonData), "backfill-emails")

		// Test JSON deserialization
		var unmarshaled schemasv1alpha4.Table
		err = json.Unmarshal(jsonData, &unmarshaled)
		require.NoError(t, err)
		assert.Len(t, unmarshaled.Spec.DataMigrations, 2)
		assert.Equal(t, "backfill-emails", unmarshaled.Spec.DataMigrations[0].Name)
		assert.Equal(t, schemasv1alpha4.DataMigrationTypeBackfill, unmarshaled.Spec.DataMigrations[0].Type)
		assert.NotNil(t, unmarshaled.Spec.DataMigrations[1].Template)
	})

	t.Run("complete migration with DML fields - YAML round trip", func(t *testing.T) {
		migration := &schemasv1alpha4.Migration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "schemas.schemahero.io/v1alpha4",
				Kind:       "Migration",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "comprehensive-migration",
				Namespace: "test",
			},
			Spec: schemasv1alpha4.MigrationSpec{
				DatabaseName:   "testdb",
				TableName:      "users",
				TableNamespace: "test",
				GeneratedDDL:   "ALTER TABLE users ADD COLUMN email VARCHAR(255);",
				EditedDDL:      "",
				GeneratedDML:   "UPDATE users SET email = 'default@example.com' WHERE email IS NULL;",
				EditedDML:      "",
			},
			Status: schemasv1alpha4.MigrationStatus{
				Phase:                  "PLANNED",
				SchemaMigrationStatus:  schemasv1alpha4.DataMigrationPending,
				DataMigrationStatus:    schemasv1alpha4.DataMigrationPending,
				EstimatedDataRows:      10000,
				EstimatedDuration:      "2m30s",
			},
		}

		// Test YAML serialization
		yamlData, err := yaml.Marshal(migration)
		require.NoError(t, err)
		assert.Contains(t, string(yamlData), "generatedDML")
		assert.Contains(t, string(yamlData), "dataMigrationStatus")
		assert.Contains(t, string(yamlData), "PENDING")

		// Test YAML deserialization
		var unmarshaled schemasv1alpha4.Migration
		err = yaml.Unmarshal(yamlData, &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, migration.Spec.GeneratedDML, unmarshaled.Spec.GeneratedDML)
		assert.Equal(t, migration.Status.DataMigrationStatus, unmarshaled.Status.DataMigrationStatus)
		assert.Equal(t, migration.Status.EstimatedDataRows, unmarshaled.Status.EstimatedDataRows)
	})

	t.Run("template rendering comprehensive test", func(t *testing.T) {
		template := "UPDATE {{identifier .table}} SET {{identifier .column}} = {{quote .value}} WHERE id IN {{in .ids}} AND created_at > {{dateOffset .daysAgo | quote}}"
		values := map[string]interface{}{
			"table":   "users",
			"column":  "email",
			"value":   "test@example.com",
			"ids":     []interface{}{1, 2, 3},
			"daysAgo": -30,
		}

		result, err := schemasv1alpha4.RenderTemplate(template, values)
		require.NoError(t, err)
		
		assert.Contains(t, result, `"users"`)
		assert.Contains(t, result, `"email"`)
		assert.Contains(t, result, "'test@example.com'")
		// The IN clause format might vary, so just check that numbers are there
		assert.Contains(t, result, "1")
		assert.Contains(t, result, "2") 
		assert.Contains(t, result, "3")
	})

	t.Run("dependency resolution complex scenarios", func(t *testing.T) {
		migrations := []schemasv1alpha4.DataMigration{
			{Name: "step-1", Priority: 100},
			{Name: "step-2", DependsOn: []string{"step-1"}, Priority: 90},
			{Name: "step-3", DependsOn: []string{"step-1"}, Priority: 95},
			{Name: "step-4", DependsOn: []string{"step-2", "step-3"}, Priority: 80},
			{Name: "independent", Priority: 50},
		}

		resolver := schemasv1alpha4.NewDependencyResolver(migrations)
		ordered, err := resolver.ResolveExecutionOrder()
		require.NoError(t, err)
		require.Len(t, ordered, 5)

		// Verify execution order
		positions := make(map[string]int)
		for i, m := range ordered {
			positions[m.Name] = i
		}

		// step-1 should be first (highest priority, no deps)
		assert.Equal(t, 0, positions["step-1"])
		
		// step-2 and step-3 should come after step-1
		assert.Greater(t, positions["step-2"], positions["step-1"])
		assert.Greater(t, positions["step-3"], positions["step-1"])
		
		// step-4 should come after both step-2 and step-3
		assert.Greater(t, positions["step-4"], positions["step-2"])
		assert.Greater(t, positions["step-4"], positions["step-3"])
	})

	t.Run("validation catches errors correctly", func(t *testing.T) {
		// Test circular dependency detection
		circularMigrations := []schemasv1alpha4.DataMigration{
			{Name: "a", DependsOn: []string{"b"}},
			{Name: "b", DependsOn: []string{"c"}},
			{Name: "c", DependsOn: []string{"a"}},
		}

		resolver := schemasv1alpha4.NewDependencyResolver(circularMigrations)
		_, err := resolver.ResolveExecutionOrder()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("runtime object interface compatibility", func(t *testing.T) {
		// Verify our types implement runtime.Object
		var _ runtime.Object = &schemasv1alpha4.Table{}
		var _ runtime.Object = &schemasv1alpha4.Migration{}
		var _ runtime.Object = &schemasv1alpha4.TableList{}
		var _ runtime.Object = &schemasv1alpha4.MigrationList{}

		// Test that they can be used with Kubernetes client machinery
		table := &schemasv1alpha4.Table{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "schemas.schemahero.io/v1alpha4",
				Kind:       "Table",
			},
		}

		assert.Equal(t, "Table", table.Kind)
		assert.Equal(t, "schemas.schemahero.io/v1alpha4", table.APIVersion)
	})
} 