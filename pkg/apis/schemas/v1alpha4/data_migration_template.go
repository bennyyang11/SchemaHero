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
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"
)

// DataMigrationTemplate handles template substitution for data migrations
type DataMigrationTemplate struct {
	// Template is the SQL template with placeholders
	Template string `json:"template" yaml:"template"`

	// Parameters define available template parameters
	Parameters []TemplateParameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// TemplateParameter defines a parameter that can be used in templates
type TemplateParameter struct {
	// Name of the parameter (used in templates as {{.ParamName}})
	Name string `json:"name" yaml:"name"`

	// Type of the parameter for validation
	Type ParameterType `json:"type" yaml:"type"`

	// Default value if not provided
	Default string `json:"default,omitempty" yaml:"default,omitempty"`

	// Required indicates if this parameter must be provided
	Required bool `json:"required,omitempty" yaml:"required,omitempty"`

	// Description for documentation
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// ParameterType defines the type of a template parameter
type ParameterType string

const (
	ParameterTypeString    ParameterType = "string"
	ParameterTypeInteger   ParameterType = "integer"
	ParameterTypeBoolean   ParameterType = "boolean"
	ParameterTypeDate      ParameterType = "date"
	ParameterTypeTimestamp ParameterType = "timestamp"
	ParameterTypeEnum      ParameterType = "enum"
	ParameterTypeTable     ParameterType = "table"  // Table name
	ParameterTypeColumn    ParameterType = "column" // Column name
)

// BuiltinFunctions provides common SQL functions for templates
var BuiltinFunctions = template.FuncMap{
	// String functions
	"quote":  QuoteString,
	"escape": EscapeSQL,
	"upper":  strings.ToUpper,
	"lower":  strings.ToLower,
	"trim":   strings.TrimSpace,

	// Date/time functions
	"now":        func() string { return time.Now().UTC().Format(time.RFC3339) },
	"date":       func() string { return time.Now().UTC().Format("2006-01-02") },
	"dateOffset": DateOffset,

	// SQL helpers
	"identifier": QuoteIdentifier,
	"in":         BuildINClause,
	"notIn":      BuildNOTINClause,
	"between":    BuildBETWEENClause,

	// Conditional helpers
	"when":   WhenClause,
	"unless": UnlessClause,
}

// QuoteString safely quotes a string for SQL
func QuoteString(s string) string {
	return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "''"))
}

// EscapeSQL escapes special SQL characters
func EscapeSQL(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return s
}

// QuoteIdentifier quotes a SQL identifier (table/column name)
func QuoteIdentifier(name string) string {
	// This is database-specific, but we'll use double quotes as default
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}

// DateOffset returns a date offset from today
func DateOffset(days int) string {
	return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02")
}

// BuildINClause builds an IN clause from values
func BuildINClause(values ...interface{}) string {
	if len(values) == 0 {
		return "(NULL)"
	}

	// Handle case where first argument is a slice
	if len(values) == 1 {
		switch v := values[0].(type) {
		case []interface{}:
			values = v
		case []int:
			newValues := make([]interface{}, len(v))
			for i, val := range v {
				newValues[i] = val
			}
			values = newValues
		case []string:
			newValues := make([]interface{}, len(v))
			for i, val := range v {
				newValues[i] = val
			}
			values = newValues
		}
	}

	parts := make([]string, len(values))
	for i, v := range values {
		switch val := v.(type) {
		case string:
			parts[i] = QuoteString(val)
		case int, int32, int64:
			parts[i] = fmt.Sprintf("%d", val)
		default:
			parts[i] = fmt.Sprintf("%d", val)
		}
	}

	return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
}

// BuildNOTINClause builds a NOT IN clause
func BuildNOTINClause(values ...interface{}) string {
	return "NOT IN " + BuildINClause(values...)
}

// BuildBETWEENClause builds a BETWEEN clause
func BuildBETWEENClause(start, end interface{}) string {
	return fmt.Sprintf("BETWEEN %v AND %v", start, end)
}

// WhenClause returns SQL when condition is true
func WhenClause(condition bool, sql string) string {
	if condition {
		return sql
	}
	return ""
}

// UnlessClause returns SQL unless condition is true
func UnlessClause(condition bool, sql string) string {
	if !condition {
		return sql
	}
	return ""
}

// RenderTemplate renders a data migration template with given values
func RenderTemplate(tmplStr string, values map[string]interface{}) (string, error) {
	// Create template with builtin functions and strict missing key handling
	tmpl, err := template.New("migration").
		Funcs(BuiltinFunctions).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, values); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}
