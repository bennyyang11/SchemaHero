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
- [ ] Create `pkg/database/interfaces/datamigration.go` interface
- [ ] Implement PostgreSQL data migration planner in `pkg/database/postgres/datamigration.go`
- [ ] Implement MySQL data migration planner in `pkg/database/mysql/datamigration.go`
- [ ] Add template processing and parameter substitution
- [ ] Implement condition evaluation logic
- [ ] Add dependency resolution algorithm

**Testing Checklist:**
- [ ] **Unit Tests**: Test DML generation for each database type
- [ ] **Unit Tests**: Verify template parameter substitution
- [ ] **Unit Tests**: Test condition evaluation with mock data
- [ ] **Unit Tests**: Validate dependency ordering algorithm
- [ ] **Integration Tests**: Test against real database instances
- [ ] **Integration Tests**: Verify generated SQL executes correctly

**Files to Create:**
- `pkg/database/interfaces/datamigration.go`
- `pkg/database/postgres/datamigration.go`
- `pkg/database/mysql/datamigration.go`
- `pkg/database/sqlite/datamigration.go`
- `pkg/database/datamigration/template.go`
- `pkg/database/datamigration/conditions.go`

**Acceptance Criteria:**
- [ ] Can generate valid DML for all supported databases
- [ ] Template system handles complex parameter substitution
- [ ] Condition evaluation works with database-specific syntax
- [ ] Dependency resolution prevents circular dependencies

#### Step 2.2: Implement Migration Execution Engine
**Objective**: Build safe execution engine for data migrations

**Tasks:**
- [ ] Create `pkg/controller/migration/executor.go`
- [ ] Implement transaction-based execution where supported
- [ ] Add batching logic for large table operations
- [ ] Implement timeout and cancellation handling
- [ ] Add progress tracking and status reporting
- [ ] Create rollback mechanism where possible

**Testing Checklist:**
- [ ] **Unit Tests**: Test execution engine with mock database connections
- [ ] **Unit Tests**: Verify batch processing logic
- [ ] **Unit Tests**: Test timeout and cancellation handling
- [ ] **Unit Tests**: Validate progress tracking accuracy
- [ ] **Integration Tests**: Execute real migrations against test databases
- [ ] **Integration Tests**: Test rollback scenarios
- [ ] **End-to-End Tests**: Full migration lifecycle with large datasets

**Acceptance Criteria:**
- [ ] Executes data migrations safely with proper error handling
- [ ] Batching works efficiently for large tables (>1M rows)
- [ ] Timeouts prevent runaway operations
- [ ] Progress reporting provides useful feedback
- [ ] Rollback works for supported database transactions

#### Step 2.3: Build Data Migration Validator
**Objective**: Create validation layer to prevent dangerous operations

**Tasks:**
- [ ] Create `pkg/controller/migration/validator.go`
- [ ] Implement SQL syntax validation
- [ ] Add safety checks for destructive operations
- [ ] Create whitelist/blacklist for allowed SQL operations
- [ ] Implement dependency cycle detection
- [ ] Add resource usage estimation

**Testing Checklist:**
- [ ] **Unit Tests**: Test SQL syntax validation for each database
- [ ] **Unit Tests**: Verify safety checks catch dangerous operations
- [ ] **Unit Tests**: Test dependency cycle detection
- [ ] **Unit Tests**: Validate resource estimation accuracy
- [ ] **Integration Tests**: Test validator against real migration scenarios

**Acceptance Criteria:**
- [ ] Prevents obviously dangerous operations (DROP, TRUNCATE without approval)
- [ ] Validates SQL syntax for target database
- [ ] Detects dependency cycles and ordering issues
- [ ] Provides useful error messages for validation failures

### Phase 3: Controller Integration (Weeks 5-6)

#### Step 3.1: Enhance Table Controller
**Objective**: Integrate data migration planning into table reconciliation

**Tasks:**
- [ ] Modify `pkg/controller/table/reconcile_table.go` to call data migration planner
- [ ] Update planning logic in `pkg/database/database.go` to include data migrations
- [ ] Implement schema-then-data execution ordering
- [ ] Add data migration status tracking to table status
- [ ] Update SHA calculation to include data migration specs

**Testing Checklist:**
- [ ] **Unit Tests**: Test enhanced planning logic with mock databases
- [ ] **Unit Tests**: Verify execution ordering (schema before data)
- [ ] **Unit Tests**: Test status tracking updates
- [ ] **Integration Tests**: Test full table reconciliation with data migrations
- [ ] **Integration Tests**: Verify migration generation includes both DDL and DML

