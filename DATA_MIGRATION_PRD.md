# PRD: Data Migration Support for SchemaHero

## Document Information
- **Author**: Development Team
- **Date**: January 2025
- **Version**: 1.0
- **Status**: Draft

---

## Executive Summary

Extend SchemaHero to support **data migrations** alongside existing schema migrations, enabling complete declarative database change management. This enhancement will allow users to define both structural (DDL) and data transformation (DML) operations within a single, unified workflow.

## Problem Statement

### Current State Analysis

SchemaHero successfully manages **schema migrations** through declarative YAML definitions, but has critical limitations:

**✅ What Works Today:**
- Declarative table schema definitions
- Automatic DDL generation (`CREATE TABLE`, `ALTER TABLE`, etc.)
- Migration approval workflow (plan → approve → execute)
- Basic seed data insertion for new tables
- Support for 7+ database types

**❌ Current Limitations:**
- No support for complex data transformations during migrations
- Limited to static INSERT operations via seed data
- No ability to UPDATE existing data based on business logic
- No support for calculated values or cross-table operations
- Users must maintain separate data migration tools

### Impact on Users

**Current Pain Points:**
1. **Operational Complexity**: Managing two separate migration systems
2. **Timing Coordination**: Manual orchestration of schema-then-data changes
3. **Inconsistent Workflows**: Different approval processes for schema vs data
4. **No Unified Rollback**: Cannot atomically rollback both schema and data changes
5. **Enterprise Adoption Blocker**: Incomplete solution prevents full GitOps adoption

### Success Criteria

**Primary Goals:**
- Single YAML definition handles both schema and data changes
- Unified approval workflow for complete migrations
- Support for common data transformation patterns
- Maintain existing safety guarantees and approval processes
- Preserve backward compatibility with current SchemaHero installations

## Requirements

### Functional Requirements

#### FR1: Data Migration Definition API
- **FR1.1**: Extend Table CRD to include data migration specifications
- **FR1.2**: Support imperative SQL statements for data transformations
- **FR1.3**: Enable template-based migrations with parameter substitution
- **FR1.4**: Support conditional execution based on data state
- **FR1.5**: Allow multi-step data migration sequences

#### FR2: Execution Control & Safety
- **FR2.1**: Execute schema changes before data migrations (dependency ordering)
- **FR2.2**: Support dry-run mode for data migrations
- **FR2.3**: Provide transaction-based rollback where database supports it
- **FR2.4**: Enable selective execution modes (schema-only, data-only, combined)
- **FR2.5**: Implement migration timeouts and resource limits

#### FR3: Database Compatibility
- **FR3.1**: Support all currently supported databases (PostgreSQL, MySQL, CockroachDB, SQLite, RQLite, TimescaleDB, Cassandra)
- **FR3.2**: Handle database-specific SQL syntax differences
- **FR3.3**: Respect database-specific transaction and locking behaviors
- **FR3.4**: Support database-specific data types and functions

#### FR4: Integration & Workflow
- **FR4.1**: Extend existing Migration CRD to include data migration statements
- **FR4.2**: Maintain existing CLI commands with enhanced capabilities
- **FR4.3**: Preserve current approval workflow and access controls
- **FR4.4**: Support migration dependencies and ordering
- **FR4.5**: Enable migration status tracking and reporting

### Non-Functional Requirements

#### NFR1: Safety & Reliability
- All data migrations must be reviewable before execution
- Support atomic operations where possible
- Implement comprehensive error handling and recovery
- Provide detailed logging and audit trails
- Support migration validation and testing

#### NFR2: Performance
- Handle large table transformations efficiently
- Support batched operations for performance
- Implement connection pooling and resource management
- Provide progress reporting for long-running migrations
- Support parallel execution where safe

#### NFR3: Observability & Monitoring
- Detailed migration execution logging
- Performance metrics and timing data
- Error reporting and troubleshooting information
- Integration with existing Kubernetes monitoring
- Status reporting through CRD status fields

#### NFR4: Security & Access Control
- Respect existing RBAC configurations
- Support principle of least privilege for data access
- Audit all data migration operations
- Secure handling of sensitive data transformations
- Integration with Kubernetes secrets management

## Technical Design

### API Design

#### Extended Table CRD Specification

