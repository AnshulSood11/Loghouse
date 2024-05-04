# Loghouse

## What is a Log?

A log is an append-only sequence of records. "logs” are binary-
encoded messages meant for other programs to read.
When you append a record to a log, the log assigns the record a unique and
sequential offset number that acts like the ID for that record. A log is like a
table that always orders the records by time and indexes each record by its
offset and time created.

## Understanding the code

### The Log Package

This fundamental working of a log is implemented in the [log](internal/log) package.

Since disks have a size limit, we can’t append to the same file forever, so we
split the log into a list of segments. The active segment is the segment
we actively write to. When we’ve filled the active segment, we create a new
segment and make it the active segment.

Each segment comprises a store file and an index file. Store file is where we store
the record data; we continually append records to this file. The Index file maps the
record offsets to their position in the store file, thus speeding up reads.
Reading a record given its offset is a two-step process: first you get the entry from
the index file for the record, which tells you the position of the record in the store
file, and then you read the record at that position in the store file.
Index files are small enough, so we
memory-map ([read more](https://mecha-mind.medium.com/understanding-when-and-how-to-use-memory-mapped-files-b94707df30e9))
them and make
operations on the file as fast as operating on in-memory data.

Structure of the log package is as follows:

* Record — not an actual struct, it refers to the data stored in our log.
* Store — the file we store records in.
* Index — the file we store index entries in.
* Segment — the abstraction that ties a store and an index together.
* Log — the abstraction that ties all the segments together.

### Networking with gRPC

gRPC is used for network interfacing of Loghouse. The gRPC server is implemented in
[server](internal/server) package.

The protobuf definitions are in [api/v1/log.proto](api/v1/log.proto). To generate go
code from proto, execute ``make compile`` and the structs and gRPC method stubs will
get generated. These stubs are then implemented in [server.go](internal/server/server.go).

## Distribute Loghouse

Since there's a hardware limitation on the size of data we can store on a single
server, at some point the only option is to add more servers to host our Loghouse
service. With the addition of nodes, challenges of server-to-server Service discovery,
Replication, Consensus and Load Balancing arise, which are tackled as follows:

### Server-to-server Service Discovery

Once we add a new node running our service, we need a mechanism to connect it to the
rest of the cluster. This Server-to-server service discovery is implemented using
[Serf](https://www.serf.io/) — a library that provides decentralized cluster membership,
failure detection, and orchestration.

The [discovery](internal/discovery) package integrates Serf where Membership is our
type wrapping Serf to provide discovery and cluster membership to our service.

### Replication and Consensus using Raft

Apart from scalability, distributing a service improves the availability as in if
any node goes down, another one will take its place and serve requests. To provide
availability, we need to ensure that the log written to one node get copied or replicated
to other nodes as well.

We might easily implement a replicator by making each node subscribe to other nodes using
the ConsumeStream rpc in [server](internal/server/server.go#L120). However, this will
cause an infinite replication of each log unless we introduce some kind of coordinator
which will keep a track of each log's offset, ensure that all nodes replicate that log
at the same offset exactly once (form consensus on the log's offset) and prevent infinite
replication.

There are many algorithms in distributed systems to solve this most popular being
Paxos and Raft. Raft is used in this project and utilizes [this](https://github.com/hashicorp/raft)
awesome implementation by Hashicorp. This piece of code sits in [distributed_log.go](internal/log/distributed_log.go)
where we setup Raft. Raft library uses a FSM interface to execute the business logic of
our service as well as the snapshotting and Restoring logic. This FSM is implemented in
[fsm.go](internal/log/fsm.go)
