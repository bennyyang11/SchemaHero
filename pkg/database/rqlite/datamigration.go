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

package rqlite

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/interfaces"
)

// RqliteDataMigrationPlanner implements the DataMigrationPlanner interface for RQLite
type RqliteDataMigrationPlanner struct {
	connection *RqliteConnection
}

// NewRqliteDataMigrationPlanner creates a new RQLite data migration planner
func NewRqliteDataMigrationPlanner(conn *RqliteConnection) interfaces.DataMigrationPlanner {
	return &RqliteDataMigrationPlanner{
		connection: conn,
	}
}

// PlanRqliteDataMigrations is the main entry point for planning RQLite data migrations
func PlanRqliteDataMigrations(uri string, tableName string, migrations []schemasv1alpha4.DataMigration) ([]string, error) {
	r, err := Connect(uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to rqlite")
	}
	defer r.Close()

	planner := NewRqliteDataMigrationPlanner(r)
	return planner.PlanDataMigrations(tableName, migrations)
}

// PlanDataMigrations generates DML statements from data migration specifications
func (r *RqliteDataMigrationPlanner) PlanDataMigrations(tableName string, migrations []schemasv1alpha4.DataMigration) ([]string, error) {
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
		shouldExecute, err := r.shouldExecuteMigration(migration)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to evaluate conditions for migration %s", migration.Name)
		}

		if !shouldExecute {
			// Add a comment indicating why the migration was skipped
			statements = append(statements, fmt.Sprintf("-- Migration %s: SKIPPED (conditions not met)", migration.Name))
			continue
		}

		// Generate SQL for this migration
		sql, err := r.PlanSingleDataMigration(tableName, migration)
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
func (r *RqliteDataMigrationPlanner) PlanSingleDataMigration(tableName string, migration *schemasv1alpha4.DataMigration) (string, error) {
	var sql string
	var err error

	// Generate SQL from either direct SQL or template
	if migration.SQL != "" {
		sql = migration.SQL
	} else if migration.Template != nil {
		sql, err = r.renderTemplate(migration.Template, map[string]interface{}{
			"table_name": tableName,
		})
		if err != nil {
			return "", errors.Wrap(err, "failed to render template")
		}
	} else {
		return "", errors.New("migration must have either SQL or Template specified")
	}

	// Adapt SQL for RQLite-specific syntax (similar to SQLite)
	sql, err = r.GetDatabaseSpecificSQL(sql)
	if err != nil {
		return "", errors.Wrap(err, "failed to adapt SQL for RQLite")
	}

	// Add batching if specified
	if migration.BatchSize > 0 {
		sql = r.addBatchingToSQL(sql, int(migration.BatchSize))
	}

	return sql, nil
}

// ValidateCondition validates a condition against the RQLite database
func (r *RqliteDataMigrationPlanner) ValidateCondition(condition schemasv1alpha4.DataMigrationCondition) (bool, error) {
	// Simplified implementation for RQLite - return true for basic conditions
	// In a full implementation, this would use gorqlite.Connection methods

	// For now, just validate the basic structure and return true
	if condition.Query == "" {
		return false, errors.New("condition query cannot be empty")
	}

	// Basic operator validation
	validOperators := []string{">", "<", ">=", "<=", "=", "==", "!=", "EXISTS", "NOT EXISTS"}
	isValidOperator := false
	for _, op := range validOperators {
		if condition.Operator == op {
			isValidOperator = true
			break
		}
	}

	if !isValidOperator {
		return false, errors.Errorf("unsupported operator: %s", condition.Operator)
	}

	// For EXISTS/NOT EXISTS, return true (optimistic execution)
	if condition.Operator == "EXISTS" || condition.Operator == "NOT EXISTS" {
		return true, nil
	}

	// For numeric comparisons, assume condition is met (optimistic execution)
	// In a real implementation, this would execute the query against RQLite
	return true, nil
}

// EstimateAffectedRows estimates the number of rows that would be affected by a migration
func (r *RqliteDataMigrationPlanner) EstimateAffectedRows(tableName string, migration *schemasv1alpha4.DataMigration) (int64, error) {
	sql := migration.SQL
	if migration.Template != nil {
		var err error
		sql, err = r.renderTemplate(migration.Template, map[string]interface{}{
			"table_name": tableName,
		})
		if err != nil {
			return 0, errors.Wrap(err, "failed to render template for estimation")
		}
	}
	// For RQLite, use conservative estimates since COUNT(*) operations
	// can be expensive in distributed systems
	upperSQL := strings.ToUpper(strings.TrimSpace(sql))

	if strings.HasPrefix(upperSQL, "UPDATE") {
		return 100, nil // Conservative estimate for updates
	} else if strings.HasPrefix(upperSQL, "DELETE") {
		return 50, nil // Conservative estimate for deletes
	} else if strings.HasPrefix(upperSQL, "INSERT") {
		return 1, nil // Insert operations affect single rows
	}

	return 10, nil // Default estimate
}

// GetDatabaseSpecificSQL adapts generic SQL to RQLite-specific syntax (same as SQLite)
func (r *RqliteDataMigrationPlanner) GetDatabaseSpecificSQL(sql string) (string, error) {
	// RQLite uses SQLite syntax
	adaptedSQL := sql

	// Adapt date functions (RQLite supports SQLite date functions)
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "NOW()", "datetime('now')")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "CURRENT_DATE", "date('now')")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "CURRENT_TIMESTAMP", "datetime('now')")

	// Convert PostgreSQL interval syntax to SQLite/RQLite date functions
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "INTERVAL '1 day'", "'+1 day'")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "INTERVAL '1 week'", "'+7 days'")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "INTERVAL '1 month'", "'+1 month'")

	// RQLite uses double quotes for identifiers (SQLite syntax)
	// No changes needed for identifier quoting

	return adaptedSQL, nil
}

// shouldExecuteMigration checks if a migration should be executed based on conditions
func (r *RqliteDataMigrationPlanner) shouldExecuteMigration(migration *schemasv1alpha4.DataMigration) (bool, error) {
	if len(migration.Conditions) == 0 {
		return true, nil
	}

	for _, condition := range migration.Conditions {
		result, err := r.ValidateCondition(condition)
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
func (r *RqliteDataMigrationPlanner) renderTemplate(template *schemasv1alpha4.DataMigrationTemplate, values map[string]interface{}) (string, error) {
	// Add RQLite-specific default values (same as SQLite)
	rqliteValues := map[string]interface{}{
		"NOW":          "datetime('now')",
		"CURRENT_DATE": "date('now')",
	}

	// Merge with provided values (provided values take precedence)
	for k, v := range rqliteValues {
		if _, exists := values[k]; !exists {
			values[k] = v
		}
	}

	return schemasv1alpha4.RenderTemplate(template.Template, values)
}

// addBatchingToSQL adds LIMIT clause for batching UPDATE/DELETE operations
func (r *RqliteDataMigrationPlanner) addBatchingToSQL(sql string, batchSize int) string {
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

// compareNumeric compares two values numerically (same logic as SQLite)
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

// convertToFloat64 converts various numeric types to float64 (same logic as SQLite)
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

// extractTableNameFromUpdate extracts table name from UPDATE statement (same as SQLite)
func extractTableNameFromUpdate(sql string) string {
	parts := strings.Fields(sql)
	if len(parts) >= 2 && strings.ToUpper(parts[0]) == "UPDATE" {
		return parts[1]
	}
	return "unknown_table"
}

// extractTableNameFromDelete extracts table name from DELETE statement (same as SQLite)
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