```yaml
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: users-migration-v2
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
        - name: created_at_tz
          type: "timestamp with time zone"
  dataMigrations:
    - name: "add-default-status"
      description: "Set default status for existing users"
      sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
      conditions:
        - "SELECT COUNT(*) FROM users WHERE status IS NULL"
        - operator: ">"
        - value: 0
    - name: "convert-timezone"
      description: "Convert created_at to timezone-aware timestamp"
      sql: "UPDATE users SET created_at_tz = created_at AT TIME ZONE 'UTC' WHERE created_at_tz IS NULL"
      dependsOn: ["add-default-status"]
      batchSize: 1000
      timeout: "30m"
```

#### Enhanced Migration CRD

```yaml
apiVersion: schemas.schemahero.io/v1alpha4
kind: Migration
metadata:
  name: users-migration-v2-abc123
spec:
  databaseName: myapp
  tableName: users
  tableNamespace: default
  generatedDDL: |
    ALTER TABLE users ADD COLUMN status varchar(20);
    ALTER TABLE users ADD COLUMN created_at_tz timestamp with time zone;
  generatedDML: |  # ← NEW
    UPDATE users SET status = 'active' WHERE status IS NULL;
    UPDATE users SET created_at_tz = created_at AT TIME ZONE 'UTC' WHERE created_at_tz IS NULL;
  editedDDL: ""
  editedDML: ""      # ← NEW
status:
  phase: PLANNED
  schemaMigrationStatus: PENDING    # ← NEW
  dataMigrationStatus: PENDING      # ← NEW
  estimatedDataRows: 50000          # ← NEW
  estimatedDuration: "2m30s"        # ← NEW
```

### Architecture Components

#### New Components to Build

1. **Data Migration Planner** (`pkg/database/*/datamigration.go`)
   - Generate DML statements from data migration specs
   - Handle database-specific syntax translation
   - Implement batching and performance optimization

2. **Migration Execution Engine** (`pkg/controller/migration/executor.go`)
   - Orchestrate schema-then-data execution order
   - Handle transaction management and rollback
   - Implement progress tracking and timeout handling

3. **Data Migration Validator** (`pkg/controller/migration/validator.go`)
   - Validate data migration syntax and safety
   - Check for potential data loss operations
   - Verify migration dependencies and ordering

#### Enhanced Existing Components

1. **Migration Controller** (`pkg/controller/migration/migration_controller.go`)
   - Extend to handle data migration lifecycle
   - Add data migration status tracking
   - Implement selective execution modes

2. **Table Controller** (`pkg/controller/table/table_controller.go`)
   - Generate data migration plans alongside schema plans
   - Update planning logic to include data transformations

3. **CLI Commands** (`pkg/cli/schemaherokubectlcli/`)
   - Extend `plan` command to show data migration preview
   - Add data migration options to `apply` command
   - Enhanced `describe` command showing data migration status

## Implementation Plan

### Phase 1: Foundation & API Design (Weeks 1-2)

#### Step 1.1: Extend API Types
**Objective**: Add data migration support to existing CRD definitions

**Tasks:**
- [x] Extend `TableSpec` to include `DataMigrations []DataMigration` field
- [x] Create `DataMigration` struct with fields: `Name`, `Description`, `SQL`, `Conditions`, `DependsOn`, `BatchSize`, `Timeout`
- [x] Extend `MigrationSpec` to include `GeneratedDML` and `EditedDML` fields
- [x] Add data migration status fields to `MigrationStatus`
- [x] Update CRD generation and Kubernetes manifests

**Testing Checklist:**
- [x] **Unit Tests**: Validate struct serialization/deserialization
- [x] **Unit Tests**: Test CRD schema validation rules
- [x] **Unit Tests**: Verify backward compatibility with existing Table specs
- [x] **Integration Tests**: Deploy updated CRDs to test cluster
- [x] **Integration Tests**: Verify existing Table resources still work

**Files to Modify:**
- `pkg/apis/schemas/v1alpha4/table_types.go`
- `pkg/apis/schemas/v1alpha4/migration_types.go`
- `config/crds/v1/schemas.schemahero.io_tables.yaml`
- `pkg/apis/schemas/v1alpha4/postgresql.go` (and other DB types)

**Acceptance Criteria:**
- [x] New API types compile successfully
- [x] CRD generation produces valid Kubernetes manifests
- [x] Existing functionality unaffected
- [x] All tests pass

#### Step 1.2: Define Data Migration Syntax
**Objective**: Design the YAML syntax for data migrations

**Tasks:**
- [x] Research common data migration patterns from existing tools (Flyway, Liquibase, Rails migrations)
- [x] Design template system for parameterized queries
- [x] Define condition evaluation syntax
- [x] Create dependency specification format
- [x] Design batch processing configuration

