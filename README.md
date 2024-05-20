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

* Record — not an actual struct, it refers to the data stored in the log.
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
server, at some point the only option is to add more servers to host this Loghouse
service. With the addition of nodes, challenges of server-to-server Service discovery,
Replication, Consensus and Load Balancing arise, which are tackled as follows:

### Server-to-server Service Discovery

Once we add a new node running our service, we need a mechanism to connect it to the
rest of the cluster. This Server-to-server service discovery is implemented using
[Serf](https://www.serf.io/) — a library that provides decentralized cluster membership,
failure detection, and orchestration.

The [discovery](internal/discovery) package integrates Serf where Membership is the
type wrapping Serf to provide discovery and cluster membership to this service.

### Replication and Consensus using Raft

Apart from scalability, distributing a service improves the availability as in if
any node goes down, another one will take its place and serve requests. To provide
availability, we need to ensure that the log written to one node get copied or replicated
to other nodes as well.

We might easily implement a replicator by making each node subscribe to other nodes using
the [ConsumeStream](internal/server/server.go#L120) rpc. However, this will
cause an infinite replication of each log unless we introduce some kind of coordinator
which will keep a track of each log's offset, ensure that all nodes replicate that log
at the same offset exactly once (form consensus on the log's offset) and prevent infinite
replication.

There are many algorithms in distributed systems to solve this most popular being
Paxos and Raft. Raft is used in this project and utilizes [this](https://github.com/hashicorp/raft)
awesome implementation by Hashicorp. This piece of code sits in [distributed_log.go](internal/log/distributed_log.go)
where we set up Raft. Raft library uses a FSM interface to execute the business logic of
Loghouse as well as the Snapshotting and Restoring logic. This FSM is implemented in
[fsm.go](internal/log/fsm.go)

Membership and distributed_log are stitched together in [agent.go](internal/agent/agent.go). 
Agent is the starting point of the service and will set up everything - distributed log, 
grpc server, membership handling and authorization.

### Encryption, Authentication and Authorization

Client-server connection is authenticated using mTLS. To generate the certificates execute
``make gencert``. This uses [cfssl](https://github.com/cloudflare/cfssl) to generate
client and server self-signed certificates from the config files present in [resources](resources).
These are then used to authenticate and encrypt gRPC and Raft connections. RSA-2048 is used
for encryption on both client and server.

Authorization is implemented using ACLs. [Casbin](https://github.com/casbin/casbin) is used to
provide the ACL functionality. In [authorizer.go](internal/auth/authorizer.go) we set up Casbin
to enforce the policies defined in [policy.csv](resources/policy.csv) as per the model: [model.conf](resources/model.conf).
Authorization takes place during Produce/Consume RPCs in [server.go](internal/server/server.go).