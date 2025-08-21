# SchemaHero Data Migrations API Documentation

## Overview

SchemaHero now supports data migrations alongside schema migrations, allowing you to manage both structural (DDL) and data transformation (DML) operations in a unified, declarative manner. This document describes the API for defining data migrations.

## Table of Contents

1. [Basic Concepts](#basic-concepts)
2. [DataMigration Specification](#datamigration-specification)
3. [Templates and Parameters](#templates-and-parameters)
4. [Conditions](#conditions)
5. [Dependencies](#dependencies)
6. [Batch Processing](#batch-processing)
7. [Migration Types](#migration-types)
8. [Validation Rules](#validation-rules)
9. [Examples](#examples)

## Basic Concepts

Data migrations in SchemaHero are defined as part of the `TableSpec` and are executed after schema changes have been applied. Each data migration represents a transformation that should be applied to the data in your database.

### Key Features

- **Declarative syntax**: Define what data changes you want, not how to execute them
- **Template support**: Use parameterized queries for reusable migration patterns
- **Conditional execution**: Only run migrations when specific conditions are met
- **Dependency management**: Control execution order with dependencies
- **Batch processing**: Handle large datasets efficiently
- **Safety features**: Built-in validation and dangerous operation detection

## DataMigration Specification

### Basic Structure

```yaml
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: my-table
spec:
  database: mydb
  name: users
  schema:
    # ... schema definition ...
  dataMigrations:
    - name: "migration-name"
      description: "Human-readable description"
      sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
      # ... additional fields ...
```

### Field Reference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier for the migration (must be a valid Kubernetes name) |
| `description` | string | No | Human-readable description of what the migration does |
| `sql` | string | Yes* | SQL statement(s) to execute (*required if template is not provided) |
| `template` | object | Yes* | Template-based SQL generation (*required if sql is not provided) |
| `conditions` | []object | No | Conditions that must be met for execution |
| `dependsOn` | []string | No | Names of migrations that must complete first |
| `batchSize` | int32 | No | Number of rows to process per batch (0 = no batching) |
| `batchDelayMs` | int32 | No | Milliseconds to wait between batches |
| `timeout` | Duration | No | Maximum time allowed for migration (e.g., "30m", "2h") |
| `type` | string | No | Migration type: BACKFILL, TRANSFORM, CLEANUP, COPY, CUSTOM |
| `priority` | int32 | No | Execution priority (higher values execute first) |
| `reversible` | bool | No | Whether the migration can be rolled back |
| `reverseSQL` | string | No | SQL to reverse the migration (required if reversible=true) |
| `tags` | []string | No | Tags for categorizing migrations |

## Templates and Parameters

Templates allow you to create reusable, parameterized data migrations. They use Go's text/template syntax with custom SQL-safe functions.

### Template Structure

```yaml
template:
  template: |
    UPDATE {{identifier .tableName}} 
    SET {{identifier .column}} = {{quote .value}}
    WHERE {{identifier .column}} IS NULL
  parameters:
    - name: tableName
      type: table
      required: true
      description: "Target table name"
    - name: column
      type: column
      required: true
      description: "Column to update"
    - name: value
      type: string
      default: "default_value"
      description: "Value to set"
```

### Parameter Types

- `string`: Text values
- `integer`: Numeric values
- `boolean`: True/false values
- `date`: Date values (YYYY-MM-DD)
- `timestamp`: Timestamp values (RFC3339)
- `enum`: Predefined set of values
- `table`: Table names (validated as identifiers)
- `column`: Column names (validated as identifiers)

### Built-in Template Functions

| Function | Description | Example |
|----------|-------------|---------|
| `quote` | Safely quotes a string value | `{{quote .value}}` → `'value'` |
| `identifier` | Quotes an identifier (table/column) | `{{identifier .table}}` → `"table"` |
| `upper` | Converts to uppercase | `{{upper .text}}` |
| `lower` | Converts to lowercase | `{{lower .text}}` |
| `trim` | Removes whitespace | `{{trim .text}}` |
| `now` | Current timestamp | `{{now}}` |
| `date` | Current date | `{{date}}` |
| `dateOffset` | Date with offset | `{{dateOffset -30}}` |
| `in` | Creates IN clause | `{{in "a" "b" "c"}}` → `('a', 'b', 'c')` |
| `when` | Conditional SQL | `{{when .condition "SQL"}}` |
| `unless` | Inverse conditional | `{{unless .condition "SQL"}}` |

## Conditions

Conditions control whether a migration should execute. All conditions must evaluate to true for the migration to run.

### Condition Structure

```yaml
conditions:
  - query: "SELECT COUNT(*) FROM users WHERE status IS NULL"
    operator: ">"
    value: 0
```

### Supported Operators

- `>`, `<`, `>=`, `<=`, `=`, `!=`: Numeric comparisons
- `EXISTS`: Check if query returns any rows
- `NOT EXISTS`: Check if query returns no rows

### Examples

```yaml
# Run only if there are records to update
conditions:
  - query: "SELECT COUNT(*) FROM orders WHERE processed = false"
    operator: ">"
    value: 0

# Run only if a table exists
conditions:
  - query: "SELECT 1 FROM information_schema.tables WHERE table_name = 'archive_table'"
    operator: "EXISTS"

# Run only if a specific configuration is set
conditions:
  - query: "SELECT value FROM config WHERE key = 'enable_migration'"
    operator: "="
    value: 1
```

## Dependencies

Dependencies ensure migrations execute in the correct order. SchemaHero uses topological sorting to determine the execution sequence.

### Simple Dependencies

```yaml
dataMigrations:
  - name: "create-archive"
    sql: "CREATE TABLE IF NOT EXISTS users_archive AS SELECT * FROM users WHERE false"
    
  - name: "archive-old-users"
    dependsOn: ["create-archive"]
    sql: "INSERT INTO users_archive SELECT * FROM users WHERE created_at < '2020-01-01'"
```

### Complex Dependencies

```yaml
dataMigrations:
  - name: "base"
    sql: "..."
    
  - name: "step1"
    dependsOn: ["base"]
    sql: "..."
    
  - name: "step2"
    dependsOn: ["base"]
    sql: "..."
    
  - name: "final"
    dependsOn: ["step1", "step2"]
    sql: "..."
```

## Batch Processing

For large datasets, batch processing prevents long-running transactions and reduces database load.

### Configuration

```yaml
batchSize: 5000        # Process 5000 rows at a time
batchDelayMs: 100      # Wait 100ms between batches
timeout: "2h"          # Allow up to 2 hours for completion
```

### How Batching Works

1. The migration is split into multiple smaller transactions
2. Each transaction processes up to `batchSize` rows
3. After each batch, SchemaHero waits `batchDelayMs` milliseconds
4. Progress is tracked between batches
5. If the `timeout` is exceeded, the migration fails (but completed batches are preserved)

## Migration Types

Migration types help categorize and document the purpose of each migration:

| Type | Description | Use Case |
|------|-------------|----------|
| `BACKFILL` | Fill new columns with data | Adding derived data to new columns |
| `TRANSFORM` | Modify existing data | Normalizing, cleaning, or converting data |
| `CLEANUP` | Remove obsolete data | Archiving or deleting old records |
| `COPY` | Copy data between tables | Creating archives or duplicating data |
| `CUSTOM` | Other migration types | Complex migrations that don't fit categories |

## Validation Rules

SchemaHero validates data migrations to prevent common errors:

### Name Validation
- Must be lowercase alphanumeric with hyphens
- Must start and end with alphanumeric characters
- Maximum 63 characters
- Must be unique within the table

### SQL Validation
- Detects dangerous patterns:
  - `DROP TABLE/DATABASE/SCHEMA`
  - `TRUNCATE TABLE`
  - `DELETE FROM table;` (without WHERE)
  - `UPDATE table SET ...;` (without WHERE)
- Prevents SQL injection patterns
- Validates template syntax

### Dependency Validation
- Checks for circular dependencies
- Ensures all referenced dependencies exist
- Validates dependency ordering

## Examples

### Example 1: Simple Backfill

```yaml
dataMigrations:
  - name: "set-default-status"
    description: "Set default status for users without one"
    type: BACKFILL
    sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
    conditions:
      - query: "SELECT COUNT(*) FROM users WHERE status IS NULL"
        operator: ">"
        value: 0
```

### Example 2: Template-Based Migration

```yaml
dataMigrations:
  - name: "normalize-emails"
    description: "Normalize email addresses across tables"
    type: TRANSFORM
    template:
      template: |
        UPDATE {{identifier .table}}
        SET email = LOWER(TRIM(email))
        WHERE email != LOWER(TRIM(email))
      parameters:
        - name: table
          type: table
          required: true
    batchSize: 1000
```

### Example 3: Complex Migration with Dependencies

```yaml
dataMigrations:
  - name: "mark-for-archive"
    type: TRANSFORM
    priority: 10
    sql: "UPDATE orders SET archive_flag = true WHERE created_at < CURRENT_DATE - INTERVAL '1 year'"
    
  - name: "copy-to-archive"
    type: COPY
    priority: 5
    dependsOn: ["mark-for-archive"]
    sql: |
      INSERT INTO orders_archive 
      SELECT * FROM orders WHERE archive_flag = true
    conditions:
      - query: "SELECT 1 FROM information_schema.tables WHERE table_name = 'orders_archive'"
        operator: "EXISTS"
    
  - name: "cleanup-archived"
    type: CLEANUP
    priority: 1
    dependsOn: ["copy-to-archive"]
    sql: "DELETE FROM orders WHERE archive_flag = true"
    reversible: true
    reverseSQL: |
      INSERT INTO orders 
      SELECT * FROM orders_archive 
      WHERE id NOT IN (SELECT id FROM orders)
```

### Example 4: Conditional Feature Rollout

```yaml
dataMigrations:
  - name: "enable-new-feature"
    description: "Gradually enable new feature for users"
    type: TRANSFORM
    template:
      template: |
        UPDATE users 
        SET features = features || '{"{{.feature}}": true}'::jsonb
        WHERE created_at > {{dateOffset .daysAgo | quote}}
          AND features->{{quote .feature}} IS NULL
        LIMIT {{.userCount}}
      parameters:
        - name: feature
          type: string
          default: "new_dashboard"
        - name: daysAgo
          type: integer
          default: -30
        - name: userCount
          type: integer
          default: 1000
    tags: ["feature-flag", "gradual-rollout"]
```

## Best Practices

1. **Always use conditions**: Prevent re-running migrations unnecessarily
2. **Set appropriate timeouts**: Prevent migrations from running indefinitely
3. **Use batching for large datasets**: Avoid long-running transactions
4. **Make migrations idempotent**: Ensure they can be safely re-run
5. **Document with descriptions**: Help future maintainers understand the purpose
6. **Test in non-production first**: Validate migrations before production
7. **Use templates for reusability**: Create consistent patterns across migrations
8. **Tag related migrations**: Group migrations for easier management

## Troubleshooting

### Common Errors

1. **"dangerous SQL pattern detected"**: Your SQL contains potentially destructive operations
2. **"circular dependency detected"**: Your migrations have cyclic dependencies
3. **"template parsing failed"**: Your template syntax is invalid
4. **"condition query must be a SELECT statement"**: Conditions must use SELECT queries

### Debug Tips

1. Use dry-run mode to preview migrations
2. Check logs for detailed error messages
3. Validate templates with test parameters
4. Use smaller batch sizes for debugging
5. Monitor migration progress through status fields 