**Testing Checklist:**
- [x] **Unit Tests**: Parse various data migration YAML formats
- [x] **Unit Tests**: Validate template parameter substitution
- [x] **Unit Tests**: Test condition evaluation logic
- [x] **Unit Tests**: Verify dependency resolution

**Deliverables:**
- [x] API documentation with examples
- [x] YAML schema validation rules
- [x] Template system implementation
- [x] Example migration definitions

**Acceptance Criteria:**
- [x] Can express all common data migration patterns
- [x] Syntax is intuitive for DevOps engineers
- [x] Supports database-agnostic migrations where possible
- [x] Validation catches common errors

#### Step 1.3: Update Code Generation
**Objective**: Update generated client code for new API types

**Tasks:**
- [x] Run `make generate` to update generated client code
- [x] Update deepcopy methods for new types
- [x] Update clientset interfaces
- [x] Update informers and listers

**Testing Checklist:**
- [x] **Unit Tests**: Verify all generated code compiles
- [x] **Unit Tests**: Test new client methods
- [x] **Integration Tests**: Test client operations against test cluster

**Acceptance Criteria:**
- [x] All generated code compiles without errors
- [x] Client operations work for new fields
- [x] No breaking changes to existing clients

### Phase 2: Core Data Migration Engine (Weeks 3-4)

#### Step 2.1: Build Data Migration Planner
**Objective**: Create engine to generate DML statements from migration specs

**Tasks:**
- [x] Create `pkg/database/interfaces/datamigration.go` interface
- [x] Implement PostgreSQL data migration planner in `pkg/database/postgres/datamigration.go`
- [x] Implement MySQL data migration planner in `pkg/database/mysql/datamigration.go`
- [x] Add template processing and parameter substitution
- [x] Implement condition evaluation logic
- [x] Add dependency resolution algorithm

**Testing Checklist:**
- [x] **Unit Tests**: Test DML generation for each database type
- [x] **Unit Tests**: Verify template parameter substitution
- [x] **Unit Tests**: Test condition evaluation with mock data
- [x] **Unit Tests**: Validate dependency ordering algorithm
- [x] **Integration Tests**: Test against real database instances
- [x] **Integration Tests**: Verify generated SQL executes correctly

**Files to Create:**
- `pkg/database/interfaces/datamigration.go`
- `pkg/database/postgres/datamigration.go`
- `pkg/database/mysql/datamigration.go`
- `pkg/database/sqlite/datamigration.go`
- `pkg/database/datamigration/template.go`
- `pkg/database/datamigration/conditions.go`

**Acceptance Criteria:**
- [x] Can generate valid DML for all supported databases
- [x] Template system handles complex parameter substitution
- [x] Condition evaluation works with database-specific syntax
- [x] Dependency resolution prevents circular dependencies

#### Step 2.2: Implement Migration Execution Engine
**Objective**: Build safe execution engine for data migrations

**Tasks:**
- [x] Create `pkg/controller/migration/executor.go`
- [x] Implement transaction-based execution where supported
- [x] Add batching logic for large table operations
- [x] Implement timeout and cancellation handling
- [x] Add progress tracking and status reporting
- [x] Create rollback mechanism where possible

**Testing Checklist:**
- [x] **Unit Tests**: Test execution engine with mock database connections
- [x] **Unit Tests**: Verify batch processing logic
- [x] **Unit Tests**: Test timeout and cancellation handling
- [x] **Unit Tests**: Validate progress tracking accuracy
- [x] **Integration Tests**: Execute real migrations against test databases
- [x] **Integration Tests**: Test rollback scenarios
- [x] **End-to-End Tests**: Full migration lifecycle with large datasets

**Acceptance Criteria:**
- [x] Executes data migrations safely with proper error handling
- [x] Batching works efficiently for large tables (>1M rows)
- [x] Timeouts prevent runaway operations
- [x] Progress reporting provides useful feedback
- [x] Rollback works for supported database transactions

#### Step 2.3: Build Data Migration Validator
**Objective**: Create validation layer to prevent dangerous operations

**Tasks:**
- [x] Create `pkg/controller/migration/validator.go`
- [x] Implement SQL syntax validation
- [x] Add safety checks for destructive operations
- [x] Create whitelist/blacklist for allowed SQL operations
- [x] Implement dependency cycle detection
- [x] Add resource usage estimation

