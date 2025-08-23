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

package cassandra

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gocql/gocql"
	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/interfaces"
)

// CassandraDataMigrationPlanner implements the DataMigrationPlanner interface for Cassandra
// Note: Cassandra support is limited due to NoSQL nature and CQL constraints
type CassandraDataMigrationPlanner struct {
	hosts    []string
	username string
	password string
	keyspace string
}

// NewCassandraDataMigrationPlanner creates a new Cassandra data migration planner
func NewCassandraDataMigrationPlanner(hosts []string, username, password, keyspace string) interfaces.DataMigrationPlanner {
	return &CassandraDataMigrationPlanner{
		hosts:    hosts,
		username: username,
		password: password,
		keyspace: keyspace,
	}
}

// PlanCassandraDataMigrations is the main entry point for planning Cassandra data migrations
func PlanCassandraDataMigrations(hosts []string, username, password, keyspace, tableName string, migrations []schemasv1alpha4.DataMigration) ([]string, error) {
	planner := NewCassandraDataMigrationPlanner(hosts, username, password, keyspace)
	return planner.PlanDataMigrations(tableName, migrations)
}

// PlanDataMigrations generates CQL statements from data migration specifications
func (c *CassandraDataMigrationPlanner) PlanDataMigrations(tableName string, migrations []schemasv1alpha4.DataMigration) ([]string, error) {
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
		// Validate that the migration is suitable for Cassandra
		if err := c.validateCassandraMigration(migration); err != nil {
			return nil, errors.Wrapf(err, "migration %s is not suitable for Cassandra", migration.Name)
		}

		// Check conditions before generating CQL (limited support)
		shouldExecute, err := c.shouldExecuteMigration(migration)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to evaluate conditions for migration %s", migration.Name)
		}

		if !shouldExecute {
			// Add a comment indicating why the migration was skipped
			statements = append(statements, fmt.Sprintf("-- Migration %s: SKIPPED (conditions not met)", migration.Name))
			continue
		}

		// Generate CQL for this migration
		cql, err := c.PlanSingleDataMigration(tableName, migration)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to plan migration %s", migration.Name)
		}

		// Add comment with migration info
		statements = append(statements, fmt.Sprintf("-- Migration: %s", migration.Name))
		if migration.Description != "" {
			statements = append(statements, fmt.Sprintf("-- Description: %s", migration.Description))
		}
		statements = append(statements, cql)
		statements = append(statements, "") // Empty line for readability
	}

	return statements, nil
}

// PlanSingleDataMigration generates CQL statements for a single data migration
func (c *CassandraDataMigrationPlanner) PlanSingleDataMigration(tableName string, migration *schemasv1alpha4.DataMigration) (string, error) {
	var cql string
	var err error

	// Generate CQL from either direct CQL or template
	if migration.SQL != "" {
		cql = migration.SQL
	} else if migration.Template != nil {
		cql, err = c.renderTemplate(migration.Template, map[string]interface{}{
			"table_name": tableName,
			"keyspace":   c.keyspace,
		})
		if err != nil {
			return "", errors.Wrap(err, "failed to render template")
		}
	} else {
		return "", errors.New("migration must have either SQL or Template specified")
	}

	// Adapt CQL for Cassandra-specific syntax
	cql, err = c.GetDatabaseSpecificSQL(cql)
	if err != nil {
		return "", errors.Wrap(err, "failed to adapt CQL for Cassandra")
	}

	// Note: Cassandra doesn't support traditional batching with LIMIT
	// Batching must be handled at the application level with pagination
	if migration.BatchSize > 0 {
		cql = c.addBatchingToCQL(cql, int(migration.BatchSize))
	}

	return cql, nil
}

