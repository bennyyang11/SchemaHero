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

package sqlite

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/interfaces"
)

// SqliteDataMigrationPlanner implements the DataMigrationPlanner interface for SQLite
type SqliteDataMigrationPlanner struct {
	connection *SqliteConnection
}

// NewSqliteDataMigrationPlanner creates a new SQLite data migration planner
func NewSqliteDataMigrationPlanner(conn *SqliteConnection) interfaces.DataMigrationPlanner {
	return &SqliteDataMigrationPlanner{
		connection: conn,
	}
}

// PlanSqliteDataMigrations is the main entry point for planning SQLite data migrations
func PlanSqliteDataMigrations(uri string, tableName string, migrations []schemasv1alpha4.DataMigration) ([]string, error) {
	s, err := Connect(uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to sqlite")
	}
	defer s.Close()

	planner := NewSqliteDataMigrationPlanner(s)
	return planner.PlanDataMigrations(tableName, migrations)
}

// PlanDataMigrations generates DML statements from data migration specifications
func (s *SqliteDataMigrationPlanner) PlanDataMigrations(tableName string, migrations []schemasv1alpha4.DataMigration) ([]string, error) {
	if len(migrations) == 0 {
		return []string{}, nil
	}

	// Resolve dependencies and get execution order
	resolver := schemasv1alpha4.NewDependencyResolver(migrations)
	orderedMigrations, err := resolver.ResolveExecutionOrder()
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve migration dependencies")
	}

	var statements []string
	for _, migration := range orderedMigrations {
		// Check conditions before generating SQL
		shouldExecute, err := s.shouldExecuteMigration(migration)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to evaluate conditions for migration %s", migration.Name)
		}

		if !shouldExecute {
			// Add a comment indicating why the migration was skipped
			statements = append(statements, fmt.Sprintf("-- Migration %s: SKIPPED (conditions not met)", migration.Name))
			continue
		}

		// Generate SQL for this migration
		sql, err := s.PlanSingleDataMigration(tableName, migration)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to plan migration %s", migration.Name)
		}

		// Add comment with migration info
		statements = append(statements, fmt.Sprintf("-- Migration: %s", migration.Name))
		if migration.Description != "" {
			statements = append(statements, fmt.Sprintf("-- Description: %s", migration.Description))
		}
		statements = append(statements, sql)
		statements = append(statements, "") // Empty line for readability
	}

	return statements, nil
}

// PlanSingleDataMigration generates DML statements for a single data migration
func (s *SqliteDataMigrationPlanner) PlanSingleDataMigration(tableName string, migration *schemasv1alpha4.DataMigration) (string, error) {
	var sql string
	var err error

	// Generate SQL from either direct SQL or template
	if migration.SQL != "" {
		sql = migration.SQL
	} else if migration.Template != nil {
		sql, err = s.renderTemplate(migration.Template, map[string]interface{}{
			"table_name": tableName,
		})
		if err != nil {
			return "", errors.Wrap(err, "failed to render template")
		}
	} else {
		return "", errors.New("migration must have either SQL or Template specified")
	}

	// Adapt SQL for SQLite-specific syntax
	sql, err = s.GetDatabaseSpecificSQL(sql)
	if err != nil {
		return "", errors.Wrap(err, "failed to adapt SQL for SQLite")
	}

	// Add batching if specified
	if migration.BatchSize > 0 {
		sql = s.addBatchingToSQL(sql, int(migration.BatchSize))
	}

	return sql, nil
}

