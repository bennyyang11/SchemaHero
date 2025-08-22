# Data Migrations in SchemaHero

## Overview

SchemaHero now supports **data migrations** alongside existing schema migrations, enabling complete declarative database change management. This feature allows you to define both structural (DDL) and data transformation (DML) operations within a single, unified workflow.

## What Are Data Migrations?

**Data migrations** are operations that modify the actual data stored in your database tables, as opposed to schema migrations which modify the structure of tables themselves.

### DDL vs DML Comparison

| Schema Migrations (DDL) | Data Migrations (DML) |
|------------------------|----------------------|
| `CREATE TABLE users` | `INSERT INTO users VALUES (...)` |
| `ALTER TABLE users ADD COLUMN status VARCHAR(20)` | `UPDATE users SET status = 'active'` |
| `DROP INDEX idx_email` | `DELETE FROM users WHERE inactive = true` |

## Getting Started

### Basic Data Migration Example

```yaml
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: users-with-data-migration
spec:
  database: myapp
  name: users
  schema:
    postgres:
      columns:
        - name: id
          type: integer
        - name: email
          type: varchar(255)
        - name: status
          type: varchar(20)
  dataMigrations:
    - name: set-default-status
      description: "Set default status for existing users"
      sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
      batchSize: 1000
      timeout: 30m
      type: BACKFILL
```

### Enhanced CLI Usage

```bash
# Plan with data migration preview
kubectl schemahero plan --spec-file users.yaml --verbose --show-metrics

# Apply with progress tracking
kubectl schemahero apply --spec-file users.yaml --show-progress

# Check migration status
kubectl schemahero get migrations
kubectl schemahero describe migration users-abc123
```

## Core Features

### 1. Migration Types

SchemaHero supports five types of data migrations:

- **`BACKFILL`**: Fill in missing data or default values
- **`TRANSFORM`**: Modify existing data format or structure
- **`CLEANUP`**: Remove or archive old data
- **`COPY`**: Duplicate data between tables or columns
- **`CUSTOM`**: User-defined migration logic

### 2. Template System

Create reusable migrations with parameters:

```yaml
dataMigrations:
  - name: normalize-emails
    template:
      template: |
        UPDATE {{.table_name}} 
        SET email = {{.email | lower | trim}} 
        WHERE created_at > {{.cutoff_date | quote}}
        AND status IN {{.valid_statuses | in}}
      parameters:
        - name: cutoff_date
          type: date
          required: true
        - name: valid_statuses
          type: enum
          default: ["active", "pending"]
```

### 3. Conditional Execution

Execute migrations only when conditions are met:

```yaml
dataMigrations:
  - name: cleanup-old-logs
    sql: "DELETE FROM logs WHERE created_at < NOW() - INTERVAL '90 days'"
    conditions:
      - query: "SELECT COUNT(*) FROM logs WHERE created_at < NOW() - INTERVAL '90 days'"
        operator: ">"
        value: 1000
```

### 4. Dependencies and Ordering

Define execution order with dependencies:

```yaml
dataMigrations:
  - name: create-user-profiles
    sql: "INSERT INTO profiles (user_id) SELECT id FROM users WHERE profile_id IS NULL"
    
  - name: link-profiles
    sql: "UPDATE users SET profile_id = p.id FROM profiles p WHERE users.id = p.user_id"
    dependsOn: ["create-user-profiles"]
    
  - name: cleanup-temp-data
    sql: "DELETE FROM temp_migration_data"
    dependsOn: ["create-user-profiles", "link-profiles"]
```

### 5. Batching for Performance

Process large datasets in chunks:

```yaml
dataMigrations:
  - name: update-large-table
    sql: "UPDATE users SET last_migration = NOW() WHERE last_migration IS NULL"
    batchSize: 10000      # Process 10K rows at a time
    batchDelayMs: 100     # 100ms delay between batches
    timeout: 2h           # Allow up to 2 hours
```

## Database Support Matrix

| Database | Support Level | Features | Limitations |
|----------|---------------|----------|-------------|
| **PostgreSQL** | 🟢 **Full** | Transactions, batching, templates, conditions | None |
| **MySQL** | 🟢 **Full** | Transactions, batching, templates, conditions | None |
| **CockroachDB** | 🟢 **Full** | Uses PostgreSQL syntax | None |
| **TimescaleDB** | 🟢 **Full** | Uses PostgreSQL syntax | None |
| **SQLite** | 🟡 **Good** | Batching, templates, conditions | Limited concurrency |
| **RQLite** | 🟡 **Good** | Distributed SQLite support | Network latency considerations |
| **Cassandra** | 🟠 **Limited** | Basic CQL operations | No JOINs, limited conditions |

## Migration Lifecycle

### 1. Planning Phase
```bash
kubectl schemahero plan --spec-file table.yaml --dry-run --verbose
```
- Validates syntax and safety
- Estimates execution time and affected rows
- Shows DDL and DML statements to be executed

