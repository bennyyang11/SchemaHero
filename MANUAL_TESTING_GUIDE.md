# Manual Testing Guide for SchemaHero Development

## Overview

This guide shows you how to manually test SchemaHero without needing cloud services. The project already includes Docker-based testing infrastructure that makes local development easy.

## Prerequisites

✅ **You Already Have:**
- Docker (v20.10.8 detected)
- docker-compose
- SchemaHero binary built (`bin/kubectl-schemahero`)

## Quick Start: Test Current SchemaHero

### Option 1: Use Existing Integration Tests (Recommended)

The easiest way to see SchemaHero in action:

```bash
# Run a simple PostgreSQL test
cd integration/tests/postgres/basic-seed
make run

# This will:
# 1. Start PostgreSQL in Docker
# 2. Run SchemaHero plan command
# 3. Show you the generated SQL
# 4. Apply the migration
# 5. Clean up
```

### Option 2: Manual Step-by-Step Testing

#### Step 1: Start a Test Database

**PostgreSQL:**
```bash
# Start PostgreSQL with test data
docker run -d \
  --name schemahero-test-postgres \
  -e POSTGRES_USER=schemahero \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=schemahero \
  -p 5432:5432 \
  postgres:15-alpine

# Wait for it to start
docker exec schemahero-test-postgres pg_isready
```

**MySQL:**
```bash
# Start MySQL with test data
docker run -d \
  --name schemahero-test-mysql \
  -e MYSQL_ROOT_PASSWORD=password \
  -e MYSQL_USER=schemahero \
  -e MYSQL_PASSWORD=password \
  -e MYSQL_DATABASE=schemahero \
  -p 3306:3306 \
  mysql:8.0

# Wait for it to start (takes ~30 seconds)
docker exec schemahero-test-mysql mysqladmin ping -u schemahero -ppassword
```

#### Step 2: Create a Test Schema

Create `test-table.yaml`:
```yaml
database: schemahero
name: users
schema:
  postgres:  # or mysql:
    primaryKey: [id]
    columns:
      - name: id
        type: integer
      - name: email
        type: varchar(255)
      - name: name
        type: varchar(100)
seedData:
  rows:
    - columns:
      - column: id
        value:
          int: 1
      - column: email
        value:
          str: "test@example.com"
      - column: name
        value:
          str: "Test User"
```

#### Step 3: Test SchemaHero Commands

```bash
# Plan the migration (see what SQL it generates)
./bin/kubectl-schemahero plan \
  --driver postgres \
  --uri "postgres://schemahero:password@localhost:5432/schemahero?sslmode=disable" \
  --spec-file test-table.yaml \
  --out planned-migration.sql

# Look at the generated SQL
cat planned-migration.sql

# Apply the migration
./bin/kubectl-schemahero apply \
  --driver postgres \
  --uri "postgres://schemahero:password@localhost:5432/schemahero?sslmode=disable" \
  --ddl planned-migration.sql

# Verify it worked
docker exec schemahero-test-postgres \
  psql -U schemahero -d schemahero -c "\d users"
```

#### Step 4: Test Schema Changes

Modify your `test-table.yaml` to add a column:
```yaml
# Add this column to the existing schema
- name: status
  type: varchar(20)
  constraints:
    notNull: false
```

Then run plan again to see the ALTER statement:
```bash
./bin/kubectl-schemahero plan \
  --driver postgres \
  --uri "postgres://schemahero:password@localhost:5432/schemahero?sslmode=disable" \
  --spec-file test-table.yaml \
  --out migration-v2.sql

cat migration-v2.sql  # Should show: ALTER TABLE users ADD COLUMN status varchar(20);
```

## Advanced Testing Setup

### Option 3: Multi-Database Testing Environment

Create `docker-compose.yml` for testing multiple databases:

```yaml
version: '3.8'
services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: schemahero
      POSTGRES_PASSWORD: password
      POSTGRES_DB: schemahero
    ports:
      - "5432:5432"
    
  mysql:
    image: mysql:8.0
    environment:
      MYSQL_ROOT_PASSWORD: password
      MYSQL_USER: schemahero
      MYSQL_PASSWORD: password
      MYSQL_DATABASE: schemahero
    ports:
      - "3306:3306"
    
  # For testing your data migration features
  postgres-with-data:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: schemahero
      POSTGRES_PASSWORD: password
      POSTGRES_DB: schemahero
    ports:
      - "5433:5432"
    volumes:
      - ./test-data.sql:/docker-entrypoint-initdb.d/init.sql
```