// ValidateCondition validates a condition against the SQLite database
func (s *SqliteDataMigrationPlanner) ValidateCondition(condition schemasv1alpha4.DataMigrationCondition) (bool, error) {
	db := s.connection.db

	// Execute the condition query
	rows, err := db.Query(condition.Query)
	if err != nil {
		return false, errors.Wrapf(err, "failed to execute condition query: %s", condition.Query)
	}
	defer rows.Close()

	if !rows.Next() {
		return false, errors.New("condition query returned no results")
	}

	var result interface{}
	if err := rows.Scan(&result); err != nil {
		return false, errors.Wrap(err, "failed to scan condition result")
	}

	// Convert result to comparable value
	var actualValue interface{}
	switch v := result.(type) {
	case int64:
		actualValue = v
	case float64:
		actualValue = v
	case string:
		actualValue = v
	case []byte:
		// SQLite sometimes returns numbers as []byte
		if val, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			actualValue = val
		} else if val, err := strconv.ParseFloat(string(v), 64); err == nil {
			actualValue = val
		} else {
			actualValue = string(v)
		}
	default:
		actualValue = fmt.Sprintf("%v", v)
	}

	// Expected value is already int64 from the condition
	expectedValue := condition.Value

	// Compare based on operator
	switch condition.Operator {
	case ">":
		return compareNumeric(actualValue, expectedValue, ">")
	case "<":
		return compareNumeric(actualValue, expectedValue, "<")
	case ">=":
		return compareNumeric(actualValue, expectedValue, ">=")
	case "<=":
		return compareNumeric(actualValue, expectedValue, "<=")
	case "=", "==":
		return fmt.Sprintf("%v", actualValue) == fmt.Sprintf("%v", expectedValue), nil
	case "!=":
		return fmt.Sprintf("%v", actualValue) != fmt.Sprintf("%v", expectedValue), nil
	default:
		return false, errors.Errorf("unsupported operator: %s", condition.Operator)
	}
}

// EstimateAffectedRows estimates the number of rows that would be affected by a migration
func (s *SqliteDataMigrationPlanner) EstimateAffectedRows(tableName string, migration *schemasv1alpha4.DataMigration) (int64, error) {
	sql := migration.SQL
	if migration.Template != nil {
		var err error
		sql, err = s.renderTemplate(migration.Template, map[string]interface{}{
			"table_name": tableName,
		})
		if err != nil {
			return 0, errors.Wrap(err, "failed to render template for estimation")
		}
	}
	// Convert UPDATE/DELETE to SELECT COUNT(*) for estimation
	estimationSQL := sql
	upperSQL := strings.ToUpper(strings.TrimSpace(sql))

	if strings.HasPrefix(upperSQL, "UPDATE") {
		// Extract WHERE clause from UPDATE statement
		whereIndex := strings.Index(upperSQL, "WHERE")
		if whereIndex != -1 {
			whereClause := sql[whereIndex:]
			estimationSQL = fmt.Sprintf("SELECT COUNT(*) FROM %s %s", extractTableNameFromUpdate(sql), whereClause)
		} else {
			tableName := extractTableNameFromUpdate(sql)
			estimationSQL = fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
		}
	} else if strings.HasPrefix(upperSQL, "DELETE") {
		// Extract WHERE clause from DELETE statement
		fromIndex := strings.Index(upperSQL, "FROM")
		whereIndex := strings.Index(upperSQL, "WHERE")
		if fromIndex != -1 && whereIndex != -1 {
			tableName := extractTableNameFromDelete(sql)
			whereClause := sql[whereIndex:]
			estimationSQL = fmt.Sprintf("SELECT COUNT(*) FROM %s %s", tableName, whereClause)
		} else if fromIndex != -1 {
			tableName := extractTableNameFromDelete(sql)
			estimationSQL = fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
		}
	} else {
		// For INSERT or other operations, return default estimate
		return 1000, nil
	}

	db := s.connection.db
	var count int64
	err := db.QueryRow(estimationSQL).Scan(&count)
	if err != nil {
		// If estimation fails, return a default
		return 1000, nil
	}

	return count, nil
}

// GetDatabaseSpecificSQL adapts generic SQL to SQLite-specific syntax
func (s *SqliteDataMigrationPlanner) GetDatabaseSpecificSQL(sql string) (string, error) {
	// SQLite-specific adaptations
	adaptedSQL := sql

	// SQLite uses || for concatenation (same as PostgreSQL)
	// No changes needed for concatenation

	// Adapt date functions
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "NOW()", "datetime('now')")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "CURRENT_DATE", "date('now')")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "CURRENT_TIMESTAMP", "datetime('now')")

	// SQLite doesn't support INTERVAL syntax like PostgreSQL
	// Convert PostgreSQL interval syntax to SQLite date functions
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "INTERVAL '1 day'", "'+1 day'")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "INTERVAL '1 week'", "'+7 days'")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "INTERVAL '1 month'", "'+1 month'")

	// SQLite uses double quotes for identifiers (similar to PostgreSQL)
	// No changes needed for identifier quoting

	return adaptedSQL, nil
}

