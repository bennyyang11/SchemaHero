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

package migration

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// EnhancedDataMigrationValidator provides comprehensive validation for data migrations
type EnhancedDataMigrationValidator struct {
	allowedOperations   map[string]OperationSafety
	dangerousPatterns   []*regexp.Regexp
	requiredApprovals   map[string]bool
	databaseType        string
	resourceThresholds  ResourceThresholds
}

// OperationSafety defines the safety level of SQL operations
type OperationSafety int

const (
	OperationSafe       OperationSafety = iota // Safe to execute automatically
	OperationCaution                           // Requires validation but can proceed
	OperationDangerous                         // Requires explicit approval
	OperationForbidden                         // Never allowed in data migrations
)

// ResourceThresholds defines limits for resource usage estimation
type ResourceThresholds struct {
	MaxEstimatedRows    int64         // Maximum rows that can be affected
	MaxExecutionTime    time.Duration // Maximum estimated execution time
	MaxBatchSize        int32         // Maximum batch size
	WarnAtRows          int64         // Warn when operations affect this many rows
	WarnAtExecutionTime time.Duration // Warn when operations take longer than this
}

// ValidationResult contains the result of migration validation
type ValidationResult struct {
	IsValid      bool
	Errors       []ValidationError
	Warnings     []ValidationWarning
	EstimatedRows int64
	EstimatedDuration time.Duration
	RequiresApproval  bool
}

// ValidationError represents a validation error that prevents execution
type ValidationError struct {
	Field   string
	Message string
	Code    string
}

// ValidationWarning represents a validation warning that should be reviewed
type ValidationWarning struct {
	Field   string
	Message string
	Code    string
}

