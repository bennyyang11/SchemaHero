# Data Migration Troubleshooting Guide

This guide helps you diagnose and resolve common issues when working with SchemaHero data migrations.

## Table of Contents

1. [Common Error Messages](#common-error-messages)
2. [Migration Planning Issues](#migration-planning-issues)
3. [Execution Problems](#execution-problems)
4. [Performance Issues](#performance-issues)
5. [Database-Specific Issues](#database-specific-issues)
6. [Debugging Tools](#debugging-tools)

## Common Error Messages

### ❌ "Migration failed: condition not met"

**Cause**: A migration condition evaluated to false.

**Solution**:
```yaml
# Check your condition logic
dataMigrations:
  - name: conditional-migration
    sql: "UPDATE users SET status = 'active'"
    conditions:
      - query: "SELECT COUNT(*) FROM users WHERE status IS NULL"
        operator: ">"
        value: 0  # Make sure this matches your expectation
```

**Debug Steps**:
1. Run the condition query manually: `SELECT COUNT(*) FROM users WHERE status IS NULL`
2. Verify the operator and value are correct
3. Check if the condition should be `>=` instead of `>`

### ❌ "Circular dependency detected"

**Cause**: Migration dependencies form a cycle.

**Example Problem**:
```yaml
dataMigrations:
  - name: migration-a
    dependsOn: ["migration-b"]
  - name: migration-b  
    dependsOn: ["migration-a"]  # Creates cycle!
```

**Solution**:
```yaml
dataMigrations:
  - name: migration-a
    sql: "UPDATE table1 SET flag = true"
    # Remove circular dependency
  - name: migration-b
    sql: "UPDATE table2 SET flag = true"
    dependsOn: ["migration-a"]  # Valid linear dependency
```

### ❌ "Timeout exceeded"

**Cause**: Migration took longer than the specified timeout.

**Solution**:
```yaml
dataMigrations:
  - name: slow-migration
    sql: "UPDATE large_table SET computed_field = expensive_calculation()"
    batchSize: 5000      # Reduce batch size
    batchDelayMs: 1000   # Add delay between batches
    timeout: 4h          # Increase timeout
```

### ❌ "Batch size too large"

**Cause**: Batch size exceeds database or memory limits.

**Solution**:
```yaml
dataMigrations:
  - name: large-update
    sql: "UPDATE users SET processed = true"
    batchSize: 10000     # Reduce from larger value
    batchDelayMs: 500    # Add delay to reduce load
```

### ❌ "Template parameter missing"

**Cause**: Required template parameter not provided.

**Problem**:
```yaml
template:
  template: "UPDATE {{.table_name}} SET status = {{.new_status}}"
  # Missing new_status parameter!
```

**Solution**:
```yaml
template:
  template: "UPDATE {{.table_name}} SET status = {{.new_status}}"
  parameters:
    - name: new_status
      type: string
      required: true
      default: "active"
```

## Migration Planning Issues

### Issue: "No DDL or DML statements generated"

**Possible Causes**:
1. **Empty data migrations**: No migrations defined
2. **Failed conditions**: All migrations skipped due to conditions
3. **Invalid syntax**: Migrations rejected during validation

**Debug Steps**:
```bash
# Check planning with verbose output
kubectl schemahero plan --spec-file table.yaml --dry-run --verbose

# Validate your table spec
kubectl schemahero validate table.yaml
```

**Example Fix**:
```yaml
# Make sure you have actual migrations defined
dataMigrations:
  - name: example-migration
    sql: "UPDATE users SET migrated = true WHERE migrated IS NULL"
    # Don't leave this empty!
```

### Issue: "Invalid migration type"

**Problem**: Using unsupported migration type.

**Solution**:
```yaml
dataMigrations:
  - name: valid-migration
    sql: "UPDATE table SET flag = true"
    type: BACKFILL  # Use: BACKFILL, TRANSFORM, CLEANUP, COPY, or CUSTOM
```

### Issue: "Template rendering failed"

**Common template errors**:

```yaml
# ❌ Wrong syntax
template: "UPDATE {{table_name}} SET value = 1"  # Missing dots

# ✅ Correct syntax  
template: "UPDATE {{.table_name}} SET value = 1"

# ❌ Invalid function
template: "UPDATE users SET name = {{.name | invalid_function}}"

# ✅ Valid functions
template: "UPDATE users SET name = {{.name | quote | upper}}"
```

## Execution Problems

### Issue: "Migration stuck in RUNNING state"

**Possible Causes**:
1. **Database lock contention**
2. **Long-running query**
3. **Network connectivity issues**

**Debug Steps**:
```bash
# Check migration status
kubectl schemahero describe migration migration-name

# Check database connections
kubectl logs deployment/schemahero-manager

# Check for database locks
-- PostgreSQL
SELECT * FROM pg_stat_activity WHERE state = 'active';

-- MySQL  
SHOW PROCESSLIST;
```

**Solutions**:
```yaml
# Reduce batch size
dataMigrations:
  - name: stuck-migration
    sql: "UPDATE large_table SET processed = true"
    batchSize: 1000      # Reduce from larger value
    batchDelayMs: 2000   # Increase delay
```

### Issue: "Schema migration completed but data migration failed"

**Cause**: DDL succeeded but DML failed, leaving database in inconsistent state.

**Recovery Steps**:
```bash
# Check migration status
kubectl schemahero describe migration migration-name

# If migration supports rollback
kubectl schemahero rollback migration migration-name

# Manual recovery (if needed)
kubectl schemahero reject migration migration-name
# Then fix the issue and recreate
```

### Issue: "Concurrent migration detected"

**Cause**: Another migration is already running on the same table.

**Solution**:
```bash
# Check active migrations
kubectl schemahero get migrations --all-namespaces

# Wait for current migration to complete, or
# Cancel if it's stuck
kubectl schemahero reject migration stuck-migration-name
```

## Performance Issues

### Issue: "Migration taking too long"

**Optimization Strategies**:

```yaml
# Strategy 1: Optimize batch size
dataMigrations:
  - name: slow-migration
    sql: "UPDATE users SET computed = expensive_function(data)"
    batchSize: 2000      # Reduce batch size
    batchDelayMs: 1000   # Add delay to reduce database load
    timeout: 6h          # Increase timeout

# Strategy 2: Use indexed columns
dataMigrations:
  - name: indexed-migration
    sql: |
      UPDATE users 
      SET status = 'active' 
      WHERE created_at > '2023-01-01'  -- Use indexed timestamp
        AND status IS NULL

# Strategy 3: Break into smaller migrations
dataMigrations:
  - name: migration-part-1
    sql: "UPDATE users SET status = 'active' WHERE id BETWEEN 1 AND 100000"
  - name: migration-part-2
    sql: "UPDATE users SET status = 'active' WHERE id BETWEEN 100001 AND 200000"
    dependsOn: ["migration-part-1"]
```

### Issue: "Database performance degraded during migration"

**Monitoring and Mitigation**:

```yaml
# Use smaller batches with delays
dataMigrations:
  - name: gentle-migration
    sql: "UPDATE large_table SET processed = true"
    batchSize: 5000
    batchDelayMs: 5000   # 5 second delay between batches
    timeout: 12h         # Allow plenty of time
```

**Database-Specific Monitoring**:
```sql
-- PostgreSQL: Monitor active queries
SELECT query, state, query_start FROM pg_stat_activity;

-- MySQL: Check processlist
SHOW FULL PROCESSLIST;

-- Check for locks
SELECT * FROM information_schema.innodb_locks;
```

## Database-Specific Issues

### PostgreSQL Issues

#### Issue: "Connection pool exhausted"
```yaml
# Solution: Use connection-efficient patterns
dataMigrations:
  - name: connection-efficient
    sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
    batchSize: 15000     # Larger batches = fewer connections
    batchDelayMs: 500    # Shorter delay
```

#### Issue: "Lock wait timeout"
```sql
-- Check for blocking queries
SELECT 
  blocked_locks.pid AS blocked_pid,
  blocked_activity.usename AS blocked_user,
  blocking_locks.pid AS blocking_pid,
  blocking_activity.usename AS blocking_user,
  blocked_activity.query AS blocked_statement
FROM pg_catalog.pg_locks blocked_locks
JOIN pg_catalog.pg_stat_activity blocked_activity 
  ON blocked_activity.pid = blocked_locks.pid
JOIN pg_catalog.pg_locks blocking_locks 
  ON blocking_locks.locktype = blocked_locks.locktype;
```

### MySQL Issues

#### Issue: "Deadlock detected"
```yaml
# Solution: Order operations consistently
dataMigrations:
  - name: deadlock-safe
    sql: |
      UPDATE users u1
      JOIN accounts a1 ON u1.account_id = a1.id
      SET u1.status = 'active'
      WHERE u1.id < a1.id  -- Consistent ordering
      ORDER BY u1.id       -- Process in order
```

#### Issue: "MySQL syntax differences"
```yaml
# Use MySQL-specific adaptations
dataMigrations:
  - name: mysql-migration
    sql: |
      UPDATE users 
      SET full_name = CONCAT(first_name, ' ', last_name)  -- MySQL CONCAT()
      WHERE full_name IS NULL
```

### SQLite Issues

#### Issue: "Database locked"
```yaml
# Solution: Use smaller batches and delays
dataMigrations:
  - name: sqlite-safe
    sql: "UPDATE users SET processed = true"
    batchSize: 1000      # Small batches for SQLite
    batchDelayMs: 2000   # Longer delays
```

### Cassandra Issues

#### Issue: "Operation not supported"
```yaml
# ❌ Problematic Cassandra migration
dataMigrations:
  - name: cassandra-problem
    sql: "UPDATE users SET status = 'active'"  # Missing WHERE with primary key

# ✅ Cassandra-compatible migration
dataMigrations:
  - name: cassandra-safe
    sql: "UPDATE users SET status = 'active' WHERE user_id = ?"
    # Always include primary key in WHERE clause
```

## Debugging Tools

### CLI Debugging Commands

```bash
# Verbose planning to see what's generated
kubectl schemahero plan --spec-file table.yaml --dry-run --verbose --show-metrics

# Check migration details
kubectl schemahero describe migration migration-name

# List all migrations with status
kubectl schemahero get migrations --all-namespaces

# Check logs
kubectl logs deployment/schemahero-manager -f
```

### Database Debugging Queries

#### PostgreSQL Debugging
```sql
-- Check active migrations
SELECT * FROM pg_stat_activity WHERE application_name LIKE '%schemahero%';

-- Monitor table locks
SELECT * FROM pg_locks WHERE relation = 'your_table_name'::regclass;

-- Check migration performance
SELECT 
  schemaname,
  tablename,
  n_tup_upd,
  n_tup_del,
  last_autovacuum
FROM pg_stat_user_tables 
WHERE tablename = 'your_table';
```

#### MySQL Debugging
```sql
-- Check migration queries
SHOW FULL PROCESSLIST;

-- Monitor InnoDB status
SHOW ENGINE INNODB STATUS;

-- Check table statistics
SELECT * FROM information_schema.table_statistics 
WHERE table_name = 'your_table';
```

### YAML Validation

```bash
# Validate YAML syntax
kubectl apply --dry-run=client -f table.yaml

# Check SchemaHero-specific validation
kubectl schemahero validate table.yaml
```

## Recovery Procedures

### Stuck Migration Recovery

```bash
# Step 1: Identify the stuck migration
kubectl schemahero get migrations

# Step 2: Check migration details
kubectl schemahero describe migration stuck-migration

# Step 3: Cancel if necessary
kubectl schemahero reject migration stuck-migration

# Step 4: Fix the issue and retry
kubectl apply -f fixed-table.yaml
```

### Failed Migration Recovery

```bash
# Step 1: Check failure reason
kubectl schemahero describe migration failed-migration

# Step 2: Review logs
kubectl logs deployment/schemahero-manager --tail=100

# Step 3: Rollback if supported
kubectl schemahero rollback migration failed-migration

# Step 4: Manual cleanup if needed
kubectl exec -it postgres-pod -- psql -c "SELECT * FROM migration_status"
```

### Data Consistency Check

```sql
-- Verify migration completed correctly
SELECT 
  COUNT(*) as total_rows,
  COUNT(CASE WHEN status IS NOT NULL THEN 1 END) as migrated_rows,
  COUNT(CASE WHEN status IS NULL THEN 1 END) as remaining_rows
FROM users;

-- Check for partial migrations
SELECT DISTINCT status FROM users;
```

## Best Practices for Troubleshooting

### 1. Enable Verbose Logging
```yaml
# Add verbose logging to migrations
dataMigrations:
  - name: debug-migration
    description: "Migration with detailed logging for troubleshooting"
    sql: "UPDATE users SET debug_flag = true WHERE id = 123"
    tags: ["debug", "troubleshooting"]
```

### 2. Test in Development First
```bash
# Always test in development environment
kubectl schemahero plan --spec-file table.yaml --dry-run --verbose

# Test with small datasets
kubectl schemahero apply --spec-file table.yaml --dry-run
```

### 3. Monitor Resource Usage
```bash
# Monitor during execution
kubectl top pods
kubectl describe migration migration-name
```

### 4. Use Incremental Approach
```yaml
# Start with small scope
dataMigrations:
  - name: incremental-test
    sql: "UPDATE users SET status = 'active' WHERE id BETWEEN 1 AND 1000"
    # Test with subset first
```

## Error Prevention Checklist

### Before Creating Migrations:
- [ ] **Review SQL syntax** for target database
- [ ] **Test queries** against development database
- [ ] **Estimate affected rows** with COUNT queries
- [ ] **Plan for rollback** if migration is reversible
- [ ] **Set appropriate timeouts** based on data size

### Before Applying Migrations:
- [ ] **Run dry-run** to preview changes
- [ ] **Check migration dependencies** are satisfied
- [ ] **Verify database resources** are available
- [ ] **Schedule during maintenance window** for large migrations
- [ ] **Have rollback plan** ready

### During Migration Execution:
- [ ] **Monitor progress** with describe commands
- [ ] **Watch database performance** metrics
- [ ] **Check application health** if database is in use
- [ ] **Be ready to cancel** if issues arise

### After Migration Completion:
- [ ] **Verify data consistency** with validation queries
- [ ] **Check application functionality** end-to-end
- [ ] **Monitor performance** for any degradation
- [ ] **Document lessons learned** for future migrations

## Getting Help

### Self-Help Resources
1. **Check this troubleshooting guide** for common issues
2. **Review [Migration Patterns](migration-patterns.md)** for examples
3. **Read [Database-Specific Guides](database-guides/)** for your database
4. **Examine [API Documentation](data-migrations-api.md)** for field details

### Community Support
1. **GitHub Issues**: Report bugs with detailed error messages
2. **GitHub Discussions**: Ask questions and get community help
3. **Discord/Slack**: Real-time help from community members
4. **Documentation**: Contribute fixes and improvements

### Creating Effective Bug Reports

When reporting issues, include:

```
**Environment:**
- SchemaHero version: vX.Y.Z
- Kubernetes version: vX.Y.Z
- Database type and version: PostgreSQL 14.2
- Deployment method: Helm/kubectl/operator

**Migration Spec:**
```yaml
# Include your table spec here
```

**Error Message:**
```
# Include full error message and stack trace
```

**Logs:**
```
# Include relevant kubectl logs
```

**Expected vs Actual:**
- Expected: Migration should complete successfully
- Actual: Migration fails with timeout

**Reproduction Steps:**
1. Create table spec with data migrations
2. Run kubectl schemahero plan
3. Apply migration
4. Observe timeout error
```

This structured approach helps maintainers quickly understand and resolve your issue. 