Create `test-data.sql` with sample data:
```sql
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255),
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    created_at TIMESTAMP DEFAULT NOW()
);

INSERT INTO users (email, first_name, last_name) VALUES
    ('john@example.com', 'John', 'Doe'),
    ('jane@example.com', 'Jane', 'Smith'),
    ('bob@example.com', 'Bob', 'Johnson');
```

Start everything:
```bash
docker-compose up -d
```

## Testing Your Data Migration Feature

### Create Test Scenarios

Once you implement data migrations, you'll want to test scenarios like:

#### Scenario 1: Add Column with Default Values
```yaml
# Before: users table has id, email, first_name, last_name
# After: add status column and set default values

database: schemahero
name: users
schema:
  postgres:
    primaryKey: [id]
    columns:
      - name: id
        type: serial
      - name: email
        type: varchar(255)
      - name: first_name
        type: varchar(100)
      - name: last_name
        type: varchar(100)
      - name: status          # ← NEW COLUMN
        type: varchar(20)
dataMigrations:              # ← YOUR NEW FEATURE
  - name: "set-default-status"
    sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
```

#### Scenario 2: Data Transformation
```yaml
dataMigrations:
  - name: "create-full-name"
    sql: "UPDATE users SET full_name = first_name || ' ' || last_name WHERE full_name IS NULL"
```

#### Scenario 3: Conditional Migration
```yaml
dataMigrations:
  - name: "update-old-users"
    sql: "UPDATE users SET status = 'legacy' WHERE created_at < '2023-01-01'"
    conditions:
      - sql: "SELECT COUNT(*) FROM users WHERE created_at < '2023-01-01'"
        operator: ">"
        value: 0
```

## Development Testing Workflow

### Daily Development Loop

1. **Make Code Changes**
2. **Build Binary**: `make bin/kubectl-schemahero`
3. **Start Test Database**: Use existing Docker setup
4. **Test Your Changes**: Run plan/apply commands
5. **Verify Results**: Connect to database and check data

### Example Development Session

```bash
# 1. Start fresh database
docker run -d --name test-db \
  -e POSTGRES_USER=schemahero \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=schemahero \
  -p 5432:5432 postgres:15-alpine

# 2. Create initial data
docker exec test-db psql -U schemahero -d schemahero -c "
  CREATE TABLE users (id SERIAL PRIMARY KEY, email VARCHAR(255));
  INSERT INTO users (email) VALUES ('test@example.com'), ('user@example.com');
"

# 3. Create migration spec
cat > migration-test.yaml << EOF
database: schemahero
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
# Your new data migration feature would go here
EOF

# 4. Test planning
./bin/kubectl-schemahero plan \
  --driver postgres \
  --uri "postgres://schemahero:password@localhost:5432/schemahero?sslmode=disable" \
  --spec-file migration-test.yaml

# 5. Check what's in the database
docker exec test-db psql -U schemahero -d schemahero -c "SELECT * FROM users;"

# 6. Clean up
docker rm -f test-db
```

## Using Existing Integration Tests

### Run Specific Test Categories

```bash
# Test PostgreSQL functionality
cd integration/tests/postgres
make run  # Runs all PostgreSQL tests

# Test specific PostgreSQL version
PG_VERSION=15.1 make 15.1

# Test MySQL functionality  
cd ../mysql
make run

# Test seed data (closest to what you're building)
cd postgres/basic-seed
make run
```

### Understanding Test Output

When you run `make run`, it will:
1. **Start database in Docker** 
2. **Generate SQL** using `kubectl-schemahero plan`
3. **Compare with expected output** (`expect.sql` vs `out.sql`)
4. **Apply the migration** using `kubectl-schemahero apply`
5. **Clean up** the Docker container

If the test fails, you'll see a diff showing what was expected vs actual.

## Development Database Setup

### Option 4: Persistent Development Database