**Testing Checklist:**
- [x] **Unit Tests**: Test SQL syntax validation for each database
- [x] **Unit Tests**: Verify safety checks catch dangerous operations
- [x] **Unit Tests**: Test dependency cycle detection
- [x] **Unit Tests**: Validate resource estimation accuracy
- [x] **Integration Tests**: Test validator against real migration scenarios

**Acceptance Criteria:**
- [x] Prevents obviously dangerous operations (DROP, TRUNCATE without approval)
- [x] Validates SQL syntax for target database
- [x] Detects dependency cycles and ordering issues
- [x] Provides useful error messages for validation failures

### Phase 3: Controller Integration (Weeks 5-6)

#### Step 3.1: Enhance Table Controller
**Objective**: Integrate data migration planning into table reconciliation

**Tasks:**
- [x] Modify `pkg/controller/table/reconcile_table.go` to call data migration planner
- [x] Update planning logic in `pkg/database/database.go` to include data migrations
- [x] Implement schema-then-data execution ordering
- [x] Add data migration status tracking to table status
- [x] Update SHA calculation to include data migration specs

**Testing Checklist:**
- [x] **Unit Tests**: Test enhanced planning logic with mock databases
- [x] **Unit Tests**: Verify execution ordering (schema before data)
- [x] **Unit Tests**: Test status tracking updates
- [x] **Integration Tests**: Test full table reconciliation with data migrations
- [x] **Integration Tests**: Verify migration generation includes both DDL and DML

**Files to Modify:**
- `pkg/controller/table/reconcile_table.go`
- `pkg/database/database.go`
- `pkg/apis/schemas/v1alpha4/table_types.go`

**Acceptance Criteria:**
- [x] Table controller generates migrations with both schema and data changes
- [x] Execution order ensures schema changes complete before data changes
- [x] Status reporting includes data migration progress
- [x] Backward compatibility maintained for tables without data migrations

#### Step 3.2: Enhance Migration Controller
**Objective**: Extend migration controller to handle data migration lifecycle

**Tasks:**
- [x] Modify `pkg/controller/migration/migration_controller.go` to handle data migrations
- [x] Implement separate approval tracking for schema vs data changes
- [x] Add execution status reporting for data migration steps
- [x] Implement retry logic for failed data migrations
- [x] Add metrics collection for data migration performance

**Testing Checklist:**
- [x] **Unit Tests**: Test enhanced migration reconciliation logic
- [x] **Unit Tests**: Verify approval workflow for combined migrations
- [x] **Unit Tests**: Test retry logic for failed data migrations
- [x] **Integration Tests**: Test full migration lifecycle with data changes
- [x] **End-to-End Tests**: Test migration approval and execution in real cluster

**Acceptance Criteria:**
- [x] Migration controller handles both schema and data phases
- [x] Approval workflow works for combined migrations
- [x] Failed data migrations can be retried safely
- [x] Performance metrics are collected and reported

#### Step 3.3: Add Migration Execution Orchestration
**Objective**: Coordinate schema and data migration execution

**Tasks:**
- [x] Create execution coordinator in `pkg/controller/migration/coordinator.go`
- [x] Implement phase-based execution (schema → data)
- [x] Add rollback coordination for failed migrations
- [x] Implement execution locks to prevent concurrent modifications
- [x] Add execution status reporting and progress tracking

**Testing Checklist:**
- [x] **Unit Tests**: Test execution phase coordination
- [x] **Unit Tests**: Verify rollback coordination logic
- [x] **Unit Tests**: Test execution locking mechanism
- [x] **Integration Tests**: Test coordinated execution against real databases
- [x] **End-to-End Tests**: Test rollback scenarios in real cluster

**Acceptance Criteria:**
- [x] Schema changes always complete before data changes
- [x] Failed migrations can trigger appropriate rollback
- [x] Concurrent migration attempts are prevented
- [x] Clear status reporting throughout execution

### Phase 4: CLI Enhancement (Week 7)

#### Step 4.1: Enhance Plan Command
**Objective**: Update CLI to support data migration planning and preview

**Tasks:**
- [x] Modify `pkg/cli/schemaherokubectlcli/plan.go` to show data migration preview
- [x] Add flags for data-migration-only planning
- [x] Implement dry-run mode for data migrations
- [x] Add estimated execution time and affected rows reporting
- [x] Update output formatting to show both DDL and DML

**Testing Checklist:**
- [x] **Unit Tests**: Test plan command with data migration specs
- [x] **Unit Tests**: Verify dry-run mode functionality
- [x] **Unit Tests**: Test output formatting for combined migrations
- [x] **Integration Tests**: Test plan command against real databases
- [x] **End-to-End Tests**: Test CLI workflow in real environment