// ValidateCondition validates a condition against the Cassandra database
// Note: Limited support due to Cassandra's NoSQL nature
func (c *CassandraDataMigrationPlanner) ValidateCondition(condition schemasv1alpha4.DataMigrationCondition) (bool, error) {
	// Cassandra has limited condition support due to NoSQL constraints
	// Only support simple existence checks and basic comparisons

	cluster := gocql.NewCluster(c.hosts...)
	cluster.Authenticator = gocql.PasswordAuthenticator{
		Username: c.username,
		Password: c.password,
	}
	cluster.Keyspace = c.keyspace

	session, err := cluster.CreateSession()
	if err != nil {
		return false, errors.Wrap(err, "failed to create Cassandra session")
	}
	defer session.Close()

	// Execute the condition query
	var result interface{}
	if err := session.Query(condition.Query).Scan(&result); err != nil {
		return false, errors.Wrapf(err, "failed to execute condition query: %s", condition.Query)
	}

	// Convert result to comparable value
	var actualValue interface{}
	switch v := result.(type) {
	case int64:
		actualValue = v
	case int:
		actualValue = int64(v)
	case float64:
		actualValue = v
	case string:
		actualValue = v
	case bool:
		actualValue = v
	default:
		actualValue = fmt.Sprintf("%v", v)
	}

	// Expected value is already int64 from the condition
	expectedValue := condition.Value

	// Limited operator support for Cassandra
	switch condition.Operator {
	case "=", "==":
		return fmt.Sprintf("%v", actualValue) == fmt.Sprintf("%v", expectedValue), nil
	case "!=":
		return fmt.Sprintf("%v", actualValue) != fmt.Sprintf("%v", expectedValue), nil
	case ">", "<", ">=", "<=":
		return compareNumericCassandra(actualValue, expectedValue, condition.Operator)
	default:
		return false, errors.Errorf("unsupported operator for Cassandra: %s", condition.Operator)
	}
}

// EstimateAffectedRows provides a basic estimate for Cassandra operations
// Note: Limited accuracy due to Cassandra's distributed nature
func (c *CassandraDataMigrationPlanner) EstimateAffectedRows(tableName string, migration *schemasv1alpha4.DataMigration) (int64, error) {
	cql := migration.SQL
	if migration.Template != nil {
		var err error
		cql, err = c.renderTemplate(migration.Template, map[string]interface{}{
			"table_name": tableName,
			"keyspace":   c.keyspace,
		})
		if err != nil {
			return 0, errors.Wrap(err, "failed to render template for estimation")
		}
	}
	// Cassandra doesn't support traditional COUNT(*) operations well
	// Return conservative estimates based on operation type
	upperCQL := strings.ToUpper(strings.TrimSpace(cql))

	if strings.HasPrefix(upperCQL, "UPDATE") {
		// Conservative estimate for UPDATE operations
		return 100, nil
	} else if strings.HasPrefix(upperCQL, "DELETE") {
		// Conservative estimate for DELETE operations
		return 50, nil
	} else if strings.HasPrefix(upperCQL, "INSERT") {
		// INSERT operations typically affect 1 row
		return 1, nil
	}

	// Default estimate for other operations
	return 10, nil
}

// GetDatabaseSpecificSQL adapts generic SQL to Cassandra CQL syntax
func (c *CassandraDataMigrationPlanner) GetDatabaseSpecificSQL(sql string) (string, error) {
	// Cassandra CQL adaptations
	adaptedCQL := sql

	// Cassandra uses different date/time functions
	adaptedCQL = strings.ReplaceAll(adaptedCQL, "NOW()", "toTimestamp(now())")
	adaptedCQL = strings.ReplaceAll(adaptedCQL, "CURRENT_DATE", "toDate(now())")
	adaptedCQL = strings.ReplaceAll(adaptedCQL, "CURRENT_TIMESTAMP", "toTimestamp(now())")

	// Cassandra doesn't support PostgreSQL interval syntax
	// Remove interval operations as they're not easily translatable
	if strings.Contains(adaptedCQL, "INTERVAL") {
		// This is a limitation - Cassandra doesn't have equivalent interval arithmetic
		// Log warning and remove interval operations
		adaptedCQL = strings.ReplaceAll(adaptedCQL, "+ INTERVAL '1 day'", "")
		adaptedCQL = strings.ReplaceAll(adaptedCQL, "- INTERVAL '1 day'", "")
	}

	// Cassandra uses different identifier quoting (not needed usually)
	// No changes needed for identifier quoting in most cases

	return adaptedCQL, nil
}

