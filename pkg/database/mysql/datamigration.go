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
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/interfaces"
)

// MysqlDataMigrationPlanner implements the DataMigrationPlanner interface for MySQL
type MysqlDataMigrationPlanner struct {
	connection *MysqlConnection
}

// NewMysqlDataMigrationPlanner creates a new MySQL data migration planner
func NewMysqlDataMigrationPlanner(conn *MysqlConnection) interfaces.DataMigrationPlanner {
	return &MysqlDataMigrationPlanner{
		connection: conn,
	}
}

// PlanMysqlDataMigrations is the main entry point for planning MySQL data migrations
func PlanMysqlDataMigrations(uri string, tableName string, migrations []schemasv1alpha4.DataMigration) ([]string, error) {
	conn, err := Connect(uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to mysql")
	}
	defer conn.Close()

	planner := NewMysqlDataMigrationPlanner(conn)
	return planner.PlanDataMigrations(tableName, migrations)
}

// PlanDataMigrations generates DML statements from data migration specifications
func (m *MysqlDataMigrationPlanner) PlanDataMigrations(tableName string, migrations []schemasv1alpha4.DataMigration) ([]string, error) {
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
		shouldExecute, err := m.shouldExecuteMigration(migration)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to evaluate conditions for migration %s", migration.Name)
		}

		if !shouldExecute {
			// Add a comment indicating why the migration was skipped
			statements = append(statements, fmt.Sprintf("-- Migration %s: SKIPPED (conditions not met)", migration.Name))
			continue
		}

		// Generate SQL for this migration
		sql, err := m.PlanSingleDataMigration(tableName, migration)
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
func (m *MysqlDataMigrationPlanner) PlanSingleDataMigration(tableName string, migration *schemasv1alpha4.DataMigration) (string, error) {
	var sql string
	var err error

	// Generate SQL from either direct SQL or template
	if migration.SQL != "" {
		sql = migration.SQL
	} else if migration.Template != nil {
		sql, err = m.renderTemplate(migration.Template)
		if err != nil {
			return "", errors.Wrap(err, "failed to render template")
		}
	} else {
		return "", fmt.Errorf("migration must have either sql or template")
	}

	// Adapt SQL for MySQL specifics
	sql, err = m.GetDatabaseSpecificSQL(sql)
	if err != nil {
		return "", errors.Wrap(err, "failed to adapt SQL for MySQL")
	}

	// Add batching if specified
	if migration.BatchSize > 0 {
		sql = m.addBatchingToSQL(sql, migration.BatchSize)
	}

	return sql, nil
}

// ValidateCondition evaluates whether a migration condition is met
func (m *MysqlDataMigrationPlanner) ValidateCondition(condition schemasv1alpha4.DataMigrationCondition) (bool, error) {
	// Execute the condition query
	var result interface{}
	err := m.connection.GetDB().QueryRowContext(context.Background(), condition.Query).Scan(&result)
	if err != nil {
		// For EXISTS/NOT EXISTS, no rows is a valid result
		if condition.Operator == "EXISTS" {
			return false, nil
		} else if condition.Operator == "NOT EXISTS" {
			return true, nil
		}
		return false, errors.Wrap(err, "failed to execute condition query")
	}

	// Handle EXISTS operators
	if condition.Operator == "EXISTS" {
		return true, nil
	} else if condition.Operator == "NOT EXISTS" {
		return false, nil
	}

	// Convert result to int64 for comparison
	var numericResult int64
	switch v := result.(type) {
	case int64:
		numericResult = v
	case int32:
		numericResult = int64(v)
	case int:
		numericResult = int64(v)
	case []uint8: // MySQL often returns numbers as byte arrays
		str := string(v)
		numericResult, err = strconv.ParseInt(str, 10, 64)
		if err != nil {
			return false, errors.Wrap(err, "condition query result is not numeric")
		}
	case string:
		numericResult, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return false, errors.Wrap(err, "condition query result is not numeric")
		}
	default:
		return false, fmt.Errorf("condition query result type %T is not supported", result)
	}

	// Evaluate the condition
	switch condition.Operator {
	case ">":
		return numericResult > condition.Value, nil
	case "<":
		return numericResult < condition.Value, nil
	case ">=":
		return numericResult >= condition.Value, nil
	case "<=":
		return numericResult <= condition.Value, nil
	case "=":
		return numericResult == condition.Value, nil
	case "!=":
		return numericResult != condition.Value, nil
	default:
		return false, fmt.Errorf("unsupported operator: %s", condition.Operator)
	}
}