For longer development sessions, set up a persistent database:

```bash
# Create a development docker-compose.yml
cat > dev-compose.yml << EOF
version: '3.8'
services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: schemahero
      POSTGRES_PASSWORD: password
      POSTGRES_DB: schemahero
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./dev-init.sql:/docker-entrypoint-initdb.d/init.sql
      
volumes:
  postgres_data:
EOF

# Create initial data
cat > dev-init.sql << EOF
-- Sample tables for testing data migrations
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    created_at TIMESTAMP DEFAULT NOW(),
    status VARCHAR(20)
);

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    amount DECIMAL(10,2),
    created_at TIMESTAMP DEFAULT NOW()
);

-- Sample data for testing
INSERT INTO users (email, first_name, last_name) VALUES
    ('john@example.com', 'John', 'Doe'),
    ('jane@example.com', 'Jane', 'Smith'),
    ('bob@example.com', 'Bob', 'Johnson'),
    ('alice@example.com', 'Alice', 'Williams');

INSERT INTO orders (user_id, amount) VALUES
    (1, 99.99),
    (2, 149.50),
    (1, 75.00);
EOF

# Start the environment
docker-compose -f dev-compose.yml up -d

# Connect to test database
docker exec -it $(docker-compose -f dev-compose.yml ps -q postgres) \
  psql -U schemahero -d schemahero
```

## Testing Your Data Migration Implementation

### Test Database States

As you develop the data migration feature, you'll want to test these scenarios:

#### 1. **Empty Table** (schema + data migration)
```sql
-- Start with empty table
CREATE TABLE test_table (id SERIAL PRIMARY KEY);
```

#### 2. **Table with Existing Data** (data migration only)
```sql
-- Table with data that needs transformation
CREATE TABLE users (id SERIAL, email VARCHAR(255), name VARCHAR(100));
INSERT INTO users VALUES (1, 'test@example.com', 'Test User');
```

#### 3. **Large Dataset** (performance testing)
```sql
-- Generate large dataset for performance testing
INSERT INTO users (email, name) 
SELECT 
  'user' || i || '@example.com',
  'User ' || i
FROM generate_series(1, 100000) i;
```

### Validation Commands

After applying migrations, verify they worked:

```bash
# Check table structure
docker exec test-db psql -U schemahero -d schemahero -c "\d users"

# Check data contents
docker exec test-db psql -U schemahero -d schemahero -c "SELECT * FROM users LIMIT 10;"

# Check specific data migration results
docker exec test-db psql -U schemahero -d schemahero -c "SELECT status, COUNT(*) FROM users GROUP BY status;"
```

## Recommended Testing Approach

### Phase 1: Learn Current Functionality
1. **Run existing tests**: `cd integration/tests/postgres && make basic-seed`
2. **Modify examples**: Change `examples/tutorial/schema/airport-table.yaml` and test
3. **Understand CLI**: Try `./bin/kubectl-schemahero --help`

### Phase 2: Set Up Development Database
1. **Use docker-compose setup** (Option 4 above)
2. **Create test migration specs** with your new data migration syntax
3. **Test incrementally** as you build features

### Phase 3: Automated Testing
1. **Write unit tests** with the existing test patterns
2. **Add integration tests** following existing structure in `integration/tests/`
3. **Use existing CI patterns** from `.github/workflows/`

## Database Connection Examples

### PostgreSQL
```bash
URI="postgres://schemahero:password@localhost:5432/schemahero?sslmode=disable"
```

### MySQL  
```bash
URI="schemahero:password@tcp(localhost:3306)/schemahero?tls=false"
```

### SQLite (for simple testing)
```bash
URI="sqlite:///tmp/test.db"
```

## Debugging Tips

### View Generated SQL
```bash
# Always use --out flag to see what SQL will be generated
./bin/kubectl-schemahero plan \
  --driver postgres \
  --uri "$POSTGRES_URI" \
  --spec-file your-table.yaml \
  --out debug.sql

cat debug.sql  # Review before applying
```

### Check Database State
```bash
# PostgreSQL
docker exec test-db psql -U schemahero -d schemahero -c "
  SELECT table_name, column_name, data_type 
  FROM information_schema.columns 
  WHERE table_name = 'users';
"

# MySQL
docker exec test-db mysql -u schemahero -ppassword schemahero -e "
  DESCRIBE users;
"
```

