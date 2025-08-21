#!/bin/bash

echo "🚀 Starting SchemaHero Manual Test..."

# Clean up any existing test containers
docker rm -f schemahero-manual-test 2>/dev/null || true

echo "🐳 Starting PostgreSQL..."
docker run -d \
  --name schemahero-manual-test \
  -e POSTGRES_USER=schemahero \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=schemahero \
  -p 5432:5432 \
  postgres:15-alpine

echo "⏳ Waiting for PostgreSQL to start..."
while ! docker exec schemahero-manual-test pg_isready -U schemahero >/dev/null 2>&1; do
  sleep 1
done
echo "✅ PostgreSQL is ready!"

echo "📝 Planning migration..."
./bin/kubectl-schemahero plan \
  --driver postgres \
  --uri "postgres://schemahero:password@localhost:5432/schemahero?sslmode=disable" \
  --spec-file manual-test-example.yaml \
  --out planned.sql

echo "🔍 Generated SQL:"
echo "----------------------------------------"
cat planned.sql
echo "----------------------------------------"

echo "🚀 Applying migration..."
./bin/kubectl-schemahero apply \
  --driver postgres \
  --uri "postgres://schemahero:password@localhost:5432/schemahero?sslmode=disable" \
  --ddl planned.sql

echo "✅ Verifying results..."
docker exec schemahero-manual-test psql -U schemahero -d schemahero -c "\d test_users"
echo ""
docker exec schemahero-manual-test psql -U schemahero -d schemahero -c "SELECT * FROM test_users;"

echo "🧹 Cleaning up..."
docker rm -f schemahero-manual-test
rm planned.sql

echo "🎉 Test complete!" 