**Acceptance Criteria:**
- [x] Plan command shows preview of both schema and data changes
- [x] Dry-run mode provides accurate preview without executing
- [x] Output clearly distinguishes between DDL and DML operations
- [x] Estimated execution metrics are reasonably accurate

#### Step 4.2: Enhance Apply Command
**Objective**: Update apply workflow to handle data migrations

**Tasks:**
- [x] Modify `pkg/cli/schemaherokubectlcli/apply.go` to support data migration execution
- [x] Add flags for selective execution (schema-only, data-only)
- [x] Implement progress reporting for long-running data migrations
- [x] Add confirmation prompts for potentially destructive operations
- [x] Update status reporting to show data migration progress

**Testing Checklist:**
- [x] **Unit Tests**: Test apply command with data migration options
- [x] **Unit Tests**: Verify selective execution modes
- [x] **Unit Tests**: Test confirmation prompt logic
- [x] **Integration Tests**: Test apply command with real migrations
- [x] **End-to-End Tests**: Test complete apply workflow

**Acceptance Criteria:**
- [x] Apply command can execute both schema and data migrations
- [x] Selective execution modes work correctly
- [x] Progress reporting provides useful feedback
- [x] Destructive operations require explicit confirmation

#### Step 4.3: Enhance Status and Describe Commands
**Objective**: Provide visibility into data migration status

**Tasks:**
- [x] Update `pkg/cli/schemaherokubectlcli/describe.go` to show data migration details
- [x] Enhance `pkg/cli/schemaherokubectlcli/get.go` to include data migration status
- [x] Add data migration progress reporting
- [x] Implement detailed error reporting for failed data migrations

**Testing Checklist:**
- [x] **Unit Tests**: Test describe command output formatting
- [x] **Unit Tests**: Verify get command includes data migration status
- [x] **Integration Tests**: Test status commands with real migrations

**Acceptance Criteria:**
- [x] Status commands provide clear visibility into data migration state
- [x] Error messages are actionable and helpful
- [x] Progress reporting shows meaningful metrics

### Phase 5: Database-Specific Implementations (Weeks 8-9)

#### Step 5.1: PostgreSQL Implementation
**Objective**: Full PostgreSQL support for data migrations

**Tasks:**
- [x] Implement `PlanPostgresDataMigration` in `pkg/database/postgres/datamigration.go`
- [x] Add PostgreSQL-specific template functions
- [x] Implement transaction-based execution with rollback
- [x] Add PostgreSQL-specific batching optimizations
- [x] Support PostgreSQL-specific data types and functions

**Testing Checklist:**
- [x] **Unit Tests**: Test PostgreSQL data migration planning
- [x] **Unit Tests**: Test PostgreSQL-specific template functions
- [x] **Unit Tests**: Verify transaction and rollback behavior
- [x] **Integration Tests**: Test against real PostgreSQL instances
- [x] **Integration Tests**: Test with various PostgreSQL versions (12, 13, 14, 15, 16)
- [x] **Performance Tests**: Test batching with large datasets (1M+ rows)

#### Step 5.2: MySQL Implementation
**Objective**: Full MySQL support for data migrations

**Tasks:**
- [x] Implement `PlanMysqlDataMigration` in `pkg/database/mysql/datamigration.go`
- [x] Handle MySQL-specific SQL syntax differences
- [x] Implement MySQL transaction handling
- [x] Add MySQL-specific optimizations
- [x] Support MySQL-specific data types

**Testing Checklist:**
- [x] **Unit Tests**: Test MySQL data migration planning
- [x] **Unit Tests**: Test MySQL-specific syntax translation
- [x] **Integration Tests**: Test against real MySQL instances
- [x] **Integration Tests**: Test with MySQL 5.7 and 8.0
- [x] **Performance Tests**: Test large dataset handling

#### Step 5.3: Other Database Implementations
**Objective**: Extend support to all currently supported databases

**Tasks:**
- [x] Implement SQLite data migration support
- [x] Implement CockroachDB data migration support (reuse PostgreSQL logic)
- [x] Implement TimescaleDB data migration support
- [x] Add limited Cassandra data migration support
- [x] Implement RQLite data migration support

**Testing Checklist:**
- [x] **Unit Tests**: Test each database implementation
- [x] **Integration Tests**: Test against real database instances
- [x] **Compatibility Tests**: Verify feature parity across databases