**Files to Modify:**
- `pkg/controller/table/reconcile_table.go`
- `pkg/database/database.go`
- `pkg/apis/schemas/v1alpha4/table_types.go`

**Acceptance Criteria:**
- [ ] Table controller generates migrations with both schema and data changes
- [ ] Execution order ensures schema changes complete before data changes
- [ ] Status reporting includes data migration progress
- [ ] Backward compatibility maintained for tables without data migrations

#### Step 3.2: Enhance Migration Controller
**Objective**: Extend migration controller to handle data migration lifecycle

**Tasks:**
- [ ] Modify `pkg/controller/migration/migration_controller.go` to handle data migrations
- [ ] Implement separate approval tracking for schema vs data changes
- [ ] Add execution status reporting for data migration steps
- [ ] Implement retry logic for failed data migrations
- [ ] Add metrics collection for data migration performance

**Testing Checklist:**
- [ ] **Unit Tests**: Test enhanced migration reconciliation logic
- [ ] **Unit Tests**: Verify approval workflow for combined migrations
- [ ] **Unit Tests**: Test retry logic for failed data migrations
- [ ] **Integration Tests**: Test full migration lifecycle with data changes
- [ ] **End-to-End Tests**: Test migration approval and execution in real cluster

**Acceptance Criteria:**
- [ ] Migration controller handles both schema and data phases
- [ ] Approval workflow works for combined migrations
- [ ] Failed data migrations can be retried safely
- [ ] Performance metrics are collected and reported

#### Step 3.3: Add Migration Execution Orchestration
**Objective**: Coordinate schema and data migration execution

**Tasks:**
- [ ] Create execution coordinator in `pkg/controller/migration/coordinator.go`
- [ ] Implement phase-based execution (schema → data)
- [ ] Add rollback coordination for failed migrations
- [ ] Implement execution locks to prevent concurrent modifications
- [ ] Add execution status reporting and progress tracking

**Testing Checklist:**
- [ ] **Unit Tests**: Test execution phase coordination
- [ ] **Unit Tests**: Verify rollback coordination logic
- [ ] **Unit Tests**: Test execution locking mechanism
- [ ] **Integration Tests**: Test coordinated execution against real databases
- [ ] **End-to-End Tests**: Test rollback scenarios in real cluster

**Acceptance Criteria:**
- [ ] Schema changes always complete before data changes
- [ ] Failed migrations can trigger appropriate rollback
- [ ] Concurrent migration attempts are prevented
- [ ] Clear status reporting throughout execution

### Phase 4: CLI Enhancement (Week 7)

#### Step 4.1: Enhance Plan Command
**Objective**: Update CLI to support data migration planning and preview

**Tasks:**
- [ ] Modify `pkg/cli/schemaherokubectlcli/plan.go` to show data migration preview
- [ ] Add flags for data-migration-only planning
- [ ] Implement dry-run mode for data migrations
- [ ] Add estimated execution time and affected rows reporting
- [ ] Update output formatting to show both DDL and DML

**Testing Checklist:**
- [ ] **Unit Tests**: Test plan command with data migration specs
- [ ] **Unit Tests**: Verify dry-run mode functionality
- [ ] **Unit Tests**: Test output formatting for combined migrations
- [ ] **Integration Tests**: Test plan command against real databases
- [ ] **End-to-End Tests**: Test CLI workflow in real environment

**Acceptance Criteria:**
- [ ] Plan command shows preview of both schema and data changes
- [ ] Dry-run mode provides accurate preview without executing
- [ ] Output clearly distinguishes between DDL and DML operations
- [ ] Estimated execution metrics are reasonably accurate

#### Step 4.2: Enhance Apply Command
**Objective**: Update apply workflow to handle data migrations

**Tasks:**
- [ ] Modify `pkg/cli/schemaherokubectlcli/apply.go` to support data migration execution
- [ ] Add flags for selective execution (schema-only, data-only)
- [ ] Implement progress reporting for long-running data migrations
- [ ] Add confirmation prompts for potentially destructive operations
- [ ] Update status reporting to show data migration progress

**Testing Checklist:**
- [ ] **Unit Tests**: Test apply command with data migration options
- [ ] **Unit Tests**: Verify selective execution modes
- [ ] **Unit Tests**: Test confirmation prompt logic
- [ ] **Integration Tests**: Test apply command with real migrations
- [ ] **End-to-End Tests**: Test complete apply workflow

**Acceptance Criteria:**
- [ ] Apply command can execute both schema and data migrations
- [ ] Selective execution modes work correctly
- [ ] Progress reporting provides useful feedback
- [ ] Destructive operations require explicit confirmation