// EstimateAffectedRows estimates how many rows a migration will affect
func (m *MysqlDataMigrationPlanner) EstimateAffectedRows(tableName string, migration *schemasv1alpha4.DataMigration) (int64, error) {
	// For UPDATE statements, try to estimate by converting to SELECT COUNT(*)
	sql := migration.SQL
	if migration.Template != nil {
		var err error
		sql, err = m.renderTemplate(migration.Template)
		if err != nil {
			return 0, errors.Wrap(err, "failed to render template for estimation")
		}
	}

	// Simple heuristic: convert UPDATE/DELETE to SELECT COUNT(*)
	sql = strings.TrimSpace(sql)
	if strings.HasPrefix(strings.ToUpper(sql), "UPDATE") {
		// Extract WHERE clause from UPDATE statement
		whereIdx := strings.Index(strings.ToUpper(sql), "WHERE")
		if whereIdx != -1 {
			whereClause := sql[whereIdx:]
			estimateQuery := fmt.Sprintf("SELECT COUNT(*) FROM `%s` %s", tableName, whereClause)
			
			var count int64
			err := m.connection.GetDB().QueryRowContext(context.Background(), estimateQuery).Scan(&count)
			if err != nil {
				return 0, errors.Wrap(err, "failed to estimate affected rows")
			}
			return count, nil
		}
	}

	// For other cases, return a conservative estimate
	var tableRowCount int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM `%s`", tableName)
	err := m.connection.GetDB().QueryRowContext(context.Background(), countQuery).Scan(&tableRowCount)
	if err != nil {
		return 0, errors.Wrap(err, "failed to get table row count")
	}

	// Assume 10% of rows might be affected for non-UPDATE statements
	return tableRowCount / 10, nil
}

// GetDatabaseSpecificSQL adapts generic SQL for MySQL
func (m *MysqlDataMigrationPlanner) GetDatabaseSpecificSQL(genericSQL string) (string, error) {
	// MySQL-specific adaptations
	sql := genericSQL

	// Replace PostgreSQL-specific syntax with MySQL equivalents
	sql = strings.ReplaceAll(sql, "||", "CONCAT(")  // PostgreSQL concatenation to MySQL
	
	// Handle PostgreSQL timestamp functions
	sql = strings.ReplaceAll(sql, "NOW()", "NOW()")
	sql = strings.ReplaceAll(sql, "CURRENT_DATE", "CURDATE()")
	sql = strings.ReplaceAll(sql, "CURRENT_TIMESTAMP", "NOW()")
	
	// Handle PostgreSQL interval syntax - more comprehensive replacement
	sql = strings.ReplaceAll(sql, "INTERVAL '30 days'", "INTERVAL 30 DAY")
	sql = strings.ReplaceAll(sql, "INTERVAL '1 year'", "INTERVAL 1 YEAR")
	sql = strings.ReplaceAll(sql, "INTERVAL '1 month'", "INTERVAL 1 MONTH")
	
	// Generic interval replacements
	sql = strings.ReplaceAll(sql, "INTERVAL '", "INTERVAL ")
	sql = strings.ReplaceAll(sql, "' days'", " DAY")
	sql = strings.ReplaceAll(sql, "' YEAR", " YEAR")
	sql = strings.ReplaceAll(sql, "' MONTH", " MONTH")
	sql = strings.ReplaceAll(sql, "' DAY", " DAY")
	
	// MySQL uses backticks for identifiers instead of double quotes
	sql = strings.ReplaceAll(sql, `"`, "`")

	return sql, nil
}

// shouldExecuteMigration checks if a migration should be executed based on conditions
func (m *MysqlDataMigrationPlanner) shouldExecuteMigration(migration *schemasv1alpha4.DataMigration) (bool, error) {
	// If no conditions, always execute
	if len(migration.Conditions) == 0 {
		return true, nil
	}

	// All conditions must be true
	for _, condition := range migration.Conditions {
		result, err := m.ValidateCondition(condition)
		if err != nil {
			return false, errors.Wrapf(err, "failed to validate condition: %s", condition.Query)
		}
		if !result {
			return false, nil // Condition not met
		}
	}

	return true, nil
}

// renderTemplate renders a data migration template with default values
func (m *MysqlDataMigrationPlanner) renderTemplate(template *schemasv1alpha4.DataMigrationTemplate) (string, error) {
	// Build default values from parameters
	values := make(map[string]interface{})
	for _, param := range template.Parameters {
		if param.Default != "" {
			switch param.Type {
			case schemasv1alpha4.ParameterTypeInteger:
				if val, err := strconv.Atoi(param.Default); err == nil {
					values[param.Name] = val
				}
			case schemasv1alpha4.ParameterTypeBoolean:
				if val, err := strconv.ParseBool(param.Default); err == nil {
					values[param.Name] = val
				}
			default:
				values[param.Name] = param.Default
			}
		}
	}

	// Add MySQL-specific template values
	values["currentTimestamp"] = "NOW()"
	values["currentDate"] = "CURDATE()"
	values["databaseType"] = "mysql"

	return schemasv1alpha4.RenderTemplate(template.Template, values)
}

// addBatchingToSQL modifies SQL to support batching for large operations
func (m *MysqlDataMigrationPlanner) addBatchingToSQL(sql string, batchSize int32) string {
	// For UPDATE/DELETE statements, add LIMIT clause if not present
	sql = strings.TrimSpace(sql)
	upperSQL := strings.ToUpper(sql)

	if (strings.HasPrefix(upperSQL, "UPDATE") || strings.HasPrefix(upperSQL, "DELETE")) &&
		!strings.Contains(upperSQL, "LIMIT") {
		// MySQL syntax for LIMIT
		sql = fmt.Sprintf("%s LIMIT %d", sql, batchSize)
	}

	return sql
} 