### 2. Approval Phase  
```bash
kubectl schemahero approve migration table-abc123
```
- Reviews generated DDL and DML
- Validates migration safety
- Approves for execution

### 3. Execution Phase
```bash
kubectl schemahero apply migration table-abc123
```
- Executes schema changes first (DDL)
- Then executes data migrations (DML)
- Tracks progress and provides rollback if needed

### 4. Monitoring Phase
```bash
kubectl schemahero get migrations
kubectl schemahero describe migration table-abc123
```
- Monitor execution progress
- View detailed status and metrics
- Check for any errors or warnings

## Safety Features

### Validation Layer
- **SQL syntax validation** for target database
- **Dangerous operation detection** (DROP, TRUNCATE, mass updates)
- **Dependency cycle detection**
- **Resource usage estimation**

### Execution Safety
- **Schema-first execution** - DDL completes before DML
- **Transaction support** where available
- **Timeout protection** prevents runaway operations
- **Rollback capability** for reversible migrations

### Operational Safety
- **Execution locking** prevents concurrent migrations on same table
- **Progress tracking** with real-time status updates
- **Comprehensive logging** for audit and debugging
- **Confirmation prompts** for destructive operations

## Migration Patterns

### Pattern 1: Add Column with Default Value
```yaml
schema:
  postgres:
    columns:
      - name: status
        type: varchar(20)
dataMigrations:
  - name: set-default-status
    sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
```

### Pattern 2: Data Type Conversion
```yaml
schema:
  postgres:
    columns:
      - name: created_at
        type: "timestamp with time zone"
dataMigrations:
  - name: convert-timezone
    sql: "UPDATE events SET created_at = created_at AT TIME ZONE 'UTC'"
```

### Pattern 3: Cross-table Data Migration
```yaml
dataMigrations:
  - name: denormalize-data
    sql: |
      UPDATE orders 
      SET customer_name = customers.name,
          customer_email = customers.email
      FROM customers 
      WHERE orders.customer_id = customers.id
```

### Pattern 4: Conditional Migration
```yaml
dataMigrations:
  - name: archive-old-data
    sql: "UPDATE posts SET archived = true WHERE created_at < NOW() - INTERVAL '1 year'"
    conditions:
      - query: "SELECT COUNT(*) FROM posts WHERE created_at < NOW() - INTERVAL '1 year'"
        operator: ">"
        value: 0
```

## Advanced Features

### Rollback Support
```yaml
dataMigrations:
  - name: reversible-migration
    sql: "UPDATE users SET status = 'migrated'"
    reversible: true
    reverseSQL: "UPDATE users SET status = 'active' WHERE status = 'migrated'"
```

### Priority and Ordering
```yaml
dataMigrations:
  - name: high-priority-fix
    sql: "UPDATE critical_table SET fixed = true"
    priority: 10
    
  - name: low-priority-cleanup
    sql: "DELETE FROM temp_data WHERE processed = true"
    priority: 1
```

### Template Functions
```yaml
dataMigrations:
  - name: template-example
    template:
      template: |
        UPDATE {{.table_name}} 
        SET updated_at = {{.now}},
            status = {{.status | quote}},
            user_count = {{.count | default "0"}}
        WHERE created_at < {{.cutoff | quote}}
```

## Best Practices

### 1. Safety First
- ✅ **Always use WHERE clauses** in UPDATE/DELETE statements
- ✅ **Test with small datasets** before production
- ✅ **Use batching** for large table operations
- ✅ **Set appropriate timeouts** for long-running migrations

### 2. Performance Optimization
- ✅ **Use indexed columns** in WHERE clauses
- ✅ **Batch large operations** (10K-50K rows per batch)
- ✅ **Schedule during low-traffic periods**
- ✅ **Monitor database performance** during execution

### 3. Maintainability
- ✅ **Use descriptive migration names** and descriptions
- ✅ **Document business logic** in migration comments
- ✅ **Use templates** for reusable patterns
- ✅ **Test rollback procedures** where applicable

### 4. Monitoring and Observability
- ✅ **Monitor migration progress** with describe commands
- ✅ **Set up alerts** for failed migrations
- ✅ **Review execution logs** regularly
- ✅ **Track performance metrics** over time

## Next Steps

1. **Review the [API Documentation](data-migrations-api.md)** for detailed field references
2. **Explore [Migration Patterns](migration-patterns.md)** for common use cases
3. **Check [Database-Specific Guides](database-guides/)** for your database type
4. **Read [Troubleshooting Guide](troubleshooting.md)** for common issues
5. **See [Examples](../examples/data-migrations/)** for working implementations

## Support and Community

- **GitHub Issues**: Report bugs and feature requests
- **Discussions**: Ask questions and share patterns
- **Documentation**: Contribute improvements and examples
- **Testing**: Help test with your database and use cases 