#### Step 4.3: Enhance Status and Describe Commands
**Objective**: Provide visibility into data migration status

**Tasks:**
- [ ] Update `pkg/cli/schemaherokubectlcli/describe.go` to show data migration details
- [ ] Enhance `pkg/cli/schemaherokubectlcli/get.go` to include data migration status
- [ ] Add data migration progress reporting
- [ ] Implement detailed error reporting for failed data migrations

**Testing Checklist:**
- [ ] **Unit Tests**: Test describe command output formatting
- [ ] **Unit Tests**: Verify get command includes data migration status
- [ ] **Integration Tests**: Test status commands with real migrations

**Acceptance Criteria:**
- [ ] Status commands provide clear visibility into data migration state
- [ ] Error messages are actionable and helpful
- [ ] Progress reporting shows meaningful metrics

### Phase 5: Database-Specific Implementations (Weeks 8-9)

#### Step 5.1: PostgreSQL Implementation
**Objective**: Full PostgreSQL support for data migrations

**Tasks:**
- [ ] Implement `PlanPostgresDataMigration` in `pkg/database/postgres/datamigration.go`
- [ ] Add PostgreSQL-specific template functions
- [ ] Implement transaction-based execution with rollback
- [ ] Add PostgreSQL-specific batching optimizations
- [ ] Support PostgreSQL-specific data types and functions

**Testing Checklist:**
- [ ] **Unit Tests**: Test PostgreSQL data migration planning
- [ ] **Unit Tests**: Test PostgreSQL-specific template functions
- [ ] **Unit Tests**: Verify transaction and rollback behavior
- [ ] **Integration Tests**: Test against real PostgreSQL instances
- [ ] **Integration Tests**: Test with various PostgreSQL versions (12, 13, 14, 15, 16)
- [ ] **Performance Tests**: Test batching with large datasets (1M+ rows)

#### Step 5.2: MySQL Implementation
**Objective**: Full MySQL support for data migrations

**Tasks:**
- [ ] Implement `PlanMysqlDataMigration` in `pkg/database/mysql/datamigration.go`
- [ ] Handle MySQL-specific SQL syntax differences
- [ ] Implement MySQL transaction handling
- [ ] Add MySQL-specific optimizations
- [ ] Support MySQL-specific data types

**Testing Checklist:**
- [ ] **Unit Tests**: Test MySQL data migration planning
- [ ] **Unit Tests**: Test MySQL-specific syntax translation
- [ ] **Integration Tests**: Test against real MySQL instances
- [ ] **Integration Tests**: Test with MySQL 5.7 and 8.0
- [ ] **Performance Tests**: Test large dataset handling

#### Step 5.3: Other Database Implementations
**Objective**: Extend support to all currently supported databases

**Tasks:**
- [ ] Implement SQLite data migration support
- [ ] Implement CockroachDB data migration support (reuse PostgreSQL logic)
- [ ] Implement TimescaleDB data migration support
- [ ] Add limited Cassandra data migration support
- [ ] Implement RQLite data migration support

**Testing Checklist:**
- [ ] **Unit Tests**: Test each database implementation
- [ ] **Integration Tests**: Test against real database instances
- [ ] **Compatibility Tests**: Verify feature parity across databases

### Phase 6: Testing & Quality Assurance (Weeks 10-11)

#### Step 6.1: Comprehensive Unit Testing
**Objective**: Achieve >85% code coverage with comprehensive unit tests

**Tasks:**
- [ ] Write unit tests for all new data migration components
- [ ] Test error handling and edge cases
- [ ] Test concurrent access scenarios
- [ ] Mock database connections for isolated testing
- [ ] Test template processing and parameter substitution

**Testing Checklist:**
- [ ] **Unit Tests**: Data migration planner for each database type
- [ ] **Unit Tests**: Migration execution engine with various scenarios
- [ ] **Unit Tests**: Validator with valid and invalid migrations
- [ ] **Unit Tests**: Controller logic with mock Kubernetes clients
- [ ] **Unit Tests**: CLI commands with mock backends
- [ ] **Unit Tests**: Error handling and recovery paths
- [ ] **Unit Tests**: Template system with complex parameters
- [ ] **Unit Tests**: Condition evaluation with edge cases

#### Step 6.2: Integration Testing
**Objective**: Test components working together with real databases

**Tasks:**
- [ ] Set up test databases for each supported type
- [ ] Create integration test scenarios covering common migration patterns
- [ ] Test migration approval workflow end-to-end
- [ ] Test rollback scenarios
- [ ] Test performance with large datasets