// NewEnhancedDataMigrationValidator creates a comprehensive validator
func NewEnhancedDataMigrationValidator(databaseType string) *EnhancedDataMigrationValidator {
	return &EnhancedDataMigrationValidator{
		databaseType: databaseType,
		allowedOperations: map[string]OperationSafety{
			// Safe operations
			"SELECT":   OperationSafe,
			"UPDATE":   OperationCaution, // Safe with WHERE clause
			"INSERT":   OperationCaution, // Generally safe
			
			// Dangerous operations  
			"DELETE":   OperationDangerous, // Can lose data
			"TRUNCATE": OperationForbidden, // Dangerous, use DELETE instead
			"DROP":     OperationForbidden, // Should not be in data migrations
			"ALTER":    OperationForbidden, // Should be in schema migrations
			"CREATE":   OperationForbidden, // Should be in schema migrations
		},
		dangerousPatterns: []*regexp.Regexp{
			// Patterns that should trigger warnings or errors
			regexp.MustCompile(`(?i)\bDROP\s+(TABLE|DATABASE|SCHEMA|INDEX|VIEW)\b`),
			regexp.MustCompile(`(?i)\bTRUNCATE\s+TABLE\b`),
			regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+\w+\s*;?\s*$`), // DELETE without WHERE
			regexp.MustCompile(`(?i)\bUPDATE\s+\w+\s+SET\s+.*\s*;?\s*$`), // UPDATE without WHERE
			regexp.MustCompile(`(?i)\bALTER\s+TABLE\b`), // Schema changes in data migration
			regexp.MustCompile(`(?i)\bCREATE\s+(TABLE|INDEX|VIEW)\b`), // Schema creation in data migration
			regexp.MustCompile(`(?i)\b(EXEC|EXECUTE)\s+`), // Dynamic SQL execution
			regexp.MustCompile(`(?i)\bSELECT\s+.*\bINTO\s+OUTFILE\b`), // File operations
			regexp.MustCompile(`(?i)\bLOAD\s+DATA\b`), // File loading
		},
		requiredApprovals: map[string]bool{
			"DELETE":             true,
			"MASS_UPDATE":        true,
			"CROSS_TABLE_UPDATE": true,
			"BULK_INSERT":        true,
		},
		resourceThresholds: ResourceThresholds{
			MaxEstimatedRows:     10000000,                // 10M rows
			MaxExecutionTime:     time.Hour * 4,           // 4 hours
			MaxBatchSize:         100000,                  // 100K per batch
			WarnAtRows:           1000000,                 // Warn at 1M rows
			WarnAtExecutionTime:  time.Hour,               // Warn at 1 hour
		},
	}
}

// ValidateDataMigration performs comprehensive validation on a single migration
func (v *EnhancedDataMigrationValidator) ValidateDataMigration(migration *schemasv1alpha4.DataMigration) (*ValidationResult, error) {
	result := &ValidationResult{
		IsValid:  true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}

	// Basic field validation
	if err := v.validateBasicFields(migration, result); err != nil {
		return result, err
	}

	// SQL safety validation
	if err := v.validateSQLSafety(migration, result); err != nil {
		return result, err
	}

	// Resource usage estimation
	if err := v.estimateResourceUsage(migration, result); err != nil {
		return result, err
	}

	// Database-specific validation
	if err := v.validateDatabaseSpecific(migration, result); err != nil {
		return result, err
	}

	// Batch configuration validation
	if err := v.validateBatchConfiguration(migration, result); err != nil {
		return result, err
	}

	// Set final validation status
	result.IsValid = len(result.Errors) == 0

	return result, nil
}

// validateBasicFields validates basic migration fields
func (v *EnhancedDataMigrationValidator) validateBasicFields(migration *schemasv1alpha4.DataMigration, result *ValidationResult) error {
	// Name validation
	if migration.Name == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "name",
			Message: "Migration name is required",
			Code:    "MISSING_NAME",
		})
	} else if !regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(migration.Name) {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "name",
			Message: "Migration name must consist of lowercase alphanumeric characters or '-'",
			Code:    "INVALID_NAME_FORMAT",
		})
	} else if len(migration.Name) > 63 {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "name",
			Message: "Migration name must be no more than 63 characters",
			Code:    "NAME_TOO_LONG",
		})
	}

	// SQL or template validation
	if migration.SQL == "" && migration.Template == nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "sql/template",
			Message: "Either sql or template must be provided",
			Code:    "MISSING_SQL_OR_TEMPLATE",
		})
	}

	if migration.SQL != "" && migration.Template != nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "sql/template",
			Message: "Only one of sql or template should be provided",
			Code:    "CONFLICTING_SQL_AND_TEMPLATE",
		})
	}

	// Reversible validation
	if migration.Reversible && migration.ReverseSQL == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "reverseSQL",
			Message: "reverseSQL must be provided when reversible is true",
			Code:    "MISSING_REVERSE_SQL",
		})
	}

	return nil
}

// validateSQLSafety validates SQL for dangerous operations
func (v *EnhancedDataMigrationValidator) validateSQLSafety(migration *schemasv1alpha4.DataMigration, result *ValidationResult) error {
	sql := migration.SQL
	if sql == "" && migration.Template != nil {
		// Try to render template for validation
		testValues := v.generateTestValues(migration.Template.Parameters)
		rendered, err := schemasv1alpha4.RenderTemplate(migration.Template.Template, testValues)
		if err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "template",
				Message: fmt.Sprintf("Template rendering failed: %v", err),
				Code:    "TEMPLATE_RENDER_ERROR",
			})
			return nil
		}
		sql = rendered
	}

	if sql == "" {
		return nil // Already handled in basic validation
	}

	// Check for dangerous patterns
	for _, pattern := range v.dangerousPatterns {
		if pattern.MatchString(sql) {
			// Determine severity based on pattern
			if strings.Contains(pattern.String(), "DROP|TRUNCATE") {
				result.Errors = append(result.Errors, ValidationError{
					Field:   "sql",
					Message: fmt.Sprintf("Forbidden operation detected: %s", pattern.String()),
					Code:    "FORBIDDEN_OPERATION",
				})
			} else if strings.Contains(pattern.String(), "DELETE.*FROM.*$|UPDATE.*SET.*$") {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "sql", 
					Message: "Potentially dangerous operation without WHERE clause detected",
					Code:    "MISSING_WHERE_CLAUSE",
				})
			} else {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "sql",
					Message: fmt.Sprintf("Potentially risky pattern: %s", pattern.String()),
					Code:    "RISKY_PATTERN",
				})
			}
		}
	}

	// Validate operation types
	upperSQL := strings.ToUpper(strings.TrimSpace(sql))
	for operation, safety := range v.allowedOperations {
		if strings.HasPrefix(upperSQL, operation) {
			switch safety {
			case OperationForbidden:
				result.Errors = append(result.Errors, ValidationError{
					Field:   "sql",
					Message: fmt.Sprintf("Operation %s is not allowed in data migrations", operation),
					Code:    "FORBIDDEN_OPERATION_TYPE",
				})
			case OperationDangerous:
				result.RequiresApproval = true
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "sql",
					Message: fmt.Sprintf("Operation %s requires explicit approval", operation),
					Code:    "REQUIRES_APPROVAL",
				})
			case OperationCaution:
				// Check for WHERE clause on UPDATE/DELETE
				if (operation == "UPDATE" || operation == "DELETE") && !regexp.MustCompile(`(?i)\bWHERE\b`).MatchString(sql) {
					result.Warnings = append(result.Warnings, ValidationWarning{
						Field:   "sql",
						Message: fmt.Sprintf("%s operation without WHERE clause will affect all rows", operation),
						Code:    "MISSING_WHERE_CLAUSE",
					})
				}
			}
			break
		}
	}

	// Check for SQL injection patterns
	if err := v.checkSQLInjectionPatterns(sql, result); err != nil {
		return err
	}

	return nil
}

// validateDatabaseSpecific performs database-specific validation
func (v *EnhancedDataMigrationValidator) validateDatabaseSpecific(migration *schemasv1alpha4.DataMigration, result *ValidationResult) error {
	sql := migration.SQL
	if sql == "" && migration.Template != nil {
		testValues := v.generateTestValues(migration.Template.Parameters)
		rendered, err := schemasv1alpha4.RenderTemplate(migration.Template.Template, testValues)
		if err != nil {
			return nil // Already handled in SQL safety validation
		}
		sql = rendered
	}

	if sql == "" {
		return nil
	}

	// Database-specific syntax validation
	switch v.databaseType {
	case "postgres", "cockroachdb", "timescaledb":
		return v.validatePostgreSQL(sql, result)
	case "mysql":
		return v.validateMySQL(sql, result)
	case "sqlite":
		return v.validateSQLite(sql, result)
	default:
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "sql",
			Message: fmt.Sprintf("Database-specific validation not available for %s", v.databaseType),
			Code:    "UNKNOWN_DATABASE_TYPE",
		})
	}

	return nil
}

// estimateResourceUsage estimates the resource impact of a migration
func (v *EnhancedDataMigrationValidator) estimateResourceUsage(migration *schemasv1alpha4.DataMigration, result *ValidationResult) error {
	sql := migration.SQL
	if sql == "" && migration.Template != nil {
		testValues := v.generateTestValues(migration.Template.Parameters)
		rendered, err := schemasv1alpha4.RenderTemplate(migration.Template.Template, testValues)
		if err != nil {
			return nil
		}
		sql = rendered
	}

	// Estimate affected rows (simplified heuristic)
	estimatedRows := v.estimateAffectedRows(sql)
	result.EstimatedRows = estimatedRows

	// Estimate execution time based on operation type and row count
	result.EstimatedDuration = v.estimateExecutionTime(sql, estimatedRows, migration.BatchSize)

	// Check against thresholds
	if estimatedRows > v.resourceThresholds.MaxEstimatedRows {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "sql",
			Message: fmt.Sprintf("Estimated %d rows exceeds maximum allowed %d", estimatedRows, v.resourceThresholds.MaxEstimatedRows),
			Code:    "EXCEEDS_ROW_LIMIT",
		})
	} else if estimatedRows > v.resourceThresholds.WarnAtRows {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "sql",
			Message: fmt.Sprintf("Migration will affect approximately %d rows - consider using batching", estimatedRows),
			Code:    "HIGH_ROW_COUNT",
		})
	}

	if result.EstimatedDuration > v.resourceThresholds.MaxExecutionTime {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "timeout",
			Message: fmt.Sprintf("Estimated execution time %v exceeds maximum %v", result.EstimatedDuration, v.resourceThresholds.MaxExecutionTime),
			Code:    "EXCEEDS_TIME_LIMIT",
		})
	} else if result.EstimatedDuration > v.resourceThresholds.WarnAtExecutionTime {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "timeout",
			Message: fmt.Sprintf("Estimated execution time %v is long - ensure timeout is set appropriately", result.EstimatedDuration),
			Code:    "LONG_EXECUTION_TIME",
		})
	}

	return nil
}

// validateBatchConfiguration validates batch processing settings
func (v *EnhancedDataMigrationValidator) validateBatchConfiguration(migration *schemasv1alpha4.DataMigration, result *ValidationResult) error {
	if migration.BatchSize < 0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "batchSize",
			Message: "Batch size must be non-negative",
			Code:    "INVALID_BATCH_SIZE",
		})
	}

	if migration.BatchSize > v.resourceThresholds.MaxBatchSize {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "batchSize",
			Message: fmt.Sprintf("Batch size %d exceeds maximum allowed %d", migration.BatchSize, v.resourceThresholds.MaxBatchSize),
			Code:    "BATCH_SIZE_TOO_LARGE",
		})
	}

	if migration.BatchDelayMs < 0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "batchDelayMs",
			Message: "Batch delay must be non-negative",
			Code:    "INVALID_BATCH_DELAY",
		})
	}

	// Recommend batching for large operations
	if result.EstimatedRows > v.resourceThresholds.WarnAtRows && migration.BatchSize == 0 {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "batchSize",
			Message: fmt.Sprintf("Consider using batching for %d estimated rows", result.EstimatedRows),
			Code:    "RECOMMEND_BATCHING",
		})
	}

	return nil
}

// ValidateDataMigrations validates a group of migrations including dependencies
func (v *EnhancedDataMigrationValidator) ValidateDataMigrations(migrations []schemasv1alpha4.DataMigration) (*ValidationResult, error) {
	result := &ValidationResult{
		IsValid:  true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}

	// Check for duplicate names
	names := make(map[string]bool)
	for i, migration := range migrations {
		if names[migration.Name] {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("migrations[%d].name", i),
				Message: fmt.Sprintf("Duplicate migration name: %s", migration.Name),
				Code:    "DUPLICATE_NAME",
			})
		}
		names[migration.Name] = true

		// Validate individual migration
		migrationResult, err := v.ValidateDataMigration(&migration)
		if err != nil {
			return result, errors.Wrapf(err, "failed to validate migration %s", migration.Name)
		}

		// Aggregate results
		result.Errors = append(result.Errors, migrationResult.Errors...)
		result.Warnings = append(result.Warnings, migrationResult.Warnings...)
		result.EstimatedRows += migrationResult.EstimatedRows
		if migrationResult.RequiresApproval {
			result.RequiresApproval = true
		}
	}

	// Validate dependencies
	if err := v.validateDependencies(migrations, result); err != nil {
		return result, err
	}

	result.IsValid = len(result.Errors) == 0
	return result, nil
}

// validateDependencies checks for circular dependencies and missing references
func (v *EnhancedDataMigrationValidator) validateDependencies(migrations []schemasv1alpha4.DataMigration, result *ValidationResult) error {
	resolver := schemasv1alpha4.NewDependencyResolver(migrations)
	
	// Check for dependency validation errors
	if err := resolver.ValidateDependencies(); err != nil {
		if strings.Contains(err.Error(), "circular dependency") {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "dependencies",
				Message: err.Error(),
				Code:    "CIRCULAR_DEPENDENCY",
			})
		} else if strings.Contains(err.Error(), "non-existent migration") {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "dependencies",
				Message: err.Error(),
				Code:    "MISSING_DEPENDENCY",
			})
		} else {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "dependencies",
				Message: err.Error(),
				Code:    "DEPENDENCY_ERROR",
			})
		}
	}

	return nil
}

// Helper functions for specific database validation
func (v *EnhancedDataMigrationValidator) validatePostgreSQL(sql string, result *ValidationResult) error {
	// PostgreSQL-specific checks
	if strings.Contains(sql, "::") && !regexp.MustCompile(`(?i)\b(text|varchar|int|bigint|timestamp)\b::`).MatchString(sql) {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "sql",
			Message: "Custom type casting detected - verify compatibility",
			Code:    "CUSTOM_TYPE_CAST",
		})
	}

	// Check for PostgreSQL-specific functions
	if regexp.MustCompile(`(?i)\bstring_agg|array_agg|jsonb_\w+\b`).MatchString(sql) {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "sql",
			Message: "PostgreSQL-specific functions detected - migration may not be portable",
			Code:    "DATABASE_SPECIFIC_FUNCTION",
		})
	}

	return nil
}

func (v *EnhancedDataMigrationValidator) validateMySQL(sql string, result *ValidationResult) error {
	// MySQL-specific checks
	if strings.Contains(sql, "`") {
		// This is expected for MySQL, just validate proper usage
	}

	// Check for MySQL-specific functions
	if regexp.MustCompile(`(?i)\bGROUP_CONCAT|FIND_IN_SET|SUBSTRING_INDEX\b`).MatchString(sql) {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "sql",
			Message: "MySQL-specific functions detected - migration may not be portable",
			Code:    "DATABASE_SPECIFIC_FUNCTION",
		})
	}

	return nil
}

