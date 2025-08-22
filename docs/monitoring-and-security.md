# Monitoring and Security Guide

This guide covers production-ready monitoring, observability, and security for SchemaHero data migrations.

## Table of Contents

1. [Monitoring Overview](#monitoring-overview)
2. [Prometheus Metrics](#prometheus-metrics)
3. [Structured Logging](#structured-logging)
4. [Health Checks](#health-checks)
5. [Alerting](#alerting)
6. [Security Controls](#security-controls)
7. [Audit Logging](#audit-logging)
8. [RBAC Configuration](#rbac-configuration)

## Monitoring Overview

### Key Monitoring Areas

1. **Migration Execution**: Track migration progress and performance
2. **Database Impact**: Monitor database load and performance
3. **System Resources**: Track memory, CPU, and network usage
4. **Error Rates**: Monitor failures and success rates
5. **Security Events**: Audit access and changes

### Monitoring Architecture

```yaml
# Monitoring stack components
monitoring_stack:
  metrics:
    - prometheus: "Metrics collection and storage"
    - grafana: "Visualization and dashboards"
    
  logging:
    - structured_logs: "JSON-formatted application logs"
    - log_aggregation: "Centralized log collection"
    
  health_checks:
    - kubernetes_probes: "Liveness and readiness checks"
    - custom_endpoints: "Migration-specific health status"
    
  alerting:
    - prometheus_rules: "Metric-based alerting"
    - webhook_integration: "External notification systems"
```

## Prometheus Metrics

### Core Migration Metrics

```yaml
# Counter Metrics
metrics:
  schemahero_data_migrations_total:
    description: "Total number of data migrations executed"
    labels: ["database_type", "migration_type", "status"]
    
  schemahero_data_migration_rows_processed_total:
    description: "Total number of rows processed by data migrations"
    labels: ["database_type", "table_name", "migration_name"]
    
  schemahero_data_migration_errors_total:
    description: "Total number of data migration errors"
    labels: ["database_type", "error_type", "table_name"]

# Histogram Metrics  
  schemahero_data_migration_duration_seconds:
    description: "Duration of data migration execution"
    labels: ["database_type", "migration_type", "table_name"]
    buckets: [1, 5, 10, 30, 60, 300, 600, 1800, 3600]
    
  schemahero_data_migration_batch_duration_seconds:
    description: "Duration of individual migration batches"
    labels: ["database_type", "table_name"]
    buckets: [0.1, 0.5, 1, 2, 5, 10, 30]

# Gauge Metrics
  schemahero_active_data_migrations:
    description: "Number of currently active data migrations"
    labels: ["database_type"]
    
  schemahero_migration_queue_depth:
    description: "Number of migrations waiting to execute"
    labels: ["database_type"]
```

### Custom Metrics Queries

```promql
# Migration success rate over last 24 hours
rate(schemahero_data_migrations_total{status="completed"}[24h]) / 
rate(schemahero_data_migrations_total[24h]) * 100

# Average migration duration by database type
avg_over_time(schemahero_data_migration_duration_seconds[1h])

# Rows processed per second
rate(schemahero_data_migration_rows_processed_total[5m])

# Error rate percentage
rate(schemahero_data_migration_errors_total[5m]) / 
rate(schemahero_data_migrations_total[5m]) * 100
```

## Structured Logging

### Log Format

```json
{
  "timestamp": "2025-01-01T10:30:00Z",
  "level": "info",
  "event_type": "migration_started",
  "migration_name": "users-status-update",
  "table_name": "users",
  "database_type": "postgres",
  "migration_type": "BACKFILL",
  "estimated_rows": 150000,
  "batch_size": 10000,
  "timeout": "30m",
  "requested_by": "admin@company.com"
}
```

### Log Event Types

```yaml
log_events:
  migration_lifecycle:
    - migration_planned
    - migration_approved
    - migration_started
    - migration_paused
    - migration_resumed
    - migration_completed
    - migration_failed
    
  batch_execution:
    - batch_started
    - batch_completed
    - batch_failed
    
  security_events:
    - permission_check
    - approval_requested
    - approval_granted
    - sensitive_data_detected
    
  performance_events:
    - slow_batch_detected
    - memory_threshold_exceeded
    - connection_pool_exhausted
```

### Log Aggregation Example

```yaml
# Fluentd configuration for log aggregation
apiVersion: v1
kind: ConfigMap
metadata:
  name: fluentd-schemahero-config
data:
  fluent.conf: |
    <source>
      @type tail
      path /var/log/schemahero/*.log
      pos_file /var/log/fluentd/schemahero.log.pos
      tag schemahero.*
      format json
    </source>
    
    <filter schemahero.**>
      @type parser
      key_name message
      reserve_data true
      <parse>
        @type json
      </parse>
    </filter>
    
    <match schemahero.migration.**>
      @type elasticsearch
      host elasticsearch.monitoring.svc.cluster.local
      port 9200
      index_name schemahero-migrations
    </match>
```

## Health Checks

### Migration Health Endpoints

```yaml
# Health check endpoints
health_endpoints:
  /health:
    description: "Overall system health"
    response:
      healthy: true
      timestamp: 1672531800
      checks:
        - name: "database_connectivity"
          status: "passing"
        - name: "active_migrations"
          status: "passing"
          
  /health/migrations:
    description: "Migration-specific health"
    response:
      active_migrations: 2
      failed_migrations: 0
      stuck_migrations: 0
      overall_healthy: true
      
  /metrics:
    description: "Prometheus metrics endpoint"
    format: "prometheus_exposition_format"
```

### Kubernetes Health Probes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: schemahero-manager
spec:
  template:
    spec:
      containers:
      - name: manager
        image: schemahero/schemahero:latest
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
          
        readinessProbe:
          httpGet:
            path: /health/migrations
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
          
        # Expose metrics port
        ports:
        - containerPort: 8080
          name: health
        - containerPort: 8443
          name: metrics
```

## Alerting

### Critical Alert Rules

```yaml
# Prometheus alerting rules
groups:
- name: schemahero-data-migrations
  rules:
  
  # Migration stuck/timeout alerts
  - alert: DataMigrationStuck
    expr: schemahero_data_migration_duration_seconds > 7200  # 2 hours
    for: 5m
    labels:
      severity: warning
      component: schemahero
    annotations:
      summary: "Data migration {{ $labels.migration_name }} running longer than expected"
      description: "Migration on {{ $labels.table_name }} has been running for more than 2 hours"
      runbook_url: "https://docs.schemahero.io/troubleshooting#stuck-migrations"
      
  # Migration failure alerts  
  - alert: DataMigrationFailed
    expr: increase(schemahero_data_migration_errors_total[1h]) > 0
    for: 1m
    labels:
      severity: critical
      component: schemahero
    annotations:
      summary: "Data migration failed on {{ $labels.table_name }}"
      description: "Migration failed with error type: {{ $labels.error_type }}"
      runbook_url: "https://docs.schemahero.io/troubleshooting#failed-migrations"
      
  # High error rate alerts
  - alert: HighDataMigrationErrorRate
    expr: rate(schemahero_data_migration_errors_total[5m]) > 0.1
    for: 10m
    labels:
      severity: warning
      component: schemahero
    annotations:
      summary: "High error rate in data migrations"
      description: "Error rate: {{ $value | humanizePercentage }}"
      
  # Resource alerts
  - alert: DataMigrationHighMemoryUsage
    expr: schemahero_migration_memory_usage_mb > 2048  # 2GB
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "Data migration using high memory"
      description: "Memory usage: {{ $value }}MB"
      
  # Queue depth alerts
  - alert: DataMigrationQueueBacklog
    expr: schemahero_migration_queue_depth > 10
    for: 15m
    labels:
      severity: warning
    annotations:
      summary: "Large migration queue backlog"
      description: "{{ $value }} migrations queued for execution"
```

### Alert Notification Channels

```yaml
# AlertManager configuration
route:
  group_by: ['alertname', 'component']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'schemahero-alerts'
  
  routes:
  - match:
      severity: critical
    receiver: 'critical-alerts'
    
  - match:
      component: schemahero
    receiver: 'schemahero-team'

receivers:
- name: 'schemahero-alerts'
  slack_configs:
  - api_url: 'YOUR_SLACK_WEBHOOK_URL'
    channel: '#database-migrations'
    title: 'SchemaHero Alert'
    text: '{{ range .Alerts }}{{ .Annotations.summary }}{{ end }}'
    
- name: 'critical-alerts'
  pagerduty_configs:
  - service_key: 'YOUR_PAGERDUTY_KEY'
    description: '{{ range .Alerts }}{{ .Annotations.summary }}{{ end }}'
```

## Security Controls

### RBAC Configuration

```yaml
# Role-based access control for data migrations
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: schemahero-data-migration-admin
rules:
- apiGroups: ["schemas.schemahero.io"]
  resources: ["tables", "migrations"]
  verbs: ["get", "list", "create", "update", "patch", "delete"]
- apiGroups: ["schemas.schemahero.io"]
  resources: ["tables/status", "migrations/status"]
  verbs: ["get", "update", "patch"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: schemahero-data-migration-viewer
rules:
- apiGroups: ["schemas.schemahero.io"]
  resources: ["tables", "migrations"]
  verbs: ["get", "list"]
- apiGroups: ["schemas.schemahero.io"]
  resources: ["tables/status", "migrations/status"]
  verbs: ["get"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: schemahero-data-migration-executor
rules:
- apiGroups: ["schemas.schemahero.io"]
  resources: ["migrations"]
  verbs: ["get", "list", "update", "patch"]
- apiGroups: ["schemas.schemahero.io"]
  resources: ["migrations/status"]
  verbs: ["get", "update", "patch"]
```

### Role Assignments

```yaml
# Assign roles to users and service accounts
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: data-migration-admins
subjects:
- kind: User
  name: dba@company.com
  apiGroup: rbac.authorization.k8s.io
- kind: User
  name: migration-admin@company.com
  apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: schemahero-data-migration-admin
  apiGroup: rbac.authorization.k8s.io

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: data-migration-executors
subjects:
- kind: ServiceAccount
  name: schemahero-manager
  namespace: schemahero-system
roleRef:
  kind: ClusterRole
  name: schemahero-data-migration-executor
  apiGroup: rbac.authorization.k8s.io
```

## Audit Logging

### Audit Event Types

```yaml
audit_events:
  planning:
    - migration_planned
    - migration_validated
    - migration_estimated
    
  approval:
    - approval_requested
    - approval_granted
    - approval_denied
    - auto_approval_applied
    
  execution:
    - migration_started
    - batch_executed
    - migration_paused
    - migration_resumed
    - migration_completed
    - migration_failed
    
  security:
    - permission_check
    - sensitive_data_detected
    - unauthorized_access_attempt
    - migration_signature_verified
```

### Audit Log Format

```json
{
  "audit_id": "audit_1672531800_abc123",
  "timestamp": "2025-01-01T10:30:00Z",
  "event_type": "migration_executed",
  "actor": "admin@company.com",
  "migration_name": "users-status-update",
  "table_name": "users",
  "database_name": "production",
  "action": "execute",
  "source_ip": "10.0.1.100",
  "user_agent": "kubectl/v1.25.0",
  "details": {
    "rows_affected": 15000,
    "batch_size": 5000,
    "duration": "30s",
    "success": true
  },
  "sensitive": false,
  "risk_level": "medium"
}
```

### Audit Log Retention

```yaml
# Audit log retention policies
retention_policies:
  security_events:
    retention: "7 years"      # Compliance requirement
    encryption: "required"
    
  execution_events:
    retention: "2 years"      # Operational history
    encryption: "optional"
    
  planning_events:
    retention: "1 year"       # Development history
    encryption: "optional"
```

## Security Controls

### Sensitive Data Protection

```yaml
# Configure sensitive data detection
sensitive_data_patterns:
  credit_cards:
    pattern: '\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13})\b'
    action: "block"           # Block migrations with credit card numbers
    
  social_security:
    pattern: '\b\d{3}-\d{2}-\d{4}\b'
    action: "block"           # Block migrations with SSNs
    
  emails:
    pattern: '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}'
    action: "audit"           # Audit migrations with emails
    
  passwords:
    pattern: '(?i)(password|passwd|pwd)\s*[=:]\s*[''"][^''"]+[''"]'
    action: "block"           # Block password literals
```

### Data Masking Rules

```yaml
# Configure data masking for audit logs
masking_rules:
  email_masking:
    field_pattern: "email"
    mask_format: "***@***.***"
    preserve: "domain"        # Preserve domain for analysis
    
  id_masking:
    field_pattern: "user_id|customer_id"
    mask_format: "ID_***"
    preserve: "none"
    
  amount_masking:
    field_pattern: "amount|price|salary"
    mask_format: "$.XX"
    preserve: "currency"
```

### Migration Approval Workflow

```yaml
# Approval requirements based on migration characteristics
approval_rules:
  destructive_operations:
    condition: |
      migration.sql contains "DROP" OR 
      migration.sql contains "TRUNCATE" OR
      (migration.sql contains "DELETE" AND NOT migration.sql contains "WHERE") OR
      (migration.sql contains "UPDATE" AND NOT migration.sql contains "WHERE")
    required_approvers: 2
    approver_roles: ["migration-admin", "dba"]
    auto_approval: false
    
  large_datasets:
    condition: "estimated_rows > 100000"
    required_approvers: 1
    approver_roles: ["migration-admin"]
    auto_approval_after: "24h"
    
  production_environment:
    condition: "database_name = 'production' OR environment = 'prod'"
    required_approvers: 2
    approver_roles: ["migration-admin", "production-admin"]
    auto_approval: false
    
  sensitive_data:
    condition: "sensitive_data_detected = true"
    required_approvers: 2
    approver_roles: ["security-admin", "migration-admin"]
    auto_approval: false
```

### Migration Signing

```yaml
# Digital signing for migration integrity
signing_configuration:
  enabled: true
  signing_key: "path/to/private/key"
  verification_key: "path/to/public/key"
  
  required_for:
    - production_migrations
    - destructive_operations
    - sensitive_data_migrations
    
  signature_format:
    algorithm: "RSA-SHA256"
    encoding: "base64"
    
  verification:
    strict_mode: true       # Reject unsigned migrations
    grace_period: "0h"      # No grace period for production
```

## Dashboard Configuration

### Grafana Dashboard JSON

```json
{
  "dashboard": {
    "title": "SchemaHero Data Migrations",
    "tags": ["schemahero", "database", "migrations"],
    "panels": [
      {
        "title": "Active Migrations",
        "type": "stat",
        "targets": [{
          "expr": "sum(schemahero_active_data_migrations)"
        }]
      },
      {
        "title": "Migration Success Rate",
        "type": "stat", 
        "targets": [{
          "expr": "rate(schemahero_data_migrations_total{status=\"completed\"}[24h]) / rate(schemahero_data_migrations_total[24h]) * 100"
        }]
      },
      {
        "title": "Migration Duration",
        "type": "graph",
        "targets": [{
          "expr": "histogram_quantile(0.95, rate(schemahero_data_migration_duration_seconds_bucket[5m]))"
        }]
      },
      {
        "title": "Rows Processed",
        "type": "graph",
        "targets": [{
          "expr": "rate(schemahero_data_migration_rows_processed_total[5m])"
        }]
      }
    ]
  }
}
```

### Custom Dashboard Metrics

```yaml
# Additional dashboard panels
dashboard_panels:
  database_performance:
    title: "Database Performance Impact"
    metrics:
      - database_cpu_usage
      - database_memory_usage
      - database_connection_count
      - database_query_duration
      
  migration_queue:
    title: "Migration Queue Status"
    metrics:
      - migrations_queued
      - migrations_running
      - migrations_completed_today
      - migrations_failed_today
      
  security_overview:
    title: "Security and Compliance"
    metrics:
      - sensitive_data_detections
      - approval_requests_pending
      - audit_events_per_hour
      - unauthorized_access_attempts
```

## Security Best Practices

### 1. Principle of Least Privilege

```yaml
# Grant minimal required permissions
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: developer-migration-access
  namespace: development
rules:
- apiGroups: ["schemas.schemahero.io"]
  resources: ["tables"]
  verbs: ["get", "list", "create", "update"]  # No delete permission
- apiGroups: ["schemas.schemahero.io"] 
  resources: ["migrations"]
  verbs: ["get", "list"]                      # Read-only migration access
```

### 2. Environment Isolation

```yaml
# Separate RBAC for different environments
production_rbac:
  strict_approval: true
  required_approvers: 2
  auto_approval: false
  audit_retention: "7_years"
  
development_rbac:
  strict_approval: false
  required_approvers: 1
  auto_approval: true
  audit_retention: "90_days"
```

### 3. Secure Migration Patterns

```yaml
# Use parameterized queries for security
dataMigrations:
  - name: secure-migration
    template:
      template: |
        UPDATE {{.table_name | identifier}}
        SET {{.column_name | identifier}} = {{.value | quote}}
        WHERE {{.condition_column | identifier}} = {{.condition_value | quote}}
      parameters:
        - name: value
          type: string
          required: true
        - name: condition_value
          type: string
          required: true
```

### 4. Compliance and Governance

```yaml
# Compliance configuration
compliance_settings:
  gdpr_compliance:
    enabled: true
    data_retention_days: 2555    # 7 years
    audit_sensitive_data: true
    require_explicit_consent: true
    
  sox_compliance:
    enabled: true
    financial_data_controls: true
    segregation_of_duties: true
    audit_trail_integrity: true
    
  hipaa_compliance:
    enabled: false               # Enable for healthcare
    phi_detection: true
    encryption_required: true
    access_logging: "detailed"
```

## Monitoring Best Practices

### 1. Gradual Rollout Monitoring

```bash
# Monitor migration rollout across environments
kubectl schemahero get migrations -A --selector=environment=development
kubectl schemahero get migrations -A --selector=environment=staging  
kubectl schemahero get migrations -A --selector=environment=production
```

### 2. Performance Baseline Monitoring

```yaml
# Establish performance baselines
performance_baselines:
  small_tables:
    max_duration: "5m"
    min_throughput: "50_rows_per_second"
    
  medium_tables:
    max_duration: "30m"
    min_throughput: "100_rows_per_second"
    
  large_tables:
    max_duration: "2h"
    min_throughput: "200_rows_per_second"
```

### 3. Continuous Monitoring

```bash
#!/bin/bash
# Continuous monitoring script

while true; do
  # Check active migrations
  ACTIVE=$(kubectl schemahero get migrations --no-headers | grep -c "RUNNING")
  
  # Check for stuck migrations
  STUCK=$(kubectl schemahero get migrations --no-headers | awk '$5 > "2h" {print $1}')
  
  # Alert if issues detected
  if [ "$ACTIVE" -gt 5 ]; then
    echo "WARNING: $ACTIVE active migrations (threshold: 5)"
  fi
  
  if [ -n "$STUCK" ]; then
    echo "CRITICAL: Stuck migrations detected: $STUCK"
  fi
  
  sleep 300  # Check every 5 minutes
done
```

This monitoring and security guide provides enterprise-grade observability and security controls for production data migration operations. 