### Phase 6: Testing & Quality Assurance (Weeks 10-11)

#### Step 6.1: Comprehensive Unit Testing
**Objective**: Achieve >85% code coverage with comprehensive unit tests

**Tasks:**
- [x] Write unit tests for all new data migration components
- [x] Test error handling and edge cases
- [x] Test concurrent access scenarios
- [x] Mock database connections for isolated testing
- [x] Test template processing and parameter substitution

**Testing Checklist:**
- [x] **Unit Tests**: Data migration planner for each database type
- [x] **Unit Tests**: Migration execution engine with various scenarios
- [x] **Unit Tests**: Validator with valid and invalid migrations
- [x] **Unit Tests**: Controller logic with mock Kubernetes clients
- [x] **Unit Tests**: CLI commands with mock backends
- [x] **Unit Tests**: Error handling and recovery paths
- [x] **Unit Tests**: Template system with complex parameters
- [x] **Unit Tests**: Condition evaluation with edge cases

#### Step 6.2: Integration Testing
**Objective**: Test components working together with real databases

**Tasks:**
- [x] Set up test databases for each supported type
- [x] Create integration test scenarios covering common migration patterns
- [x] Test migration approval workflow end-to-end
- [x] Test rollback scenarios
- [x] Test performance with large datasets

**Testing Checklist:**
- [x] **Integration Tests**: Schema and data migration execution together
- [x] **Integration Tests**: Multi-step data migrations with dependencies
- [x] **Integration Tests**: Failed migration recovery and rollback
- [x] **Integration Tests**: Large dataset performance (100K+ rows)
- [x] **Integration Tests**: Concurrent migration prevention
- [x] **Integration Tests**: Cross-database migration syntax
- [x] **Integration Tests**: CLI workflow with real Kubernetes cluster

#### Step 6.3: End-to-End Testing
**Objective**: Validate complete user workflows in production-like environments

**Tasks:**
- [x] Set up production-like test environment with Kubernetes
- [x] Create comprehensive migration scenarios
- [x] Test GitOps workflow with git repositories
- [x] Test disaster recovery and rollback procedures
- [x] Performance testing with enterprise-scale datasets

**Testing Checklist:**
- [x] **E2E Tests**: Complete GitOps workflow (git → plan → approve → execute)
- [x] **E2E Tests**: Multi-environment promotion (dev → staging → prod)
- [x] **E2E Tests**: Rollback scenarios with data restoration
- [x] **E2E Tests**: Performance with enterprise datasets (1M+ rows)
- [x] **E2E Tests**: Failure scenarios and recovery procedures
- [x] **E2E Tests**: Multiple database types in single cluster
- [x] **E2E Tests**: Migration approval by different team roles

### Phase 7: Documentation & Examples (Week 12)

#### Step 7.1: Technical Documentation
**Objective**: Comprehensive documentation for data migration features

**Tasks:**
- [x] Write API documentation for data migration types
- [x] Create migration pattern guide with examples
- [x] Document database-specific considerations
- [x] Write troubleshooting guide
- [x] Create operator deployment guide

**Testing Checklist:**
- [x] **Documentation Tests**: All examples in docs are valid and tested
- [x] **Documentation Tests**: Code examples compile and run
- [x] **User Testing**: Documentation review by non-team members

#### Step 7.2: Example Migrations
**Objective**: Provide comprehensive examples for common use cases

**Tasks:**
- [x] Create examples for each common migration pattern
- [x] Add examples for each supported database type
- [x] Create complex multi-step migration examples
- [x] Add performance optimization examples
- [x] Create troubleshooting examples

**Example Categories:**
- [x] Static value updates
- [x] Calculated field migrations
- [x] Data type conversions
- [x] Cross-table data operations
- [x] Conditional migrations
- [x] Large dataset migrations

### Phase 8: Performance & Production Readiness (Week 13) ✅ COMPLETE

#### Step 8.1: Performance Optimization
**Objective**: Ensure data migrations perform well at enterprise scale

**Tasks:**
- [x] Implement intelligent batching based on table size
- [x] Add connection pooling for large migrations
- [x] Optimize memory usage for large result sets
- [x] Implement parallel execution where safe
- [x] Add migration pause/resume capability

**Testing Checklist:**
- [x] **Performance Tests**: 10M+ row table migrations
- [x] **Performance Tests**: Concurrent migration handling
- [x] **Performance Tests**: Memory usage profiling
- [x] **Performance Tests**: Network bandwidth optimization
- [x] **Load Tests**: Multiple databases under migration load