### Monitor Logs
```bash
# Watch database logs during migration
docker logs -f test-db

# Check SchemaHero logs (when running in Kubernetes)
kubectl logs -f deployment/schemahero-manager -n schemahero-system
```

## Testing Your Data Migration Feature

### Create Migration Test Cases

When you implement data migrations, create these test files:

#### `test-data-migration-basic.yaml`
```yaml
database: schemahero
name: users
schema:
  postgres:
    primaryKey: [id]
    columns:
      - name: id
        type: serial
      - name: email
        type: varchar(255)
      - name: status
        type: varchar(20)
# This is what you'll implement:
dataMigrations:
  - name: "set-default-status"
    description: "Set status for existing users"
    sql: "UPDATE users SET status = 'active' WHERE status IS NULL"
```

#### `test-data-migration-complex.yaml`
```yaml
dataMigrations:
  - name: "create-full-name"
    description: "Combine first and last name"
    sql: "UPDATE users SET full_name = first_name || ' ' || last_name"
    conditions:
      - sql: "SELECT COUNT(*) FROM users WHERE first_name IS NOT NULL AND last_name IS NOT NULL"
        operator: ">"
        value: 0
```

### Test Database Setup for Data Migrations

Create `test-setup.sql`:
```sql
-- Create table with existing data for migration testing
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    created_at TIMESTAMP DEFAULT NOW()
);

-- Insert test data
INSERT INTO users (email, first_name, last_name) VALUES
    ('john@example.com', 'John', 'Doe'),
    ('jane@example.com', 'Jane', 'Smith'),
    ('bob@example.com', 'Bob', 'Johnson'),
    ('alice@example.com', 'Alice', 'Williams'),
    ('charlie@example.com', 'Charlie', 'Brown');

-- Create table for cross-table migration testing
CREATE TABLE user_preferences (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    preference_key VARCHAR(100),
    preference_value VARCHAR(255)
);

INSERT INTO user_preferences (user_id, preference_key, preference_value) VALUES
    (1, 'theme', 'dark'),
    (2, 'language', 'en'),
    (3, 'timezone', 'UTC');
```

Use it:
```bash
# Start database with test data
docker run -d --name test-db-with-data \
  -e POSTGRES_USER=schemahero \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=schemahero \
  -p 5432:5432 \
  -v $(pwd)/test-setup.sql:/docker-entrypoint-initdb.d/init.sql \
  postgres:15-alpine
```

## Performance Testing Setup

### Large Dataset Creation

```sql
-- Generate large dataset for performance testing
INSERT INTO users (email, first_name, last_name)
SELECT 
  'user' || i || '@example.com',
  'FirstName' || i,
  'LastName' || i
FROM generate_series(1, 100000) i;
```

### Monitor Performance
```bash
# Time your migration
time ./bin/kubectl-schemahero apply \
  --driver postgres \
  --uri "$POSTGRES_URI" \
  --ddl migration.sql

# Monitor database activity
docker exec test-db psql -U schemahero -d schemahero -c "
  SELECT query, state, query_start 
  FROM pg_stat_activity 
  WHERE state = 'active';
"
```

## No Cloud Services Needed!

**Why you DON'T need Supabase or cloud databases:**

✅ **Docker provides everything you need:**
- Real database instances
- Fast setup/teardown
- Multiple database types
- Isolated testing environments
- No network latency
- Free and unlimited

✅ **SchemaHero's existing test infrastructure:**
- Proven Docker configurations
- Database initialization scripts
- Automated testing patterns
- Multiple database versions

✅ **Local development advantages:**
- Faster iteration cycles
- No internet dependency
- No cloud costs
- Full control over database state
- Easy debugging and inspection

## Next Steps

1. **Try the Quick Start** examples above to understand current SchemaHero behavior
2. **Set up your development database** using docker-compose
3. **Follow the PRD implementation steps** using this testing setup
4. **Add your own test cases** as you implement data migration features

The existing Docker-based testing infrastructure is production-grade and used by the SchemaHero team themselves. It's perfect for developing and testing your data migration feature! 