**Testing Checklist:**
- [ ] **Integration Tests**: Schema and data migration execution together
- [ ] **Integration Tests**: Multi-step data migrations with dependencies
- [ ] **Integration Tests**: Failed migration recovery and rollback
- [ ] **Integration Tests**: Large dataset performance (100K+ rows)
- [ ] **Integration Tests**: Concurrent migration prevention
- [ ] **Integration Tests**: Cross-database migration syntax
- [ ] **Integration Tests**: CLI workflow with real Kubernetes cluster

#### Step 6.3: End-to-End Testing
**Objective**: Validate complete user workflows in production-like environments

**Tasks:**
- [ ] Set up production-like test environment with Kubernetes
- [ ] Create comprehensive migration scenarios
- [ ] Test GitOps workflow with git repositories
- [ ] Test disaster recovery and rollback procedures
- [ ] Performance testing with enterprise-scale datasets

**Testing Checklist:**
- [ ] **E2E Tests**: Complete GitOps workflow (git → plan → approve → execute)
- [ ] **E2E Tests**: Multi-environment promotion (dev → staging → prod)
- [ ] **E2E Tests**: Rollback scenarios with data restoration
- [ ] **E2E Tests**: Performance with enterprise datasets (1M+ rows)
- [ ] **E2E Tests**: Failure scenarios and recovery procedures
- [ ] **E2E Tests**: Multiple database types in single cluster
- [ ] **E2E Tests**: Migration approval by different team roles

### Phase 7: Documentation & Examples (Week 12)

#### Step 7.1: Technical Documentation
**Objective**: Comprehensive documentation for data migration features

**Tasks:**
- [ ] Write API documentation for data migration types
- [ ] Create migration pattern guide with examples
- [ ] Document database-specific considerations
- [ ] Write troubleshooting guide
- [ ] Create operator deployment guide

**Testing Checklist:**
- [ ] **Documentation Tests**: All examples in docs are valid and tested
- [ ] **Documentation Tests**: Code examples compile and run
- [ ] **User Testing**: Documentation review by non-team members

#### Step 7.2: Example Migrations
**Objective**: Provide comprehensive examples for common use cases

**Tasks:**
- [ ] Create examples for each common migration pattern
- [ ] Add examples for each supported database type
- [ ] Create complex multi-step migration examples
- [ ] Add performance optimization examples
- [ ] Create troubleshooting examples

**Example Categories:**
- [ ] Static value updates
- [ ] Calculated field migrations
- [ ] Data type conversions
- [ ] Cross-table data operations
- [ ] Conditional migrations
- [ ] Large dataset migrations

### Phase 8: Performance & Production Readiness (Week 13)

#### Step 8.1: Performance Optimization
**Objective**: Ensure data migrations perform well at enterprise scale

**Tasks:**
- [ ] Implement intelligent batching based on table size
- [ ] Add connection pooling for large migrations
- [ ] Optimize memory usage for large result sets
- [ ] Implement parallel execution where safe
- [ ] Add migration pause/resume capability

**Testing Checklist:**
- [ ] **Performance Tests**: 10M+ row table migrations
- [ ] **Performance Tests**: Concurrent migration handling
- [ ] **Performance Tests**: Memory usage profiling
- [ ] **Performance Tests**: Network bandwidth optimization
- [ ] **Load Tests**: Multiple databases under migration load

#### Step 8.2: Monitoring & Observability
**Objective**: Production-ready monitoring and debugging capabilities

**Tasks:**
- [ ] Add Prometheus metrics for data migration operations
- [ ] Implement structured logging for data migration events
- [ ] Add health checks for migration execution status
- [ ] Create alerting for failed or stuck migrations
- [ ] Add migration execution dashboards

**Testing Checklist:**
- [ ] **Unit Tests**: Metrics collection accuracy
- [ ] **Integration Tests**: Metrics endpoint functionality
- [ ] **Integration Tests**: Log format and content validation

#### Step 8.3: Security & Access Control
**Objective**: Enterprise-grade security for data migration operations

**Tasks:**
- [ ] Implement RBAC for data migration operations
- [ ] Add audit logging for all data migration activities
- [ ] Secure handling of sensitive data in migrations
- [ ] Add approval requirements for destructive operations
- [ ] Implement migration signing and verification

**Testing Checklist:**
- [ ] **Security Tests**: RBAC enforcement
- [ ] **Security Tests**: Audit log completeness
- [ ] **Security Tests**: Sensitive data handling
- [ ] **Penetration Tests**: Security vulnerability assessment

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