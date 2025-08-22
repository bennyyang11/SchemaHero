# Database-Specific Data Migration Guides

This directory contains detailed guides for implementing data migrations with each supported database type.

## Available Guides

### Fully Supported Databases
- **[PostgreSQL Guide](postgresql.md)** - Complete feature support with advanced capabilities
- **[MySQL Guide](mysql.md)** - Full support with MySQL-specific syntax handling
- **[CockroachDB Guide](cockroachdb.md)** - PostgreSQL-compatible with distributed considerations
- **[TimescaleDB Guide](timescaledb.md)** - Time-series optimizations with PostgreSQL syntax

### Well Supported Databases  
- **[SQLite Guide](sqlite.md)** - Local development and embedded applications
- **[RQLite Guide](rqlite.md)** - Distributed SQLite for cluster environments

### Limited Support Databases
- **[Cassandra Guide](cassandra.md)** - NoSQL with CQL support and limitations

## Quick Database Selection Guide

| Use Case | Recommended Database | Guide |
|----------|---------------------|-------|
| **Web Applications** | PostgreSQL, MySQL | [PostgreSQL](postgresql.md), [MySQL](mysql.md) |
| **Microservices** | PostgreSQL, CockroachDB | [PostgreSQL](postgresql.md), [CockroachDB](cockroachdb.md) |
| **Time Series Data** | TimescaleDB | [TimescaleDB](timescaledb.md) |
| **Local Development** | SQLite | [SQLite](sqlite.md) |
| **Distributed Systems** | CockroachDB, RQLite | [CockroachDB](cockroachdb.md), [RQLite](rqlite.md) |
| **NoSQL Requirements** | Cassandra | [Cassandra](cassandra.md) |

## Common Patterns Across Databases

### Standard SQL Operations (All Databases)
```sql
UPDATE table_name SET column = 'value' WHERE condition;
INSERT INTO table_name (col1, col2) VALUES ('val1', 'val2');
DELETE FROM table_name WHERE condition;
```

### Database-Specific Optimizations

#### PostgreSQL/CockroachDB/TimescaleDB
```sql
-- Use RETURNING for feedback
UPDATE users SET status = 'active' WHERE id = 123 RETURNING id, status;

-- Use UPSERT operations
INSERT INTO user_stats (user_id, login_count) 
VALUES (123, 1) 
ON CONFLICT (user_id) 
DO UPDATE SET login_count = user_stats.login_count + 1;
```

#### MySQL
```sql
-- Use ON DUPLICATE KEY UPDATE
INSERT INTO user_stats (user_id, login_count) 
VALUES (123, 1) 
ON DUPLICATE KEY UPDATE login_count = login_count + 1;

-- Use REPLACE for simple upserts
REPLACE INTO settings (key, value) VALUES ('theme', 'dark');
```

#### SQLite/RQLite
```sql
-- Use REPLACE for upserts
REPLACE INTO user_preferences (user_id, theme) VALUES (123, 'dark');

-- Use efficient date functions
UPDATE events SET created_date = date(created_at) WHERE created_date IS NULL;
```

#### Cassandra
```cql
-- Always specify primary key
UPDATE users SET last_login = toTimestamp(now()) WHERE user_id = ?;

-- Use parameterized queries for safety
INSERT INTO events (id, user_id, event_type) VALUES (?, ?, ?);
```

## Migration Planning by Database Type

### High-Performance Databases (PostgreSQL, MySQL, CockroachDB)
- ✅ Use large batch sizes (10K-50K rows)
- ✅ Leverage database transactions
- ✅ Use advanced SQL features
- ✅ Monitor connection pooling

### Embedded Databases (SQLite, RQLite)
- ✅ Use smaller batch sizes (1K-5K rows)
- ✅ Be aware of file locking (SQLite)
- ✅ Consider network latency (RQLite)
- ✅ Use simpler SQL patterns

### NoSQL Databases (Cassandra)
- ✅ Use very small batch sizes (100-1K rows)
- ✅ Always include primary key in updates
- ✅ Avoid complex WHERE clauses
- ✅ Handle eventual consistency

## Next Steps

1. **Choose your database** from the list above
2. **Read the specific guide** for your database type
3. **Review the examples** in the guide
4. **Start with simple migrations** to understand the patterns
5. **Gradually adopt advanced features** as you become comfortable

## Contributing

Help improve these guides by:
- **Adding new patterns** you've discovered
- **Sharing performance tips** for your database
- **Reporting issues** or unclear documentation
- **Contributing examples** from real-world usage 