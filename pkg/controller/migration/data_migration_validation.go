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
	"strings"
	"time"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// DataMigrationValidator validates data migration specifications
type DataMigrationValidator struct {
	allowedOperators map[string]bool
	sqlPatterns      []*regexp.Regexp
}

// NewDataMigrationValidator creates a new validator
func NewDataMigrationValidator() *DataMigrationValidator {
	return &DataMigrationValidator{
		allowedOperators: map[string]bool{
			">":         true,
			"<":         true,
			">=":        true,
			"<=":        true,
			"=":         true,
			"!=":        true,
			"EXISTS":    true,
			"NOT EXISTS": true,
		},
		sqlPatterns: []*regexp.Regexp{
			// Dangerous operations that should be caught
			regexp.MustCompile(`(?i)\bDROP\s+(TABLE|DATABASE|SCHEMA)\b`),
			regexp.MustCompile(`(?i)\bTRUNCATE\s+TABLE\b`),
			regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+\w+\s*;`), // DELETE without WHERE
			regexp.MustCompile(`(?i)\bUPDATE\s+\w+\s+SET\s+.*\s*;`), // UPDATE without WHERE
		},
	}
}

// ValidateDataMigration validates a single data migration
func (v *DataMigrationValidator) ValidateDataMigration(dm *schemasv1alpha4.DataMigration) error {
	// Validate name
	if err := v.validateName(dm.Name); err != nil {
		return err
	}
	
	// Validate SQL or template (one must be provided)
	if dm.SQL == "" && dm.Template == nil {
		return fmt.Errorf("either sql or template must be provided")
	}
	
	if dm.SQL != "" && dm.Template != nil {
		return fmt.Errorf("only one of sql or template should be provided")
	}
	
	// Validate SQL content
	if dm.SQL != "" {
		if err := v.validateSQL(dm.SQL); err != nil {
			return fmt.Errorf("invalid SQL: %w", err)
		}
	}
	
	// Validate template
	if dm.Template != nil {
		if err := v.validateTemplate(dm.Template); err != nil {
			return fmt.Errorf("invalid template: %w", err)
		}
	}
	
	// Validate conditions
	for i, condition := range dm.Conditions {
		if err := v.validateCondition(condition); err != nil {
			return fmt.Errorf("invalid condition[%d]: %w", i, err)
		}
	}
	
	// Validate batch configuration
	if dm.BatchSize < 0 {
		return fmt.Errorf("batchSize must be non-negative")
	}
	
	if dm.BatchDelayMs < 0 {
		return fmt.Errorf("batchDelayMs must be non-negative")
	}
	
	// Validate timeout
	if dm.Timeout != nil && dm.Timeout.Duration < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}
	
	// Validate priority
	if dm.Priority < 0 {
		return fmt.Errorf("priority must be non-negative")
	}
	
	// Validate reversible configuration
	if dm.Reversible && dm.ReverseSQL == "" {
		return fmt.Errorf("reverseSQL must be provided when reversible is true")
	}
	
	if dm.ReverseSQL != "" {
		if err := v.validateSQL(dm.ReverseSQL); err != nil {
			return fmt.Errorf("invalid reverseSQL: %w", err)
		}
	}
	
	return nil
}

// validateName validates migration name
func (v *DataMigrationValidator) validateName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	
	// Name should be valid Kubernetes name
	nameRegex := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	if !nameRegex.MatchString(name) {
		return fmt.Errorf("name must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character")
	}
	
	if len(name) > 63 {
		return fmt.Errorf("name must be no more than 63 characters")
	}
	
	return nil
}

// validateSQL validates SQL content
func (v *DataMigrationValidator) validateSQL(sql string) error {
	if sql == "" {
		return fmt.Errorf("SQL cannot be empty")
	}
	
	// Check for dangerous patterns
	for _, pattern := range v.sqlPatterns {
		if pattern.MatchString(sql) {
			return fmt.Errorf("dangerous SQL pattern detected: %s", pattern.String())
		}
	}
	
	// Basic SQL validation
	trimmedSQL := strings.TrimSpace(sql)
	
	// Should not end with GO or delimiter commands
	if regexp.MustCompile(`(?i)\bGO\s*$`).MatchString(trimmedSQL) {
		return fmt.Errorf("SQL should not end with GO statement")
	}
	
	// Check for common SQL injection patterns
	if strings.Contains(sql, "--") && !strings.HasPrefix(trimmedSQL, "--") {
		// Allow comments at start of line but be suspicious of inline comments
		lines := strings.Split(sql, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "--") && !strings.HasPrefix(trimmed, "--") {
				return fmt.Errorf("suspicious inline comment detected")
			}
		}
	}
	
	return nil
}

// validateTemplate validates a data migration template
func (v *DataMigrationValidator) validateTemplate(tmpl *schemasv1alpha4.DataMigrationTemplate) error {
	if tmpl.Template == "" {
		return fmt.Errorf("template cannot be empty")
	}
	
	// Try to parse the template
	testValues := make(map[string]interface{})
	for _, param := range tmpl.Parameters {
		// Add test values based on type
		switch param.Type {
		case schemasv1alpha4.ParameterTypeString:
			testValues[param.Name] = "test"
		case schemasv1alpha4.ParameterTypeInteger:
			testValues[param.Name] = 1
		case schemasv1alpha4.ParameterTypeBoolean:
			testValues[param.Name] = true
		case schemasv1alpha4.ParameterTypeDate:
			testValues[param.Name] = time.Now().Format("2006-01-02")
		case schemasv1alpha4.ParameterTypeTimestamp:
			testValues[param.Name] = time.Now().Format(time.RFC3339)
		default:
			testValues[param.Name] = "test"
		}
	}
	
	// Try to render template with test values
	rendered, err := schemasv1alpha4.RenderTemplate(tmpl.Template, testValues)
	if err != nil {
		return fmt.Errorf("template parsing failed: %w", err)
	}
	
	// Validate rendered SQL
	if err := v.validateSQL(rendered); err != nil {
		return fmt.Errorf("rendered template produces invalid SQL: %w", err)
	}
	
	// Validate parameters
	for i, param := range tmpl.Parameters {
		if err := v.validateTemplateParameter(param); err != nil {
			return fmt.Errorf("invalid parameter[%d]: %w", i, err)
		}
	}
	
	return nil
}

// validateTemplateParameter validates a template parameter
func (v *DataMigrationValidator) validateTemplateParameter(param schemasv1alpha4.TemplateParameter) error {
	if param.Name == "" {
		return fmt.Errorf("parameter name is required")
	}
	
	// Validate parameter name (should be valid Go identifier)
	paramRegex := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
	if !paramRegex.MatchString(param.Name) {
		return fmt.Errorf("parameter name must be a valid identifier")
	}
	
	// Validate parameter type
	validTypes := map[schemasv1alpha4.ParameterType]bool{
		schemasv1alpha4.ParameterTypeString:    true,
		schemasv1alpha4.ParameterTypeInteger:   true,
		schemasv1alpha4.ParameterTypeBoolean:   true,
		schemasv1alpha4.ParameterTypeDate:      true,
		schemasv1alpha4.ParameterTypeTimestamp: true,
		schemasv1alpha4.ParameterTypeEnum:      true,
		schemasv1alpha4.ParameterTypeTable:     true,
		schemasv1alpha4.ParameterTypeColumn:    true,
	}
	
	if !validTypes[param.Type] {
		return fmt.Errorf("invalid parameter type: %s", param.Type)
	}
	
	return nil
}

// validateCondition validates a migration condition
func (v *DataMigrationValidator) validateCondition(condition schemasv1alpha4.DataMigrationCondition) error {
	if condition.Query == "" {
		return fmt.Errorf("condition query is required")
	}
	
	if !v.allowedOperators[condition.Operator] {
		return fmt.Errorf("invalid operator: %s", condition.Operator)
	}
	
	// For EXISTS and NOT EXISTS, value should be ignored
	if condition.Operator == "EXISTS" || condition.Operator == "NOT EXISTS" {
		if condition.Value != 0 {
			return fmt.Errorf("value should not be set for %s operator", condition.Operator)
		}
	}
	
	// Validate the query itself
	if err := v.validateSQL(condition.Query); err != nil {
		return fmt.Errorf("invalid condition query: %w", err)
	}
	
	// Query should be a SELECT statement
	if !regexp.MustCompile(`(?i)^\s*SELECT\b`).MatchString(condition.Query) {
		return fmt.Errorf("condition query must be a SELECT statement")
	}
	
	return nil
}

// ValidateDataMigrations validates a list of data migrations
func (v *DataMigrationValidator) ValidateDataMigrations(migrations []schemasv1alpha4.DataMigration) error {
	// Check for duplicate names
	names := make(map[string]bool)
	for i, dm := range migrations {
		if names[dm.Name] {
			return fmt.Errorf("duplicate migration name: %s", dm.Name)
		}
		names[dm.Name] = true
		
		// Validate individual migration
		if err := v.ValidateDataMigration(&migrations[i]); err != nil {
			return fmt.Errorf("migration[%d] %s: %w", i, dm.Name, err)
		}
	}
	
	// Validate dependencies exist
	resolver := schemasv1alpha4.NewDependencyResolver(migrations)
	if err := resolver.ValidateDependencies(); err != nil {
		return fmt.Errorf("dependency validation failed: %w", err)
	}
	
	return nil
} 