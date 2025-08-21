#!/bin/bash

# Integration test for data migration CRDs and client operations
# This test verifies that:
# 1. CRDs can be deployed successfully
# 2. Resources with new fields can be created
# 3. Client operations work correctly

set -e

echo "Starting data migration CRD integration test..."

# Test if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "kubectl not found, skipping integration test"
    exit 0
fi

# Check if we have a cluster
if ! kubectl cluster-info &> /dev/null; then
    echo "No Kubernetes cluster available, skipping integration test"
    exit 0
fi

# Deploy the CRDs
echo "Deploying updated CRDs..."
kubectl apply -f config/crds/v1/schemas.schemahero.io_tables.yaml
kubectl apply -f config/crds/v1/schemas.schemahero.io_migrations.yaml

# Wait for CRDs to be established
echo "Waiting for CRDs to be established..."
kubectl wait --for condition=established --timeout=60s crd/tables.schemas.schemahero.io
kubectl wait --for condition=established --timeout=60s crd/migrations.schemas.schemahero.io

# Create a test namespace
TEST_NS="schemahero-test-$$"
echo "Creating test namespace: $TEST_NS"
kubectl create namespace $TEST_NS

# Create a table with data migrations
echo "Creating a table with data migrations..."
cat <<EOF | kubectl apply -n $TEST_NS -f -
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: test-table-with-data-migrations
spec:
  database: testdb
  name: users
  schema:
    postgres:
      columns:
        - name: id
          type: serial
        - name: username
          type: varchar(100)
        - name: email
          type: varchar(255)
        - name: status
          type: varchar(20)
  dataMigrations:
    - name: backfill-status
      description: "Set default status for existing users"
      sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
      conditions:
        - query: "SELECT COUNT(*) FROM users WHERE status IS NULL"
          operator: ">"
          value: 0
      batchSize: 1000
      timeout: "10m"
    - name: normalize-emails
      description: "Convert emails to lowercase"
      type: TRANSFORM
      template:
        template: "UPDATE users SET email = LOWER(email) WHERE email != LOWER(email)"
      dependsOn: ["backfill-status"]
      priority: 5
      tags: ["email", "normalization"]
EOF

# Verify the table was created
echo "Verifying table creation..."
kubectl get table -n $TEST_NS test-table-with-data-migrations

# Check that data migrations are present
echo "Checking data migrations in table spec..."
MIGRATIONS_COUNT=$(kubectl get table -n $TEST_NS test-table-with-data-migrations -o jsonpath='{.spec.dataMigrations}' | jq '. | length')
if [ "$MIGRATIONS_COUNT" != "2" ]; then
    echo "ERROR: Expected 2 data migrations, found $MIGRATIONS_COUNT"
    kubectl delete namespace $TEST_NS
    exit 1
fi

# Create a migration with DML fields
echo "Creating a migration with DML fields..."
cat <<EOF | kubectl apply -n $TEST_NS -f -
apiVersion: schemas.schemahero.io/v1alpha4
kind: Migration
metadata:
  name: test-migration-with-dml
spec:
  databaseName: testdb
  tableName: users
  tableNamespace: $TEST_NS
  generatedDDL: |
    ALTER TABLE users ADD COLUMN created_at TIMESTAMP;
  generatedDML: |
    UPDATE users SET created_at = NOW() WHERE created_at IS NULL;
  editedDDL: ""
  editedDML: ""
status:
  phase: PLANNED
  schemaMigrationStatus: PENDING
  dataMigrationStatus: PENDING
  estimatedDataRows: 1000
  estimatedDuration: "30s"
EOF

# Verify the migration was created
echo "Verifying migration creation..."
kubectl get migration -n $TEST_NS test-migration-with-dml

# Check that DML fields are present
echo "Checking DML fields in migration spec..."
DML_CONTENT=$(kubectl get migration -n $TEST_NS test-migration-with-dml -o jsonpath='{.spec.generatedDML}')
if [ -z "$DML_CONTENT" ]; then
    echo "ERROR: generatedDML field is empty"
    kubectl delete namespace $TEST_NS
    exit 1
fi

# Check migration status fields
echo "Checking data migration status fields..."
DATA_STATUS=$(kubectl get migration -n $TEST_NS test-migration-with-dml -o jsonpath='{.status.dataMigrationStatus}')
if [ "$DATA_STATUS" != "PENDING" ]; then
    echo "ERROR: Expected dataMigrationStatus to be PENDING, got: $DATA_STATUS"
    kubectl delete namespace $TEST_NS
    exit 1
fi

# Clean up
echo "Cleaning up test namespace..."
kubectl delete namespace $TEST_NS

echo "Integration test completed successfully!"
echo "✅ CRDs deployed successfully"
echo "✅ Table with data migrations created"
echo "✅ Migration with DML fields created"
echo "✅ All client operations work correctly" 