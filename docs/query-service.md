<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->

# Query Service

1. [Overview](#1-overview)
    - [Core Responsibilities](#core-responsibilities)
2. [View Management](#2-view-management)
3. [API Operations](#3-api-operations)
    - [BeginView](#beginview)
    - [EndView](#endview)
    - [GetRows](#getrows)
    - [GetNamespacePolicies](#getnamespacepolicies)
    - [GetConfigTransaction](#getconfigtransaction)
4. [Performance Optimization](#4-performance-optimization)
    - [Connection Pooling](#connection-pooling)
    - [View and Query Batching](#view-and-query-batching)
    - [Batching Strategy](#batching-strategy)
    - [Lock-Free Design](#lock-free-design)
5. [Configuration](#5-configuration)

## 1. Overview

The Query Service is a component of the Fabric-X Committer architecture that provides efficient, consistent read-only access to the state database. It implements a view-based query mechanism that allows clients to retrieve data with specific isolation guarantees while optimizing database access through sophisticated batching techniques.

### Core Responsibilities

The Query Service performs several key functions:

1. **Namespace Access**: Provides access to data across different namespaces.
2. **Configuration Access**: Retrieves system configuration and policy information.
3. **Efficient Data Retrieval**: Optimizes database access by batching multiple queries and views together.
4. **Efficient View Management**: Creates and manages read-only transactions (AKA views) with configurable isolation levels and timeouts.

## 2. View Management

The Query Service implements a view-based query model that allows clients to:

1. Begin a view with specific isolation requirements
2. Execute multiple queries within the context of that view
3. End the view when finished

A views represents a read-only transaction that provide access to the database with configurable isolation levels:

- **Read Uncommitted**: Lowest isolation level, may read uncommitted changes
- **Read Committed**: Only reads committed data, but may see changes during the view
- **Repeatable Read**: Ensures the same data is returned for repeated reads
- **Serializable**: Highest isolation level, ensures transactions are completely isolated

Consistent access is provided only with **Repeatable Read** and **Serializable**.

Views can also be configured as deferred or non-deferred, affecting transaction behavior. In deferred mode, constraint violations and conflicts are checked only at transaction commit time, potentially allowing operations that would otherwise fail immediately. In non-deferred mode, these checks happen at the time of each operation, causing immediate failures when violations occur. This parameter allows applications to balance between early error detection and performance optimization.

## 3. API Operations

The Query Service implements the `QueryServiceServer` interface defined in the Protocol Buffers API:

### BeginView

```go
BeginView(ctx context.Context, params *protoqueryservice.ViewParameters) (*protoqueryservice.View, error)
```

Begins a new view with the specified parameters, including isolation level and timeout. Returns a view identifier that can be used for subsequent queries.
It may create a new read-only database transation, or reuse an existing one.
See [6. Performance Optimization](#6-performance-optimization) below.

### EndView

```go
EndView(ctx context.Context, view *protoqueryservice.View) (*protoqueryservice.View, error)
```

Ends a previously created view, releasing any associated resources.

### GetRows

```go
GetRows(ctx context.Context, query *protoqueryservice.Query) (*protoqueryservice.Rows, error)
```

Retrieves rows from the database based on the specified query parameters. The query includes:

- A view identifier
- A list of namespaces to query
- For each namespace, a list of keys to retrieve

### GetNamespacePolicies

```go
GetNamespacePolicies(ctx context.Context, _ *protoqueryservice.Empty) (*protoblocktx.NamespacePolicies, error)
```

Retrieves the policies associated with all namespaces from the metadata namespace.

### GetConfigTransaction

```go
GetConfigTransaction(ctx context.Context, _ *protoqueryservice.Empty) (*protoblocktx.ConfigTransaction, error)
```

Retrieves the current configuration transaction from the config namespace.

## 4. Performance Optimization

### Connection Pooling

The Query Service uses a connection pool to efficiently manage database connections:

1. **Connection Reuse**: Connections are reused across multiple queries to reduce overhead. 
2. **Connection Limits**: The pool size is configurable to prevent resource exhaustion.
3. **Transaction Management**: Transactions are created and managed as needed.

### View and Query Batching

To optimize database access, the Query Service implements a sophisticated batching mechanism:

1. **View Aggregation**: Multiple views are aggregated into a single read-only database transaction based on the view parameters and timing to reduce number of active database transactions and reduce the number of states the database have to maintain.
2. **Request Aggregation**: Multiple client requests are aggregated into batches based on namespace and timing to reduce number of round-trip to the database and reduce the number of tranactions the database have to manage.
3. **Concurrent Processing**: Batches are processed concurrently to maximize throughput.
4. **Result Distribution**: Results are distributed back to the appropriate clients.

### Batching Strategy

The Query Service employs a sophisticated batching strategy to optimize database access:

1. **Time-Based Batching**: Queries and views are collected over a configurable time window before being executed.
2. **Parameter-Based Grouping**: Queries and views with similar parameters (isolation level, deferred status) are grouped together.
3. **Namespace Separation**: Queries are separated by namespace to allow for parallel processing.

### Lock-Free Design

To minimize contention and maximize throughput, the Query Service uses lock-free data structures:

1. **Concurrent Maps**: For storing view and batch information.
2. **Atomic Operations**: For updating counters and state.
3. **Channel-Based Communication**: For signaling between components.

## 5. Configuration

The Query Service configuration is designed to optimize database access by efficiently batching queries while maintaining performance and resource constraints. The configuration is defined in a YAML file with the following key parameters:

### Server Configuration

- **Server**: Network configuration for the gRPC server, including address and port.

### Database Configuration

- **Database**: Parameters for connecting to the underlying database, including:
   - Connection string
   - Credentials
   - Connection pool settings
   - Timeout settings

### Query Batching Parameters

- **MinBatchKeys**: Minimum number of keys required to trigger a batch query execution. When a batch contains at least this many keys, it becomes eligible for execution.
- **MaxBatchWait**: Maximum time a batch will wait for additional keys before being executed, even if it hasn't reached MinBatchKeys. This ensures queries don't wait indefinitely.

### View Management Parameters

- **ViewAggregationWindow**: Time window during which views with identical parameters will be batched together. Views created within this window that have the same isolation level and deferred status will share database resources.
- **MaxAggregatedViews**: Maximum number of views that can be aggregated together in a single batch. This prevents excessive resource consumption.
- **MaxViewTimeout**: Maximum allowed lifetime for a view. This includes the time required to execute the last query in the view. If a query is executed after this timeout expires, it will be aborted.

### Connection Management

The number of database connections should be carefully configured based on the view parameters. The recommended minimum number of connections can be calculated as:

```
(MaxViewTimeout / ViewAggregationWindow) * <number-of-used-view-configuration-permutations>
```

Where the number of view configuration permutations is determined by the isolation levels and deferred status options in use (maximum of 8 permutations: 4 isolation levels × 2 deferred options).

If there are insufficient database connections available, queries will wait until a connection becomes available, which may exceed the MaxBatchWait time.

A sample configuration structure is defined in the `Config` type within the query package:

```go
type Config struct {
    Server                *connection.ServerConfig
    Database              *vc.DatabaseConfig
    MinBatchKeys          int
    MaxBatchWait          time.Duration
    ViewAggregationWindow time.Duration
    MaxAggregatedViews    int
    MaxViewTimeout        time.Duration
}
```