// validateCassandraMigration checks if a migration is suitable for Cassandra
func (c *CassandraDataMigrationPlanner) validateCassandraMigration(migration *schemasv1alpha4.DataMigration) error {
	upperSQL := strings.ToUpper(strings.TrimSpace(migration.SQL))

	// Cassandra limitations
	forbiddenOperations := []string{
		"JOIN",
		"SUBQUERY",
		"UNION",
		"GROUP BY",
		"HAVING",
		"ORDER BY", // Limited support
	}

	for _, forbidden := range forbiddenOperations {
		if strings.Contains(upperSQL, forbidden) {
			return errors.Errorf("operation '%s' is not supported in Cassandra", forbidden)
		}
	}

	// Warn about potentially problematic operations
	if strings.Contains(upperSQL, "UPDATE") && !strings.Contains(upperSQL, "WHERE") {
		return errors.New("mass UPDATE operations are not recommended in Cassandra")
	}

	if strings.Contains(upperSQL, "DELETE") && !strings.Contains(upperSQL, "WHERE") {
		return errors.New("mass DELETE operations are not supported in Cassandra")
	}

	return nil
}

// shouldExecuteMigration checks if a migration should be executed based on conditions
func (c *CassandraDataMigrationPlanner) shouldExecuteMigration(migration *schemasv1alpha4.DataMigration) (bool, error) {
	if len(migration.Conditions) == 0 {
		return true, nil
	}

	// Cassandra has limited condition support
	for _, condition := range migration.Conditions {
		result, err := c.ValidateCondition(condition)
		if err != nil {
			// Return the actual connection error instead of ignoring it
			return false, errors.Wrapf(err, "failed to validate condition for migration %s", migration.Name)
		}
		if !result {
			return false, nil
		}
	}

	return true, nil
}

// renderTemplate renders a migration template with provided values
func (c *CassandraDataMigrationPlanner) renderTemplate(template *schemasv1alpha4.DataMigrationTemplate, values map[string]interface{}) (string, error) {
	// Add Cassandra-specific default values
	cassandraValues := map[string]interface{}{
		"NOW":          "toTimestamp(now())",
		"CURRENT_DATE": "toDate(now())",
		"KEYSPACE":     c.keyspace,
	}

	// Merge with provided values (provided values take precedence)
	for k, v := range cassandraValues {
		if _, exists := values[k]; !exists {
			values[k] = v
		}
	}

	return schemasv1alpha4.RenderTemplate(template.Template, values)
}

// addBatchingToCQL adds batch processing hints for Cassandra operations
// Note: Cassandra batching is different from SQL LIMIT clauses
func (c *CassandraDataMigrationPlanner) addBatchingToCQL(cql string, batchSize int) string {
	// Cassandra doesn't support LIMIT in UPDATE/DELETE like SQL databases
	// Instead, add a comment indicating the intended batch size for application-level handling
	return fmt.Sprintf("-- BATCH_SIZE: %d\n%s", batchSize, cql)
}

// compareNumericCassandra compares two values numerically for Cassandra
func compareNumericCassandra(actual, expected interface{}, operator string) (bool, error) {
	actualFloat, err := convertToFloat64Cassandra(actual)
	if err != nil {
		return false, errors.Wrapf(err, "failed to convert actual value to number: %v", actual)
	}

	expectedFloat, err := convertToFloat64Cassandra(expected)
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
		return false, errors.Errorf("unsupported numeric operator for Cassandra: %s", operator)
	}
}

// convertToFloat64Cassandra converts various numeric types to float64 for Cassandra
func convertToFloat64Cassandra(value interface{}) (float64, error) {
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
		return 0, errors.Errorf("unsupported value type for Cassandra: %T", value)
	}
}
