#!/bin/bash
# Integration test for data migration API types
# This script tests that the updated CRDs can be deployed and used

set -e

echo "=== Data Migration API Integration Test ==="

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "kubectl not found. Please install kubectl to run integration tests."
    exit 1
fi

# Check if we have a cluster context
if ! kubectl cluster-info &> /dev/null; then
    echo "No Kubernetes cluster found. Please ensure you have access to a test cluster."
    exit 1
fi

echo "1. Applying updated CRDs..."
kubectl apply -f config/crds/v1/schemas.schemahero.io_tables.yaml
kubectl apply -f config/crds/v1/schemas.schemahero.io_migrations.yaml

echo "2. Waiting for CRDs to be established..."
kubectl wait --for condition=established --timeout=60s crd/tables.schemas.schemahero.io
kubectl wait --for condition=established --timeout=60s crd/migrations.schemas.schemahero.io

echo "3. Creating a test namespace..."
kubectl create namespace schemahero-test-datamigration || true

echo "4. Creating a legacy Table resource (without data migrations)..."
cat <<EOF | kubectl apply -f -
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: legacy-table-test
  namespace: schemahero-test-datamigration
spec:
  database: testdb
  name: legacy_users
  schema:
    postgres:
      columns:
        - name: id
          type: serial
        - name: email
          type: varchar(255)
EOF

echo "5. Verifying legacy Table resource was created..."
kubectl get table legacy-table-test -n schemahero-test-datamigration

echo "6. Creating a new Table resource with data migrations..."
cat <<EOF | kubectl apply -f -
apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: users-with-datamigration
  namespace: schemahero-test-datamigration
spec:
  database: testdb
  name: users
  schema:
    postgres:
      columns:
        - name: id
          type: serial
        - name: email
          type: varchar(255)
        - name: status
          type: varchar(20)
  dataMigrations:
    - name: "set-default-status"
      description: "Set default status for existing users"
      sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
      batchSize: 1000
      timeout: "10m"
EOF

echo "7. Verifying Table with data migrations was created..."
kubectl get table users-with-datamigration -n schemahero-test-datamigration -o yaml | grep -A 5 dataMigrations

echo "8. Creating a Migration resource with data fields..."
cat <<EOF | kubectl apply -f -
apiVersion: schemas.schemahero.io/v1alpha4
kind: Migration
metadata:
  name: test-migration-with-dml
  namespace: schemahero-test-datamigration
spec:
  databaseName: testdb
  tableName: users
  tableNamespace: schemahero-test-datamigration
  generatedDDL: "ALTER TABLE users ADD COLUMN status varchar(20);"
  generatedDML: "UPDATE users SET status = 'active' WHERE status IS NULL;"
status:
  phase: PLANNED
  schemaMigrationStatus: PENDING
  dataMigrationStatus: PENDING
  estimatedDataRows: 1000
  estimatedDuration: "2m"
EOF

echo "9. Verifying Migration with data fields was created..."
kubectl get migration test-migration-with-dml -n schemahero-test-datamigration -o yaml | grep -E "(generatedDML|dataMigrationStatus|estimatedDataRows)"

echo "10. Cleaning up test resources..."
kubectl delete namespace schemahero-test-datamigration --wait=false

echo ""
echo "=== Integration Test PASSED ==="
echo "✅ CRDs successfully deployed"
echo "✅ Legacy Table resources (without data migrations) still work"
echo "✅ New Table resources with data migrations can be created"
echo "✅ Migration resources with data fields can be created" 