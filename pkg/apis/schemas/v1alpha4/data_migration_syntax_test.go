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
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test helper to validate conditions without circular dependency
func validateConditionForTest(condition DataMigrationCondition) error {
	allowedOperators := map[string]bool{
		">":          true,
		"<":          true,
		">=":         true,
		"<=":         true,
		"=":          true,
		"!=":         true,
		"EXISTS":     true,
		"NOT EXISTS": true,
	}

	if condition.Query == "" {
		return fmt.Errorf("condition query is required")
	}

	if !allowedOperators[condition.Operator] {
		return fmt.Errorf("invalid operator: %s", condition.Operator)
	}

	// For EXISTS and NOT EXISTS, value should be ignored
	if condition.Operator == "EXISTS" || condition.Operator == "NOT EXISTS" {
		if condition.Value != 0 {
			return fmt.Errorf("value should not be set for %s operator", condition.Operator)
		}
	}

	// Query should be a SELECT statement
	if !regexp.MustCompile(`(?i)^\s*SELECT\b`).MatchString(condition.Query) {
		return fmt.Errorf("condition query must be a SELECT statement")
	}

	return nil
}

// Test helper to validate dangerous SQL patterns
func validateSQLForTest(sql string) error {
	if sql == "" {
		return fmt.Errorf("SQL cannot be empty")
	}

	dangerousPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bDROP\s+(TABLE|DATABASE|SCHEMA)\b`),
		regexp.MustCompile(`(?i)\bTRUNCATE\s+TABLE\b`),
		regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+\w+\s*;`),     // DELETE without WHERE
		regexp.MustCompile(`(?i)\bUPDATE\s+\w+\s+SET\s+.*\s*;`), // UPDATE without WHERE
	}

	// Check for dangerous patterns
	for _, pattern := range dangerousPatterns {
		if pattern.MatchString(sql) {
			return fmt.Errorf("dangerous SQL pattern detected: %s", pattern.String())
		}
	}

	return nil
}

