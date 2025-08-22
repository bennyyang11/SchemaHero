# Migration Patterns Guide

This guide provides comprehensive examples of common data migration patterns using SchemaHero's enhanced features.

## Table of Contents

1. [Basic Patterns](#basic-patterns)
2. [Advanced Patterns](#advanced-patterns)
3. [Database-Specific Patterns](#database-specific-patterns)
4. [Performance Patterns](#performance-patterns)
5. [Safety Patterns](#safety-patterns)

## Basic Patterns

### Pattern 1: Add Column with Default Value

**Use Case**: Adding a new required column to an existing table with default values.

```yaml
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: users-add-status
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
          constraints:
            notNull: true
  dataMigrations:
    - name: set-default-status
      description: "Set default status for existing users"
      sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
      type: BACKFILL
      batchSize: 5000
```

### Pattern 2: Data Type Conversion

**Use Case**: Converting a column to a different data type with data transformation.

```yaml
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: events-timezone-conversion
spec:
  database: analytics
  name: events
  schema:
    postgres:
      columns:
        - name: id
          type: bigserial
        - name: created_at
          type: "timestamp with time zone"
        - name: legacy_created_at
          type: timestamp
  dataMigrations:
    - name: convert-timezone
      description: "Convert legacy timestamps to timezone-aware format"
      sql: |
        UPDATE events 
        SET created_at = legacy_created_at AT TIME ZONE 'UTC'
        WHERE created_at IS NULL AND legacy_created_at IS NOT NULL
      type: TRANSFORM
      batchSize: 10000
      timeout: 1h
```

### Pattern 3: Normalize Data

**Use Case**: Splitting combined data into separate normalized fields.

```yaml
dataMigrations:
  - name: split-full-name
    description: "Split full_name into first_name and last_name"
    sql: |
      UPDATE users 
      SET first_name = SPLIT_PART(full_name, ' ', 1),
          last_name = SPLIT_PART(full_name, ' ', 2)
      WHERE full_name IS NOT NULL 
        AND (first_name IS NULL OR last_name IS NULL)
    type: TRANSFORM
    batchSize: 2000
```

## Advanced Patterns

### Pattern 4: Cross-Table Data Migration

**Use Case**: Denormalizing data from related tables for performance.

```yaml
dataMigrations:
  - name: denormalize-customer-data
    description: "Copy customer details to orders for faster queries"
    sql: |
      UPDATE orders 
      SET customer_name = customers.name,
          customer_email = customers.email,
          customer_tier = customers.tier
      FROM customers 
      WHERE orders.customer_id = customers.id
        AND orders.customer_name IS NULL
    type: COPY
    batchSize: 5000
    timeout: 45m
```

### Pattern 5: Conditional Data Migration

**Use Case**: Migrate data only when certain conditions are met.

```yaml
dataMigrations:
  - name: archive-inactive-users
    description: "Archive users who haven't logged in for 2 years"
    sql: |
      UPDATE users 
      SET status = 'archived',
          archived_at = NOW()
      WHERE last_login_at < NOW() - INTERVAL '2 years'
        AND status = 'active'
    conditions:
      - query: "SELECT COUNT(*) FROM users WHERE last_login_at < NOW() - INTERVAL '2 years' AND status = 'active'"
        operator: ">"
        value: 0
    type: CLEANUP
    batchSize: 1000
```

### Pattern 6: Multi-Step Migration with Dependencies

**Use Case**: Complex migration requiring multiple ordered steps.

```yaml
dataMigrations:
  - name: create-user-profiles
    description: "Create profile records for users without profiles"
    sql: |
      INSERT INTO user_profiles (user_id, created_at)
      SELECT id, NOW() FROM users 
      WHERE id NOT IN (SELECT user_id FROM user_profiles WHERE user_id IS NOT NULL)
    type: BACKFILL
    priority: 3

  - name: populate-profile-defaults
    description: "Set default values in newly created profiles"
    sql: |
      UPDATE user_profiles 
      SET preferences = '{"theme": "light", "notifications": true}',
          settings = '{}'
      WHERE preferences IS NULL
    dependsOn: ["create-user-profiles"]
    type: BACKFILL
    priority: 2

  - name: link-user-profiles
    description: "Update users table with profile references"
    sql: |
      UPDATE users 
      SET profile_id = up.id
      FROM user_profiles up
      WHERE users.id = up.user_id AND users.profile_id IS NULL
    dependsOn: ["create-user-profiles", "populate-profile-defaults"]
    type: TRANSFORM
    priority: 1
```

### Pattern 7: Template-Based Reusable Migration

**Use Case**: Reusable migration pattern that can be parameterized.

```yaml
dataMigrations:
  - name: standardize-phone-numbers
    description: "Standardize phone number format"
    template:
      template: |
        UPDATE {{.table_name}}
        SET {{.phone_column}} = REGEXP_REPLACE(
          {{.phone_column}}, 
          '[^0-9]', 
          '', 
          'g'
        )
        WHERE {{.phone_column}} IS NOT NULL
          AND LENGTH({{.phone_column}}) {{.length_condition}}
      parameters:
        - name: phone_column
          type: column
          required: true
        - name: length_condition
          type: string
          default: "> 10"
          required: false
    type: TRANSFORM
    batchSize: 3000
```

## Database-Specific Patterns

### PostgreSQL Advanced Features

```yaml
dataMigrations:
  - name: jsonb-migration
    description: "Migrate JSON data to JSONB with indexing"
    sql: |
      UPDATE user_metadata 
      SET settings_jsonb = settings::jsonb
      WHERE settings_jsonb IS NULL AND settings IS NOT NULL
    type: TRANSFORM
    
  - name: full-text-search-setup
    description: "Create full-text search vectors"
    sql: |
      UPDATE articles 
      SET search_vector = to_tsvector('english', title || ' ' || content)
      WHERE search_vector IS NULL
    type: BACKFILL
```

### MySQL-Specific Patterns

```yaml
dataMigrations:
  - name: utf8mb4-migration
    description: "Convert text data for emoji support"
    sql: |
      UPDATE posts 
      SET content = CONVERT(content USING utf8mb4)
      WHERE content IS NOT NULL
    type: TRANSFORM
    
  - name: mysql-date-conversion
    description: "Convert MySQL date formats"
    sql: |
      UPDATE events 
      SET event_date = STR_TO_DATE(legacy_date, '%Y-%m-%d %H:%i:%s')
      WHERE event_date IS NULL AND legacy_date IS NOT NULL
    type: TRANSFORM
```

### Cassandra CQL Patterns

```yaml
dataMigrations:
  - name: cassandra-update
    description: "Update records with primary key"
    sql: "UPDATE users SET last_login = toTimestamp(now()) WHERE user_id = ?"
    type: TRANSFORM
    # Note: Cassandra requires WHERE clause with primary key
    
  - name: cassandra-insert
    description: "Insert default data with time-based UUID"
    sql: "INSERT INTO events (id, user_id, event_type, created_at) VALUES (now(), ?, 'migration', toTimestamp(now()))"
    type: BACKFILL
```

## Performance Patterns

### Pattern 8: Large Dataset Migration

**Use Case**: Migrating millions of records efficiently.

```yaml
dataMigrations:
  - name: large-dataset-migration
    description: "Update 10M+ user records efficiently"
    sql: |
      UPDATE users 
      SET normalized_email = LOWER(TRIM(email)),
          updated_at = NOW()
      WHERE normalized_email IS NULL
        AND email IS NOT NULL
    type: TRANSFORM
    batchSize: 50000        # Large batches for efficiency
    batchDelayMs: 500       # Longer delay to reduce database load
    timeout: 6h             # Allow plenty of time
    priority: 5             # Lower priority to not block other operations
```

### Pattern 9: Index-Optimized Migration

**Use Case**: Using database indexes for optimal performance.

```yaml
dataMigrations:
  - name: index-optimized-cleanup
    description: "Remove old records using indexed timestamp"
    sql: |
      DELETE FROM audit_logs 
      WHERE created_at < NOW() - INTERVAL '1 year'
        AND log_level = 'DEBUG'
    conditions:
      - query: "SELECT COUNT(*) FROM audit_logs WHERE created_at < NOW() - INTERVAL '1 year' AND log_level = 'DEBUG'"
        operator: ">"
        value: 10000
    type: CLEANUP
    batchSize: 25000
```

## Safety Patterns

### Pattern 10: Safe Mass Update with Verification

**Use Case**: Performing mass updates with safety checks.

```yaml
dataMigrations:
  - name: safe-mass-update
    description: "Safely update all user passwords with verification"
    sql: |
      UPDATE users 
      SET password_hash = 'MIGRATION_PLACEHOLDER',
          password_updated_at = NOW()
      WHERE password_hash IS NOT NULL
        AND password_updated_at IS NULL
    conditions:
      - query: "SELECT COUNT(*) FROM users WHERE password_hash IS NOT NULL AND password_updated_at IS NULL"
        operator: ">"
        value: 0
      - query: "SELECT COUNT(*) FROM users WHERE password_hash = 'MIGRATION_PLACEHOLDER'"
        operator: "="
        value: 0  # Ensure no existing placeholder values
    type: TRANSFORM
    reversible: true
    reverseSQL: |
      UPDATE users 
      SET password_hash = NULL,
          password_updated_at = NULL
      WHERE password_hash = 'MIGRATION_PLACEHOLDER'
```

### Pattern 11: Rollback-Safe Migration

**Use Case**: Migration with comprehensive rollback support.

```yaml
dataMigrations:
  - name: user-status-migration
    description: "Migrate user status with rollback capability"
    sql: |
      UPDATE users 
      SET status = CASE 
        WHEN last_login_at > NOW() - INTERVAL '30 days' THEN 'active'
        WHEN last_login_at > NOW() - INTERVAL '90 days' THEN 'inactive'
        ELSE 'dormant'
      END,
      status_updated_at = NOW()
      WHERE status IS NULL
    reversible: true
    reverseSQL: |
      UPDATE users 
      SET status = NULL,
          status_updated_at = NULL
      WHERE status_updated_at > NOW() - INTERVAL '1 hour'
    type: TRANSFORM
    batchSize: 10000
```

## Error Handling Patterns

### Pattern 12: Migration with Error Recovery

**Use Case**: Handling potential errors gracefully.

```yaml
dataMigrations:
  - name: data-validation-migration
    description: "Clean and validate email addresses"
    sql: |
      UPDATE users 
      SET email = LOWER(TRIM(email)),
          email_validated = true
      WHERE email IS NOT NULL
        AND email_validated IS NOT TRUE
        AND email ~ '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'
    conditions:
      - query: "SELECT COUNT(*) FROM users WHERE email IS NOT NULL AND email_validated IS NOT TRUE"
        operator: ">"
        value: 0
    type: TRANSFORM
    batchSize: 5000
    timeout: 30m
```

## Template Library

### Reusable Templates

#### Email Normalization Template
```yaml
template:
  template: |
    UPDATE {{.table_name}}
    SET {{.email_column}} = {{.email_column | lower | trim}}
    WHERE {{.email_column}} IS NOT NULL
      AND {{.email_column}} != {{.email_column | lower | trim}}
  parameters:
    - name: email_column
      type: column
      required: true
```

#### Status Migration Template
```yaml
template:
  template: |
    UPDATE {{.table_name}}
    SET {{.status_column}} = {{.new_status | quote}}
    WHERE {{.condition_column}} {{.operator}} {{.condition_value | quote}}
      AND {{.status_column}} IS {{.null_check}}
  parameters:
    - name: status_column
      type: column
      required: true
    - name: new_status
      type: string
      required: true
    - name: condition_column
      type: column
      required: true
    - name: operator
      type: string
      default: "="
    - name: condition_value
      type: string
      required: true
    - name: null_check
      type: string
      default: "NULL"
```

## Migration Testing Patterns

### Pattern 13: Test-Safe Migration

**Use Case**: Migration that can be safely tested in development.

```yaml
dataMigrations:
  - name: test-safe-migration
    description: "Migration designed for safe testing"
    sql: |
      UPDATE users 
      SET test_migration_flag = true,
          test_migration_at = NOW()
      WHERE id IN (
        SELECT id FROM users 
        WHERE email LIKE '%@example.com'  -- Test data only
        LIMIT 100                         -- Limit scope
      )
    conditions:
      - query: "SELECT COUNT(*) FROM users WHERE email LIKE '%@example.com'"
        operator: ">"
        value: 0
    type: CUSTOM
    reversible: true
    reverseSQL: |
      UPDATE users 
      SET test_migration_flag = NULL,
          test_migration_at = NULL
      WHERE test_migration_flag = true
        AND test_migration_at > NOW() - INTERVAL '1 hour'
```

## Production Patterns

### Pattern 14: Enterprise-Scale Migration

**Use Case**: Production migration for large enterprise datasets.

```yaml
dataMigrations:
  - name: enterprise-user-migration
    description: "Migrate 10M+ user records with performance optimization"
    sql: |
      UPDATE users 
      SET normalized_email = LOWER(TRIM(email)),
          migration_batch = {{.batch_id}},
          migrated_at = NOW()
      WHERE normalized_email IS NULL
        AND email IS NOT NULL
        AND id BETWEEN {{.start_id}} AND {{.end_id}}
    template:
      parameters:
        - name: batch_id
          type: integer
          required: true
        - name: start_id
          type: integer
          required: true
        - name: end_id
          type: integer
          required: true
    type: TRANSFORM
    batchSize: 25000        # Large batches for performance
    batchDelayMs: 1000      # 1 second delay to reduce load
    timeout: 4h             # Generous timeout
    priority: 3
```

## Monitoring and Observability Patterns

### Pattern 15: Migration with Comprehensive Monitoring

**Use Case**: Migration with detailed progress tracking and monitoring.

```yaml
dataMigrations:
  - name: monitored-migration
    description: "Migration with comprehensive monitoring and logging"
    sql: |
      UPDATE user_analytics 
      SET last_computed = NOW(),
          analytics_version = 'v2.1',
          computation_duration = EXTRACT(EPOCH FROM (NOW() - created_at))
      WHERE last_computed IS NULL 
        OR analytics_version != 'v2.1'
    conditions:
      - query: "SELECT COUNT(*) FROM user_analytics WHERE last_computed IS NULL OR analytics_version != 'v2.1'"
        operator: ">"
        value: 100
    type: TRANSFORM
    batchSize: 10000
    timeout: 2h
    tags: ["monitoring", "analytics", "v2.1"]
```

## Common Anti-Patterns (What NOT to Do)

### ❌ Anti-Pattern 1: Mass Update Without WHERE
```yaml
# DON'T DO THIS
dataMigrations:
  - name: dangerous-mass-update
    sql: "UPDATE users SET migrated = true"  # No WHERE clause!
```

### ❌ Anti-Pattern 2: No Batching for Large Tables
```yaml
# DON'T DO THIS
dataMigrations:
  - name: large-table-update
    sql: "UPDATE large_table SET processed = true WHERE processed IS NULL"
    # Missing batchSize for large table!
```

### ❌ Anti-Pattern 3: No Timeout for Long Operations
```yaml
# DON'T DO THIS
dataMigrations:
  - name: complex-calculation
    sql: "UPDATE users SET score = (complex calculation...)"
    # Missing timeout for potentially long operation!
```

## Database-Specific Best Practices

### PostgreSQL
- ✅ Use `RETURNING` clauses for feedback
- ✅ Leverage `JSONB` operations for flexible data
- ✅ Use `ON CONFLICT` for upsert operations
- ✅ Take advantage of `VACUUM ANALYZE` after large updates

### MySQL
- ✅ Use `INSERT ... ON DUPLICATE KEY UPDATE`
- ✅ Be aware of transaction isolation levels
- ✅ Use `LIMIT` clauses for batching
- ✅ Consider `utf8mb4` for full Unicode support

### SQLite
- ✅ Use `PRAGMA` statements for optimization
- ✅ Be aware of limited concurrency
- ✅ Use `VACUUM` after large deletions
- ✅ Consider WAL mode for better concurrency

### Cassandra
- ✅ Always include primary key in WHERE clauses
- ✅ Avoid cross-partition operations
- ✅ Use prepared statements with parameters
- ✅ Batch operations at application level

## Troubleshooting Common Issues

### Issue 1: Migration Timeouts
```yaml
# Solution: Increase timeout and reduce batch size
dataMigrations:
  - name: slow-migration
    sql: "UPDATE large_table SET complex_field = calculate_value()"
    batchSize: 1000        # Smaller batches
    batchDelayMs: 500      # More delay
    timeout: 4h            # Longer timeout
```

### Issue 2: Dependency Cycles
```yaml
# Solution: Remove circular dependencies
dataMigrations:
  - name: step-1
    sql: "UPDATE table1 SET flag = true"
    # dependsOn: ["step-2"]  # Remove this circular dependency
    
  - name: step-2
    sql: "UPDATE table2 SET flag = true"
    dependsOn: ["step-1"]    # Keep this valid dependency
```

### Issue 3: Failed Conditions
```yaml
# Solution: Add fallback logic or make conditions more flexible
dataMigrations:
  - name: flexible-migration
    sql: "UPDATE users SET status = 'active'"
    conditions:
      - query: "SELECT COUNT(*) FROM users WHERE status IS NULL"
        operator: ">="
        value: 1  # Use >= instead of exact match
```

## Performance Optimization Examples

### Optimized Large Table Update
```yaml
dataMigrations:
  - name: optimized-large-update
    description: "Efficiently update large table using indexes"
    sql: |
      UPDATE users 
      SET last_activity = activity_logs.last_seen
      FROM (
        SELECT user_id, MAX(created_at) as last_seen
        FROM activity_logs 
        WHERE created_at > NOW() - INTERVAL '30 days'
        GROUP BY user_id
      ) activity_logs
      WHERE users.id = activity_logs.user_id
        AND users.last_activity IS NULL
    type: TRANSFORM
    batchSize: 20000
    timeout: 3h
```

### Memory-Efficient Migration
```yaml
dataMigrations:
  - name: memory-efficient-migration
    description: "Process data in small chunks to minimize memory usage"
    sql: |
      UPDATE large_json_table 
      SET processed_data = json_extract(raw_data, '$.processed')
      WHERE processed_data IS NULL 
        AND id BETWEEN {{.start_id}} AND {{.end_id}}
    template:
      parameters:
        - name: start_id
          type: integer
          required: true
        - name: end_id
          type: integer
          required: true
    type: TRANSFORM
    batchSize: 5000        # Smaller batches for memory efficiency
    batchDelayMs: 2000     # Longer delay for garbage collection
```

This migration patterns guide provides a comprehensive set of examples for implementing data migrations safely and efficiently across different use cases and database types. 