// shouldExecuteMigration checks if a migration should be executed based on conditions
func (s *SqliteDataMigrationPlanner) shouldExecuteMigration(migration *schemasv1alpha4.DataMigration) (bool, error) {
	if len(migration.Conditions) == 0 {
		return true, nil
	}

	for _, condition := range migration.Conditions {
		result, err := s.ValidateCondition(condition)
		if err != nil {
			return false, errors.Wrapf(err, "failed to validate condition: %s", condition.Query)
		}
		if !result {
			return false, nil
		}
	}

	return true, nil
}

// renderTemplate renders a migration template with provided values
func (s *SqliteDataMigrationPlanner) renderTemplate(template *schemasv1alpha4.DataMigrationTemplate, values map[string]interface{}) (string, error) {
	// Add SQLite-specific default values
	sqliteValues := map[string]interface{}{
		"NOW":          "datetime('now')",
		"CURRENT_DATE": "date('now')",
	}

	// Merge with provided values (provided values take precedence)
	for k, v := range sqliteValues {
		if _, exists := values[k]; !exists {
			values[k] = v
		}
	}

	return schemasv1alpha4.RenderTemplate(template.Template, values)
}

// addBatchingToSQL adds LIMIT clause for batching UPDATE/DELETE operations
func (s *SqliteDataMigrationPlanner) addBatchingToSQL(sql string, batchSize int) string {
	upperSQL := strings.ToUpper(strings.TrimSpace(sql))

	// Only add batching to UPDATE and DELETE statements
	if strings.HasPrefix(upperSQL, "UPDATE") || strings.HasPrefix(upperSQL, "DELETE") {
		// Check if LIMIT already exists
		if !strings.Contains(upperSQL, "LIMIT") {
			return fmt.Sprintf("%s LIMIT %d", sql, batchSize)
		}
	}

	return sql
}

// compareNumeric compares two values numerically
func compareNumeric(actual, expected interface{}, operator string) (bool, error) {
	actualFloat, err := convertToFloat64(actual)
	if err != nil {
		return false, errors.Wrapf(err, "failed to convert actual value to number: %v", actual)
	}

	expectedFloat, err := convertToFloat64(expected)
	if err != nil {
		return false, errors.Wrapf(err, "failed to convert expected value to number: %v", expected)
	}

	switch operator {
	case ">":
		return actualFloat > expectedFloat, nil
	case "<":
		return actualFloat < expectedFloat, nil
	case ">=":
		return actualFloat >= expectedFloat, nil
	case "<=":
		return actualFloat <= expectedFloat, nil
	default:
		return false, errors.Errorf("unsupported numeric operator: %s", operator)
	}
}

// convertToFloat64 converts various numeric types to float64
func convertToFloat64(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	case int:
		return float64(v), nil
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, nil
		}
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return float64(i), nil
		}
		return 0, errors.Errorf("cannot convert string to number: %s", v)
	default:
		return 0, errors.Errorf("unsupported value type: %T", value)
	}
}

// extractTableNameFromUpdate extracts table name from UPDATE statement
func extractTableNameFromUpdate(sql string) string {
	parts := strings.Fields(sql)
	if len(parts) >= 2 && strings.ToUpper(parts[0]) == "UPDATE" {
		return parts[1]
	}
	return "unknown_table"
}

// extractTableNameFromDelete extracts table name from DELETE statement
func extractTableNameFromDelete(sql string) string {
	upperSQL := strings.ToUpper(sql)
	fromIndex := strings.Index(upperSQL, "FROM")
	if fromIndex == -1 {
		return "unknown_table"
	}

	// Extract everything after FROM until the next keyword
	afterFrom := strings.TrimSpace(sql[fromIndex+4:])
	parts := strings.Fields(afterFrom)
	if len(parts) > 0 {
		return parts[0]
	}

	return "unknown_table"
}