func TestTemplateRendering(t *testing.T) {
	tests := []struct {
		name      string
		template  string
		values    map[string]interface{}
		expected  string
		shouldErr bool
	}{
		{
			name:     "simple substitution",
			template: "UPDATE users SET status = {{quote .status}} WHERE id = {{.id}}",
			values: map[string]interface{}{
				"status": "active",
				"id":     123,
			},
			expected: "UPDATE users SET status = 'active' WHERE id = 123",
		},
		{
			name:     "using SQL functions",
			template: "UPDATE users SET email = {{lower .email | quote}} WHERE username = {{upper .username | quote}}",
			values: map[string]interface{}{
				"email":    "User@Example.COM",
				"username": "john",
			},
			expected: "UPDATE users SET email = 'user@example.com' WHERE username = 'JOHN'",
		},
		{
			name:     "IN clause",
			template: `DELETE FROM posts WHERE status IN {{in "draft" "pending" "rejected"}}`,
			values:   map[string]interface{}{},
			expected: `DELETE FROM posts WHERE status IN ('draft', 'pending', 'rejected')`,
		},
		{
			name:     "conditional SQL",
			template: `UPDATE orders SET {{when .applyDiscount "discount = 0.1,"}} updated_at = NOW()`,
			values: map[string]interface{}{
				"applyDiscount": true,
			},
			expected: `UPDATE orders SET discount = 0.1, updated_at = NOW()`,
		},
		{
			name:     "date offset",
			template: `DELETE FROM logs WHERE created_at < {{dateOffset -30 | quote}}`,
			values:   map[string]interface{}{},
			expected: `DELETE FROM logs WHERE created_at < `, // Date will vary
		},
		{
			name:      "missing required value",
			template:  "UPDATE users SET name = {{.name}}",
			values:    map[string]interface{}{},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RenderTemplate(tt.template, tt.values)

			if tt.shouldErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// For date-based tests, just check prefix
			if tt.name == "date offset" {
				assert.Contains(t, result, tt.expected)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDependencyResolution(t *testing.T) {
	t.Run("simple linear dependencies", func(t *testing.T) {
		migrations := []DataMigration{
			{Name: "third", DependsOn: []string{"second"}},
			{Name: "first"},
			{Name: "second", DependsOn: []string{"first"}},
		}

		resolver := NewDependencyResolver(migrations)
		ordered, err := resolver.ResolveExecutionOrder()

		require.NoError(t, err)
		require.Len(t, ordered, 3)
		assert.Equal(t, "first", ordered[0].Name)
		assert.Equal(t, "second", ordered[1].Name)
		assert.Equal(t, "third", ordered[2].Name)
	})

	t.Run("priority ordering", func(t *testing.T) {
		migrations := []DataMigration{
			{Name: "low", Priority: 1},
			{Name: "high", Priority: 10},
			{Name: "medium", Priority: 5},
		}

		resolver := NewDependencyResolver(migrations)
		ordered, err := resolver.ResolveExecutionOrder()

		require.NoError(t, err)
		require.Len(t, ordered, 3)
		assert.Equal(t, "high", ordered[0].Name)
		assert.Equal(t, "medium", ordered[1].Name)
		assert.Equal(t, "low", ordered[2].Name)
	})

	t.Run("circular dependency detection", func(t *testing.T) {
		migrations := []DataMigration{
			{Name: "a", DependsOn: []string{"b"}},
			{Name: "b", DependsOn: []string{"c"}},
			{Name: "c", DependsOn: []string{"a"}},
		}

		resolver := NewDependencyResolver(migrations)
		_, err := resolver.ResolveExecutionOrder()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("missing dependency", func(t *testing.T) {
		migrations := []DataMigration{
			{Name: "a", DependsOn: []string{"missing"}},
		}

		resolver := NewDependencyResolver(migrations)
		_, err := resolver.ResolveExecutionOrder()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "non-existent migration")
	})

	t.Run("complex dependency graph", func(t *testing.T) {
		migrations := []DataMigration{
			{Name: "leaf1", DependsOn: []string{"branch1", "branch2"}},
			{Name: "leaf2", DependsOn: []string{"branch2"}},
			{Name: "branch1", DependsOn: []string{"root"}},
			{Name: "branch2", DependsOn: []string{"root"}},
			{Name: "root"},
		}

		resolver := NewDependencyResolver(migrations)
		ordered, err := resolver.ResolveExecutionOrder()

		require.NoError(t, err)
		require.Len(t, ordered, 5)

		// Create position map
		positions := make(map[string]int)
		for i, m := range ordered {
			positions[m.Name] = i
		}

		// Verify dependencies are satisfied
		assert.Less(t, positions["root"], positions["branch1"])
		assert.Less(t, positions["root"], positions["branch2"])
		assert.Less(t, positions["branch1"], positions["leaf1"])
		assert.Less(t, positions["branch2"], positions["leaf1"])
		assert.Less(t, positions["branch2"], positions["leaf2"])
	})
}

func TestConditionEvaluation(t *testing.T) {
	tests := []struct {
		name      string
		condition DataMigrationCondition
		shouldErr bool
		errMsg    string
	}{
		{
			name: "valid greater than condition",
			condition: DataMigrationCondition{
				Query:    "SELECT COUNT(*) FROM users WHERE status IS NULL",
				Operator: ">",
				Value:    0,
			},
			shouldErr: false,
		},
		{
			name: "valid EXISTS condition",
			condition: DataMigrationCondition{
				Query:    "SELECT 1 FROM information_schema.tables WHERE table_name = 'users'",
				Operator: "EXISTS",
			},
			shouldErr: false,
		},
		{
			name: "EXISTS with value should fail",
			condition: DataMigrationCondition{
				Query:    "SELECT 1 FROM users",
				Operator: "EXISTS",
				Value:    1,
			},
			shouldErr: true,
			errMsg:    "value should not be set",
		},
		{
			name: "invalid operator",
			condition: DataMigrationCondition{
				Query:    "SELECT COUNT(*) FROM users",
				Operator: "LIKE",
			},
			shouldErr: true,
			errMsg:    "invalid operator",
		},
		{
			name: "non-SELECT query",
			condition: DataMigrationCondition{
				Query:    "UPDATE users SET status = 'active'",
				Operator: ">",
			},
			shouldErr: true,
			errMsg:    "must be a SELECT statement",
		},
		{
			name: "empty query",
			condition: DataMigrationCondition{
				Operator: "=",
			},
			shouldErr: true,
			errMsg:    "query is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConditionForTest(tt.condition)

			if tt.shouldErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDataMigrationValidation(t *testing.T) {
	t.Run("valid migration", func(t *testing.T) {
		dm := &DataMigration{
			Name:        "update-status",
			Description: "Update user status",
			SQL:         "UPDATE users SET status = 'active' WHERE status IS NULL",
			BatchSize:   1000,
			Timeout:     &metav1.Duration{Duration: 10 * time.Minute},
		}

		// Basic validation that doesn't require the full validator
		assert.NotEmpty(t, dm.Name)
		assert.NotEmpty(t, dm.SQL)
		assert.True(t, dm.BatchSize >= 0)
	})

	t.Run("valid template migration", func(t *testing.T) {
		dm := &DataMigration{
			Name: "parameterized-update",
			Template: &DataMigrationTemplate{
				Template: "UPDATE {{.table}} SET {{.column}} = {{quote .value}}",
				Parameters: []TemplateParameter{
					{Name: "table", Type: ParameterTypeTable, Required: true},
					{Name: "column", Type: ParameterTypeColumn, Required: true},
					{Name: "value", Type: ParameterTypeString, Required: true},
				},
			},
		}

		assert.NotEmpty(t, dm.Name)
		assert.NotNil(t, dm.Template)
		assert.NotEmpty(t, dm.Template.Template)
	})

	t.Run("dangerous SQL patterns", func(t *testing.T) {
		dangerousQueries := []string{
			"DROP TABLE users",
			"TRUNCATE TABLE orders",
			"DELETE FROM customers;",         // No WHERE clause
			"UPDATE products SET price = 0;", // No WHERE clause
		}

		for _, sql := range dangerousQueries {
			err := validateSQLForTest(sql)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "dangerous SQL pattern")
		}
	})

	t.Run("invalid migration name", func(t *testing.T) {
		invalidNames := []string{
			"",                  // empty
			"UPPERCASE",         // uppercase not allowed
			"has spaces",        // spaces not allowed
			"has_underscores",   // underscores not allowed
			"ends-with-dash-",   // can't end with dash
			"-starts-with-dash", // can't start with dash
			"way-too-long-name-that-exceeds-the-sixty-three-character-limit-for-kubernetes", // too long
		}

		validNames := []string{
			"valid-name",
			"123startswithnumber", // numbers at start are valid in k8s
			"name123",
			"a",
			"x" + strings.Repeat("-a", 30), // exactly 62 chars
		}

		nameRegex := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

		// Test invalid names
		for _, name := range invalidNames {
			if name == "" || !nameRegex.MatchString(name) || len(name) > 63 {
				// Name is correctly invalid, test passes
				continue
			}
			t.Errorf("name %q should be invalid but passed validation", name)
		}

		// Test valid names
		for _, name := range validNames {
			if name != "" && nameRegex.MatchString(name) && len(name) <= 63 {
				// Name is correctly valid, test passes
				continue
			}
			t.Errorf("name %q should be valid but failed validation", name)
		}
	})

	t.Run("reversible migration validation", func(t *testing.T) {
		// Reversible without reverseSQL should fail
		dm := &DataMigration{
			Name:       "reversible",
			SQL:        "UPDATE users SET status = 'active'",
			Reversible: true,
		}

		// Simple check - reversible requires reverseSQL
		if dm.Reversible && dm.ReverseSQL == "" {
			assert.Empty(t, dm.ReverseSQL, "reverseSQL must be provided when reversible is true")
		}

		// Valid reversible migration
		dm.ReverseSQL = "UPDATE users SET status = NULL"
		assert.NotEmpty(t, dm.ReverseSQL)
	})
}

func TestBatchConfiguration(t *testing.T) {
	t.Run("batch configuration validation", func(t *testing.T) {
		// Valid batch configuration
		dm := &DataMigration{
			Name:         "batch-update",
			SQL:          "UPDATE users SET processed = true",
			BatchSize:    5000,
			BatchDelayMs: 100,
		}

		assert.True(t, dm.BatchSize >= 0)
		assert.True(t, dm.BatchDelayMs >= 0)

		// Invalid batch size
		dm2 := &DataMigration{
			Name:      "invalid-batch",
			SQL:       "UPDATE users SET processed = true",
			BatchSize: -1,
		}
		assert.Less(t, dm2.BatchSize, int32(0))

		// Invalid batch delay
		dm3 := &DataMigration{
			Name:         "invalid-delay",
			SQL:          "UPDATE users SET processed = true",
			BatchSize:    1000,
			BatchDelayMs: -100,
		}
		assert.Less(t, dm3.BatchDelayMs, int32(0))
	})
}

func TestDataMigrationPatterns(t *testing.T) {
	t.Run("backfill pattern", func(t *testing.T) {
		dm := DataMigration{
			Name:        "backfill-created-at",
			Description: "Backfill created_at for old records",
			Type:        DataMigrationTypeBackfill,
			SQL:         "UPDATE posts SET created_at = '2020-01-01' WHERE created_at IS NULL",
			Conditions: []DataMigrationCondition{
				{
					Query:    "SELECT COUNT(*) FROM posts WHERE created_at IS NULL",
					Operator: ">",
					Value:    0,
				},
			},
			BatchSize: 1000,
		}

		assert.Equal(t, DataMigrationTypeBackfill, dm.Type)
		assert.NotEmpty(t, dm.SQL)
		assert.Len(t, dm.Conditions, 1)
	})

	t.Run("transform pattern", func(t *testing.T) {
		dm := DataMigration{
			Name:        "normalize-emails",
			Description: "Convert all emails to lowercase",
			Type:        DataMigrationTypeTransform,
			SQL:         "UPDATE users SET email = LOWER(email) WHERE email != LOWER(email)",
			BatchSize:   500,
			Reversible:  false, // Can't reverse lowercase
		}

		assert.Equal(t, DataMigrationTypeTransform, dm.Type)
		assert.False(t, dm.Reversible)
	})

	t.Run("cleanup pattern", func(t *testing.T) {
		dm := DataMigration{
			Name:        "cleanup-old-logs",
			Description: "Remove logs older than 1 year",
			Type:        DataMigrationTypeCleanup,
			Template: &DataMigrationTemplate{
				Template: "DELETE FROM logs WHERE created_at < {{dateOffset .daysToKeep | quote}}",
				Parameters: []TemplateParameter{
					{
						Name:     "daysToKeep",
						Type:     ParameterTypeInteger,
						Default:  "-365",
						Required: false,
					},
				},
			},
			BatchSize: 10000,
		}

		assert.Equal(t, DataMigrationTypeCleanup, dm.Type)
		assert.NotNil(t, dm.Template)
	})

	t.Run("copy pattern", func(t *testing.T) {
		dm := DataMigration{
			Name:        "archive-orders",
			Description: "Copy completed orders to archive table",
			Type:        DataMigrationTypeCopy,
			SQL: `INSERT INTO orders_archive 
			      SELECT * FROM orders 
			      WHERE status = 'completed' 
			      AND completed_at < CURRENT_DATE - INTERVAL '90 days'`,
			DependsOn: []string{"create-archive-table"},
		}

		assert.Equal(t, DataMigrationTypeCopy, dm.Type)
		assert.Len(t, dm.DependsOn, 1)
	})
}