func (v *EnhancedDataMigrationValidator) validateSQLite(sql string, result *ValidationResult) error {
	// SQLite-specific checks
	if regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+\w+\s+DROP\s+COLUMN\b`).MatchString(sql) {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Field:   "sql",
			Message: "SQLite has limited ALTER TABLE support",
			Code:    "SQLITE_ALTER_LIMITATION",
		})
	}

	return nil
}

// checkSQLInjectionPatterns checks for potential SQL injection patterns
func (v *EnhancedDataMigrationValidator) checkSQLInjectionPatterns(sql string, result *ValidationResult) error {
	injectionPatterns := []*regexp.Regexp{
		regexp.MustCompile(`'.*?'.*?;.*?--`), // String with semicolon and comment
		regexp.MustCompile(`\bunion\s+select\b`), // UNION-based injection
		regexp.MustCompile(`'.*?'\s*;\s*\w+`), // Statement termination
	}

	for _, pattern := range injectionPatterns {
		if pattern.MatchString(sql) {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "sql",
				Message: "Potential SQL injection pattern detected - review carefully",
				Code:    "POTENTIAL_SQL_INJECTION",
			})
		}
	}

	return nil
}

// generateTestValues creates test values for template validation
func (v *EnhancedDataMigrationValidator) generateTestValues(params []schemasv1alpha4.TemplateParameter) map[string]interface{} {
	values := make(map[string]interface{})
	
	for _, param := range params {
		if param.Default != "" {
			switch param.Type {
			case schemasv1alpha4.ParameterTypeInteger:
				if val, err := strconv.Atoi(param.Default); err == nil {
					values[param.Name] = val
				} else {
					values[param.Name] = 1 // Fallback
				}
			case schemasv1alpha4.ParameterTypeBoolean:
				if val, err := strconv.ParseBool(param.Default); err == nil {
					values[param.Name] = val
				} else {
					values[param.Name] = true // Fallback
				}
			default:
				values[param.Name] = param.Default
			}
		} else {
			// Generate safe test values
			switch param.Type {
			case schemasv1alpha4.ParameterTypeString:
				values[param.Name] = "test_value"
			case schemasv1alpha4.ParameterTypeInteger:
				values[param.Name] = 1
			case schemasv1alpha4.ParameterTypeBoolean:
				values[param.Name] = true
			case schemasv1alpha4.ParameterTypeTable:
				values[param.Name] = "test_table"
			case schemasv1alpha4.ParameterTypeColumn:
				values[param.Name] = "test_column"
			default:
				values[param.Name] = "test"
			}
		}
	}

	return values
}

// estimateAffectedRows provides a rough estimate of affected rows
func (v *EnhancedDataMigrationValidator) estimateAffectedRows(sql string) int64 {
	upperSQL := strings.ToUpper(sql)

	// Heuristic estimation based on SQL patterns
	if strings.HasPrefix(upperSQL, "UPDATE") || strings.HasPrefix(upperSQL, "DELETE") {
		// Look for WHERE clauses that might limit scope
		if regexp.MustCompile(`(?i)\bWHERE\s+\w+\s*=\s*\w+\b`).MatchString(sql) {
			return 1000 // Specific condition, likely affects moderate number of rows
		} else if regexp.MustCompile(`(?i)\bWHERE\s+\w+\s+IS\s+(NULL|NOT NULL)\b`).MatchString(sql) {
			return 50000 // NULL checks often affect many rows
		} else if !regexp.MustCompile(`(?i)\bWHERE\b`).MatchString(sql) {
			return 1000000 // No WHERE clause - potentially affects all rows
		}
		return 10000 // Default estimate for UPDATE/DELETE with WHERE
	}

	if strings.HasPrefix(upperSQL, "INSERT") {
		if strings.Contains(upperSQL, "SELECT") {
			return 100000 // INSERT ... SELECT can insert many rows
		}
		return 1 // Simple INSERT
	}

	return 1000 // Default estimate
}

// estimateExecutionTime estimates how long a migration might take
func (v *EnhancedDataMigrationValidator) estimateExecutionTime(sql string, estimatedRows int64, batchSize int32) time.Duration {
	upperSQL := strings.ToUpper(sql)
	
	// Base time per row (varies by operation)
	var timePerRow time.Duration
	if strings.HasPrefix(upperSQL, "UPDATE") {
		timePerRow = time.Microsecond * 100 // 100µs per row for UPDATE
	} else if strings.HasPrefix(upperSQL, "DELETE") {
		timePerRow = time.Microsecond * 50 // 50µs per row for DELETE
	} else if strings.HasPrefix(upperSQL, "INSERT") {
		timePerRow = time.Microsecond * 75 // 75µs per row for INSERT
	} else {
		timePerRow = time.Microsecond * 50 // Default
	}

	baseTime := time.Duration(estimatedRows) * timePerRow

	// Adjust for batching (batching adds overhead but reduces lock time)
	if batchSize > 0 {
		batchCount := estimatedRows / int64(batchSize)
		if estimatedRows%int64(batchSize) != 0 {
			batchCount++
		}
		// Add 10ms overhead per batch
		batchOverhead := time.Duration(batchCount) * 10 * time.Millisecond
		return baseTime + batchOverhead
	}

	return baseTime
}

// GetValidationSummary returns a human-readable summary of validation results
func (v *ValidationResult) GetValidationSummary() string {
	if v.IsValid && len(v.Warnings) == 0 {
		return "✅ Migration validation passed - ready for execution"
	}

	var summary strings.Builder
	
	if !v.IsValid {
		summary.WriteString(fmt.Sprintf("❌ Migration validation FAILED with %d error(s):\n", len(v.Errors)))
		for _, err := range v.Errors {
			summary.WriteString(fmt.Sprintf("  - %s: %s [%s]\n", err.Field, err.Message, err.Code))
		}
	} else {
		summary.WriteString("✅ Migration validation PASSED")
	}

	if len(v.Warnings) > 0 {
		summary.WriteString(fmt.Sprintf("\n⚠️  %d warning(s):\n", len(v.Warnings)))
		for _, warning := range v.Warnings {
			summary.WriteString(fmt.Sprintf("  - %s: %s [%s]\n", warning.Field, warning.Message, warning.Code))
		}
	}

	if v.EstimatedRows > 0 {
		summary.WriteString(fmt.Sprintf("\n📊 Estimated impact: %d rows, %v execution time\n", v.EstimatedRows, v.EstimatedDuration))
	}

	if v.RequiresApproval {
		summary.WriteString("\n🔒 This migration requires explicit approval before execution\n")
	}

	return summary.String()
} 