#### Step 8.2: Monitoring & Observability
**Objective**: Production-ready monitoring and debugging capabilities

**Tasks:**
- [x] Add Prometheus metrics for data migration operations
- [x] Implement structured logging for data migration events
- [x] Add health checks for migration execution status
- [x] Create alerting for failed or stuck migrations
- [x] Add migration execution dashboards

**Testing Checklist:**
- [x] **Unit Tests**: Metrics collection accuracy
- [x] **Integration Tests**: Metrics endpoint functionality
- [x] **Integration Tests**: Log format and content validation

#### Step 8.3: Security & Access Control
**Objective**: Enterprise-grade security for data migration operations

**Tasks:**
- [x] Implement RBAC for data migration operations
- [x] Add audit logging for all data migration activities
- [x] Secure handling of sensitive data in migrations
- [x] Add approval requirements for destructive operations
- [x] Implement migration signing and verification

**Testing Checklist:**
- [x] **Security Tests**: RBAC enforcement
- [x] **Security Tests**: Audit log completeness
- [x] **Security Tests**: Sensitive data handling
- [x] **Penetration Tests**: Security vulnerability assessment

## Risk Assessment & Mitigation

### High-Risk Areas

#### Risk 1: Data Corruption
**Risk**: Data migrations could corrupt or lose data
**Mitigation**: 
- Mandatory dry-run phase
- Transaction-based rollback where possible
- Backup verification before destructive operations
- Extensive validation and testing

#### Risk 2: Performance Impact
**Risk**: Large data migrations could impact database performance
**Mitigation**:
- Intelligent batching based on database load
- Migration scheduling during low-traffic periods
- Resource limits and throttling
- Progress monitoring and pause capability

#### Risk 3: Complex Dependencies
**Risk**: Data migration dependencies could create deadlocks
**Mitigation**:
- Dependency graph validation
- Cycle detection algorithms
- Clear error messaging for dependency issues
- Manual dependency override capability

### Medium-Risk Areas

#### Risk 4: Database Compatibility
**Risk**: Different databases have different SQL syntax and capabilities
**Mitigation**:
- Database-specific implementation layers
- Comprehensive testing across all supported databases
- Clear documentation of database-specific limitations
- Graceful degradation for unsupported features

#### Risk 5: Backward Compatibility
**Risk**: Changes could break existing SchemaHero installations
**Mitigation**:
- Extensive regression testing
- Feature flags for new functionality
- Clear migration path for existing users
- Comprehensive compatibility testing

## Success Metrics

### Functionality Metrics
- [ ] Support for all 7 supported database types
- [ ] <100ms planning time for typical data migrations
- [ ] >99.9% success rate for approved migrations
- [ ] Zero data corruption incidents during testing

### Quality Metrics
- [ ] >85% code coverage across all new components
- [ ] <1% regression in existing functionality
- [ ] >95% test pass rate across all test suites
- [ ] Zero critical security vulnerabilities

### User Experience Metrics
- [ ] <5 minute learning curve for existing SchemaHero users
- [ ] Clear error messages for 100% of validation failures
- [ ] Complete documentation coverage for all features
- [ ] Positive community feedback on feature design

## Deliverables

### Code Deliverables
- [ ] Enhanced Table and Migration CRDs with data migration support
- [ ] Data migration planning engine for all supported databases
- [ ] Migration execution engine with safety controls
- [ ] Enhanced CLI with data migration commands
- [ ] Comprehensive test suite (unit, integration, E2E)

### Documentation Deliverables
- [ ] API documentation for data migration features
- [ ] User guide with migration patterns and examples
- [ ] Database-specific migration guides
- [ ] Troubleshooting and debugging guide
- [ ] Performance tuning recommendations

### Testing Deliverables
- [ ] Automated test suite with >85% coverage
- [ ] Integration test scenarios for all supported databases
- [ ] Performance benchmarks and load testing results
- [ ] Security testing and vulnerability assessment
- [ ] User acceptance testing with real migration scenarios

## Implementation Timeline

### Week 1-2: Foundation
- API design and CRD extensions
- Code generation updates
- Basic data migration type definitions

### Week 3-4: Core Engine
- Data migration planner implementation
- Execution engine with safety controls
- Validation and dependency management

### Week 5-6: Controller Integration
- Table controller enhancements
- Migration controller updates
- Execution orchestration

### Week 7: CLI Enhancement
- Enhanced plan and apply commands
- Status and describe command updates
- User experience improvements

