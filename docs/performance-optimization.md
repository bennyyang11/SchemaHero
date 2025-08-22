# Performance Optimization Guide

This guide provides comprehensive strategies for optimizing data migration performance at enterprise scale.

## Table of Contents

1. [Performance Overview](#performance-overview)
2. [Intelligent Batching](#intelligent-batching)
3. [Memory Optimization](#memory-optimization)
4. [Parallel Execution](#parallel-execution)
5. [Database-Specific Optimizations](#database-specific-optimizations)
6. [Monitoring and Metrics](#monitoring-and-metrics)

## Performance Overview

### Key Performance Factors

1. **Batch Size**: Number of rows processed per operation
2. **Memory Usage**: RAM consumption during migration
3. **Connection Management**: Database connection efficiency
4. **Parallel Execution**: Running independent migrations simultaneously
5. **Database Load**: Impact on database performance

### Performance Targets

| Migration Size | Target Performance | Recommended Batch Size |
|-----------------|-------------------|----------------------|
| **Small** (<10K rows) | Complete in <5 minutes | Process all at once |
| **Medium** (10K-100K rows) | Complete in <30 minutes | 5,000-10,000 rows |
| **Large** (100K-1M rows) | Complete in <2 hours | 25,000-50,000 rows |
| **Enterprise** (1M+ rows) | Complete in <8 hours | 50,000-100,000 rows |

## Intelligent Batching

### Automatic Batch Size Optimization

SchemaHero automatically determines optimal batch sizes based on:

```yaml
dataMigrations:
  - name: auto-optimized-migration
    sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
    # BatchSize automatically calculated based on:
    # - Table size (row count)
    # - Migration complexity (JOINs, calculations)
    # - Database type (performance characteristics)
    # - Historical performance data
    type: BACKFILL
```

### Manual Batch Tuning

Override automatic batching for specific requirements:

```yaml
# Small batches for complex operations
dataMigrations:
  - name: complex-calculation
    sql: |
      UPDATE users 
      SET score = (
        SELECT AVG(rating) FROM reviews WHERE user_id = users.id
      ) * complexity_factor
    batchSize: 1000        # Small batches for complex subqueries
    batchDelayMs: 2000     # Longer delay for database recovery
    
# Large batches for simple operations
dataMigrations:
  - name: simple-flag-update
    sql: "UPDATE users SET migrated = true WHERE migrated IS NULL"
    batchSize: 100000      # Large batches for simple updates
    batchDelayMs: 500      # Short delay for fast processing
```

### Dynamic Batch Adjustment

```yaml
# Template-based batch sizing
dataMigrations:
  - name: dynamic-batching
    template:
      template: |
        UPDATE {{.table_name}} 
        SET processed = true 
        WHERE id BETWEEN {{.start_id}} AND {{.end_id}}
      parameters:
        - name: start_id
          type: integer
        - name: end_id
          type: integer
    # Batch size adjusted automatically based on ID range
    batchSize: 0  # 0 = auto-calculate
```

## Memory Optimization

### Memory-Efficient Migration Patterns

```yaml
# Streaming mode for large datasets
dataMigrations:
  - name: memory-efficient-migration
    sql: |
      UPDATE large_json_table 
      SET processed_data = parse_json_field(raw_data)
      WHERE processed_data IS NULL
    batchSize: 2000        # Small batches to limit memory usage
    batchDelayMs: 3000     # Longer delay for garbage collection
    # Automatically enables streaming mode for large JSON processing
```

### Memory Management Best Practices

#### 1. Optimize JSON/JSONB Operations
```sql
-- ❌ Memory-intensive: loads entire JSON into memory
UPDATE users SET settings = jsonb_set(settings, '{theme}', '"dark"');

-- ✅ Memory-efficient: targeted JSON updates
UPDATE users SET settings = settings || '{"theme": "dark"}' 
WHERE settings->>'theme' IS NULL;
```

#### 2. Use Efficient Data Types
```yaml
# Choose efficient data types for temporary calculations
dataMigrations:
  - name: efficient-calculation
    sql: |
      UPDATE orders 
      SET total_cents = (amount * 100)::bigint  -- Use integer for money calculations
      WHERE total_cents IS NULL
```

#### 3. Limit Result Set Processing
```sql
-- ❌ Processes all columns
UPDATE users SET updated_at = NOW() FROM user_profiles WHERE users.id = user_profiles.user_id;

-- ✅ Processes only needed columns  
UPDATE users SET updated_at = NOW() 
WHERE id IN (SELECT user_id FROM user_profiles);
```

## Parallel Execution

### Safe Parallel Migration Patterns

```yaml
# Multiple independent table migrations can run in parallel
---
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: users-parallel-migration
spec:
  dataMigrations:
    - name: users-status-update
      sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
      # Can run in parallel with orders migration

---
apiVersion: schemas.schemahero.io/v1alpha4  
kind: Table
metadata:
  name: orders-parallel-migration
spec:
  dataMigrations:
    - name: orders-status-update
      sql: "UPDATE orders SET processed = true WHERE processed IS NULL"
      # Can run in parallel with users migration
```

### Sequential Dependencies

```yaml
# Migrations with dependencies run sequentially
dataMigrations:
  - name: create-profiles
    sql: "INSERT INTO profiles (user_id) SELECT id FROM users WHERE profile_id IS NULL"
    
  - name: link-profiles
    sql: "UPDATE users SET profile_id = p.id FROM profiles p WHERE users.id = p.user_id"
    dependsOn: ["create-profiles"]  # Must wait for profiles to be created
```

### Parallel Execution Configuration

```yaml
# Configure parallel execution limits per database type
# PostgreSQL/MySQL: Up to 4 parallel migrations
# SQLite: 1 migration (single-threaded)
# Cassandra: Up to 3 parallel migrations
# RQLite: Up to 2 parallel migrations
```

## Database-Specific Optimizations

### PostgreSQL Performance

```yaml
dataMigrations:
  - name: postgres-optimized
    sql: |
      UPDATE users 
      SET normalized_email = LOWER(email),
          updated_at = NOW()
      WHERE normalized_email IS NULL
      RETURNING id, normalized_email  -- Use RETURNING for feedback
    batchSize: 50000               # Large batches for PostgreSQL
    batchDelayMs: 500              # Short delay
    # PostgreSQL handles large batches efficiently
```

**PostgreSQL Tips:**
- ✅ Use `RETURNING` clauses for verification
- ✅ Leverage `ON CONFLICT` for upserts
- ✅ Use `EXPLAIN ANALYZE` to optimize queries
- ✅ Consider `VACUUM ANALYZE` after large updates

### MySQL Performance

```yaml
dataMigrations:
  - name: mysql-optimized
    sql: |
      UPDATE users 
      SET full_name = CONCAT(first_name, ' ', last_name)
      WHERE full_name IS NULL
    batchSize: 25000               # Moderate batches for MySQL
    batchDelayMs: 1000             # Moderate delay
    # MySQL benefits from moderate batch sizes
```

**MySQL Tips:**
- ✅ Use `INSERT ... ON DUPLICATE KEY UPDATE` for upserts
- ✅ Monitor `SHOW PROCESSLIST` during execution
- ✅ Consider `innodb_buffer_pool_size` for large operations
- ✅ Use `OPTIMIZE TABLE` after large deletions

### SQLite Performance

```yaml
dataMigrations:
  - name: sqlite-optimized
    sql: "UPDATE users SET processed = true WHERE processed IS NULL"
    batchSize: 5000                # Small batches for SQLite
    batchDelayMs: 2000             # Longer delay for file I/O
    # SQLite is single-threaded, optimize for file I/O
```

**SQLite Tips:**
- ✅ Use `PRAGMA journal_mode=WAL` for better concurrency
- ✅ Enable `PRAGMA synchronous=NORMAL` for performance
- ✅ Use `VACUUM` after large deletions
- ✅ Consider `PRAGMA cache_size` for memory tuning

### Cassandra Performance

```yaml
dataMigrations:
  - name: cassandra-optimized
    sql: "UPDATE users SET status = 'active' WHERE user_id = ? AND partition_key = ?"
    batchSize: 500                 # Very small batches for Cassandra
    batchDelayMs: 5000             # Long delay for distributed consistency
    # Cassandra requires primary key in WHERE clauses
```

**Cassandra Tips:**
- ✅ Always include partition key in WHERE clauses
- ✅ Use prepared statements with parameters
- ✅ Avoid cross-partition operations
- ✅ Consider eventual consistency in timing

## Performance Monitoring

### Key Performance Metrics

```yaml
# Monitor these metrics during migration execution
metrics:
  - migration_duration_seconds
  - migration_rows_processed_per_second
  - migration_memory_usage_mb
  - migration_batch_duration_seconds
  - migration_errors_per_hour
  - database_connection_pool_usage
```

### Performance Dashboards

#### Migration Performance Dashboard
```yaml
dashboard_metrics:
  - active_migrations_count
  - migration_queue_depth
  - average_migration_duration
  - total_rows_processed_today
  - migration_success_rate
  - database_performance_impact
```

#### Real-time Monitoring
```bash
# Monitor active migrations
kubectl schemahero get migrations --watch

# Check migration progress
kubectl schemahero describe migration migration-name

# Monitor database performance impact
kubectl top pods | grep postgres
```

## Performance Tuning Strategies

### Strategy 1: Time-Based Optimization

```yaml
# Schedule large migrations during off-peak hours
dataMigrations:
  - name: off-peak-migration
    sql: "UPDATE large_table SET normalized = normalize_data(raw_data)"
    batchSize: 100000
    # Recommended execution: 2 AM - 5 AM UTC
    # Use deployment scheduling to run during maintenance windows
```

### Strategy 2: Resource-Based Optimization

```yaml
# Adjust based on available resources
dataMigrations:
  - name: resource-adaptive
    sql: "UPDATE users SET computed_field = expensive_calculation(data)"
    # Small batches during business hours
    batchSize: 5000
    batchDelayMs: 10000  # 10 second delay
    
    # OR large batches during maintenance windows  
    # batchSize: 50000
    # batchDelayMs: 1000
```

### Strategy 3: Progressive Optimization

```yaml
# Start with small batches, increase as confidence grows
dataMigrations:
  - name: progressive-migration
    sql: "UPDATE critical_table SET migrated = true"
    batchSize: 1000      # Start small
    # Monitor performance, then increase batch size in subsequent runs
    
  - name: progressive-migration-phase-2
    sql: "UPDATE critical_table SET migrated = true WHERE migrated IS NULL"
    batchSize: 10000     # Increased after successful phase 1
    dependsOn: ["progressive-migration"]
```

## Performance Testing

### Load Testing Scripts

```bash
#!/bin/bash
# Performance test script for large migrations

# 1. Baseline performance
kubectl schemahero plan --spec-file large-migration.yaml --show-metrics

# 2. Execute with monitoring
kubectl schemahero apply --spec-file large-migration.yaml --show-progress &

# 3. Monitor resource usage
while kubectl get migration large-migration-abc123; do
  kubectl top pods
  sleep 30
done

# 4. Collect performance metrics
kubectl schemahero describe migration large-migration-abc123
```

### Performance Benchmarks

```yaml
# Benchmark migration for different batch sizes
dataMigrations:
  - name: benchmark-small-batch
    sql: "UPDATE benchmark_table SET processed = true"
    batchSize: 1000
    tags: ["benchmark", "small-batch"]
    
  - name: benchmark-medium-batch  
    sql: "UPDATE benchmark_table SET processed = true"
    batchSize: 10000
    tags: ["benchmark", "medium-batch"]
    
  - name: benchmark-large-batch
    sql: "UPDATE benchmark_table SET processed = true" 
    batchSize: 100000
    tags: ["benchmark", "large-batch"]
```

## Troubleshooting Performance Issues

### Issue: Migration Running Slowly

**Diagnosis:**
```bash
# Check migration progress
kubectl schemahero describe migration slow-migration

# Check database performance
kubectl exec postgres-pod -- psql -c "SELECT * FROM pg_stat_activity;"

# Check resource usage
kubectl top pods
```

**Solutions:**
```yaml
# Reduce batch size
dataMigrations:
  - name: slow-migration-optimized
    sql: "UPDATE large_table SET computed = calculate(data)"
    batchSize: 5000      # Reduced from larger value
    batchDelayMs: 2000   # Increased delay
```

### Issue: High Memory Usage

**Solutions:**
```yaml
# Enable streaming mode for large operations
dataMigrations:
  - name: memory-efficient
    sql: "UPDATE users SET data = process_large_field(raw_data)"
    batchSize: 1000      # Very small batches
    batchDelayMs: 5000   # Long delay for memory recovery
    # Automatically enables streaming for large field processing
```

### Issue: Database Performance Impact

**Solutions:**
```yaml
# Minimize database impact
dataMigrations:
  - name: gentle-migration
    sql: "UPDATE users SET last_migration = NOW()"
    batchSize: 2000
    batchDelayMs: 10000  # 10 second delay between batches
    timeout: 24h         # Allow plenty of time
    # Schedule during maintenance windows
```

## Advanced Performance Patterns

### Pattern 1: Graduated Batch Sizing

```yaml
# Start small, increase batch size as confidence grows
dataMigrations:
  - name: graduated-phase-1
    sql: "UPDATE users SET migrated = true WHERE id BETWEEN 1 AND 10000"
    batchSize: 1000      # Small batches for initial testing
    
  - name: graduated-phase-2
    sql: "UPDATE users SET migrated = true WHERE id BETWEEN 10001 AND 100000"
    batchSize: 10000     # Larger batches after successful phase 1
    dependsOn: ["graduated-phase-1"]
    
  - name: graduated-phase-3
    sql: "UPDATE users SET migrated = true WHERE id > 100000"
    batchSize: 50000     # Largest batches for bulk processing
    dependsOn: ["graduated-phase-2"]
```

### Pattern 2: Index-Optimized Batching

```yaml
# Optimize batching based on database indexes
dataMigrations:
  - name: index-optimized
    sql: |
      UPDATE users 
      SET updated_at = NOW()
      WHERE created_at BETWEEN '2023-01-01' AND '2023-12-31'  -- Use indexed timestamp
        AND updated_at IS NULL
    batchSize: 25000
    # Batching works efficiently with indexed WHERE clauses
```

### Pattern 3: Resource-Aware Scheduling

```yaml
# Adjust performance based on system resources
dataMigrations:
  - name: resource-aware
    sql: "UPDATE large_table SET processed = true"
    # Performance varies by deployment:
    # - Development: batchSize: 1000, batchDelayMs: 5000
    # - Staging: batchSize: 10000, batchDelayMs: 2000  
    # - Production: batchSize: 50000, batchDelayMs: 1000
    batchSize: 10000
    batchDelayMs: 2000
```

## Connection Pool Optimization

### Database Connection Limits

```yaml
# Configure connection pools per database type
connection_pools:
  postgres:
    max_connections: 20      # PostgreSQL handles connections well
    connection_timeout: 30s
    
  mysql:
    max_connections: 15      # MySQL has lower default limits
    connection_timeout: 30s
    
  sqlite:
    max_connections: 1       # SQLite is single-threaded
    connection_timeout: 60s
    
  cassandra:
    max_connections: 10      # Cassandra cluster connections
    connection_timeout: 45s
```

### Connection Pool Monitoring

```bash
# Monitor connection usage
kubectl schemahero get migrations --show-connections

# Check for connection exhaustion
kubectl logs deployment/schemahero-manager | grep "connection"
```

## Performance Testing Framework

### Automated Performance Tests

```yaml
# Performance test suite
performance_tests:
  - name: small_dataset_performance
    table_size: 10000
    expected_duration: 5m
    expected_throughput: 50_rows_per_second
    
  - name: medium_dataset_performance  
    table_size: 100000
    expected_duration: 30m
    expected_throughput: 100_rows_per_second
    
  - name: large_dataset_performance
    table_size: 1000000
    expected_duration: 2h
    expected_throughput: 200_rows_per_second
```

### Performance Regression Testing

```bash
#!/bin/bash
# Performance regression test script

# Run standard benchmark migration
kubectl schemahero apply --spec-file benchmark-migration.yaml

# Collect performance metrics
DURATION=$(kubectl schemahero describe migration benchmark | grep Duration)
THROUGHPUT=$(kubectl schemahero describe migration benchmark | grep Throughput) 

# Compare with baseline performance
if [ "$DURATION" -gt "$BASELINE_DURATION" ]; then
  echo "PERFORMANCE REGRESSION: Duration increased"
  exit 1
fi

echo "PERFORMANCE TEST PASSED"
```

## Monitoring Integration

### Prometheus Metrics

```yaml
# Key metrics to monitor
prometheus_metrics:
  - schemahero_data_migrations_total
  - schemahero_data_migration_duration_seconds  
  - schemahero_data_migration_rows_processed_total
  - schemahero_active_data_migrations
  - schemahero_migration_queue_depth
  - schemahero_data_migration_errors_total
```

### Grafana Dashboard Example

```yaml
# Grafana dashboard configuration
dashboard:
  title: "SchemaHero Data Migrations"
  panels:
    - title: "Active Migrations"
      metric: schemahero_active_data_migrations
      type: gauge
      
    - title: "Migration Duration"
      metric: schemahero_data_migration_duration_seconds
      type: histogram
      
    - title: "Rows Processed"
      metric: schemahero_data_migration_rows_processed_total
      type: counter
      
    - title: "Error Rate"
      metric: rate(schemahero_data_migration_errors_total[5m])
      type: graph
```

### Alerting Rules

```yaml
# Prometheus alerting rules
alerting_rules:
  - alert: DataMigrationStuck
    expr: schemahero_data_migration_duration_seconds > 7200  # 2 hours
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "Data migration running longer than expected"
      
  - alert: DataMigrationFailed
    expr: increase(schemahero_data_migration_errors_total[1h]) > 0
    for: 1m
    labels:
      severity: critical
    annotations:
      summary: "Data migration execution failed"
      
  - alert: HighMigrationErrorRate
    expr: rate(schemahero_data_migration_errors_total[5m]) > 0.1
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: "High error rate in data migrations"
```

## Enterprise Scale Optimization

### Multi-Region Deployment

```yaml
# Optimize for multi-region deployments
dataMigrations:
  - name: region-aware-migration
    sql: "UPDATE users SET region = get_user_region(ip_address) WHERE region IS NULL"
    batchSize: 10000
    # Consider network latency between regions
    batchDelayMs: 3000
    timeout: 6h
```

### High Availability Considerations

```yaml
# Design migrations for high availability
dataMigrations:
  - name: ha-safe-migration
    sql: |
      UPDATE users 
      SET status = 'active'
      WHERE status IS NULL
        AND last_login > NOW() - INTERVAL '30 days'
    batchSize: 15000
    # Safe for read replicas and failover scenarios
    reversible: true
    reverseSQL: "UPDATE users SET status = NULL WHERE status = 'active' AND updated_at > NOW() - INTERVAL '1 hour'"
```

### Load Balancer Considerations

```yaml
# Migrations that work well with load balancers
dataMigrations:
  - name: load-balancer-safe
    sql: |
      UPDATE user_sessions 
      SET last_accessed = NOW()
      WHERE last_accessed < NOW() - INTERVAL '1 hour'
    batchSize: 5000
    # Session updates are safe during load balancer health checks
    batchDelayMs: 1000
```

This performance optimization guide provides the foundation for enterprise-scale data migration performance tuning and monitoring. 