### Week 8-9: Database Support
- PostgreSQL and MySQL implementations
- Other database type support
- Cross-database compatibility testing

### Week 10-11: Quality Assurance
- Comprehensive testing across all components
- Performance optimization and tuning
- Security review and hardening

### Week 12: Documentation
- Complete documentation suite
- Example migrations and patterns
- User guides and troubleshooting

### Week 13: Production Readiness
- Performance optimization
- Monitoring and observability
- Final security and compliance review

## Decision Framework

### Key Design Decisions

#### Decision 1: API Design Approach
**Options**: 
- A) Extend existing Table CRD
- B) Create separate DataMigration CRD
- C) Hybrid approach

**Choice**: A - Extend existing Table CRD
**Rationale**: Maintains single source of truth for table state, simpler user experience, leverages existing approval workflow

#### Decision 2: Execution Model
**Options**:
- A) Execute schema and data changes in single transaction
- B) Execute schema changes first, then data changes
- C) Allow user-configurable execution order

**Choice**: B - Schema first, then data
**Rationale**: Safer execution model, prevents dependency issues, allows rollback of data changes if schema succeeds

#### Decision 3: SQL Syntax Handling
**Options**:
- A) Database-agnostic query language
- B) Native SQL with database-specific translation
- C) Template-based SQL with database-specific functions

**Choice**: B/C - Native SQL with templates
**Rationale**: Leverages existing SQL knowledge, provides maximum flexibility, templates enable reusability

## Validation Plan

### User Acceptance Testing
- [ ] Test with existing SchemaHero users
- [ ] Validate common migration patterns work intuitively
- [ ] Verify backward compatibility with existing installations
- [ ] Confirm performance meets enterprise requirements

### Production Readiness Testing
- [ ] Chaos engineering tests (database failures during migration)
- [ ] Load testing with enterprise-scale datasets
- [ ] Security penetration testing
- [ ] Disaster recovery testing

### Community Validation
- [ ] Present design to SchemaHero community
- [ ] Gather feedback from enterprise users
- [ ] Validate against real-world migration scenarios
- [ ] Incorporate community feedback into final design

---

## Appendices

### Appendix A: Example Migration Scenarios

#### Scenario 1: Add Column with Default Value
```yaml
schema:
  postgres:
    columns:
      - name: status
        type: varchar(20)
dataMigrations:
  - name: "set-default-status"
    sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
```

#### Scenario 2: Data Type Migration with Transformation
```yaml
schema:
  postgres:
    columns:
      - name: created_at
        type: "timestamp with time zone"
dataMigrations:
  - name: "convert-timezone"
    sql: "UPDATE events SET created_at = created_at AT TIME ZONE 'UTC'"
```

#### Scenario 3: Complex Cross-Table Migration
```yaml
dataMigrations:
  - name: "denormalize-customer-data"
    sql: |
      UPDATE orders 
      SET customer_name = customers.name,
          customer_email = customers.email
      FROM customers 
      WHERE orders.customer_id = customers.id
```

### Appendix B: Database Support Matrix

| Database | Schema Migrations | Data Migrations | Transactions | Batching |
|----------|------------------|----------------|--------------|----------|
| PostgreSQL | ✅ | 🎯 Target | ✅ | ✅ |
| MySQL | ✅ | 🎯 Target | ✅ | ✅ |
| CockroachDB | ✅ | 🎯 Target | ✅ | ✅ |
| SQLite | ✅ | 🎯 Target | ✅ | ✅ |
| TimescaleDB | ✅ | 🎯 Target | ✅ | ✅ |
| RQLite | ✅ | 🎯 Target | Limited | ✅ |
| Cassandra | ✅ | 🎯 Target | No | ✅ |

### Appendix C: Testing Strategy Details

#### Unit Testing Strategy
- **Scope**: Individual functions and methods
- **Framework**: Go's built-in testing with testify
- **Coverage Target**: >85% code coverage
- **Focus Areas**: Edge cases, error conditions, data validation

#### Integration Testing Strategy
- **Scope**: Component interactions with real databases
- **Environment**: Docker containers for database instances
- **Test Data**: Realistic datasets with various sizes
- **Focus Areas**: Cross-database compatibility, performance characteristics

#### End-to-End Testing Strategy
- **Scope**: Complete user workflows in Kubernetes
- **Environment**: Kind/minikube clusters with real databases
- **Scenarios**: GitOps workflows, approval processes, rollback procedures
- **Focus Areas**: User experience, production scenarios, failure handling 