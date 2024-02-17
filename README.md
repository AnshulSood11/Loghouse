# Log Package

A log is an append-only sequence of records. "logs” are binary-
encoded messages meant for other programs to read.
When you append a record to a log, the log assigns the record a unique and
sequential offset number that acts like the ID for that record. A log is like a
table that always orders the records by time and indexes each record by its
offset and time created.

Concrete implementations of logs have to deal with us not having disks with
infinite space, which means we can’t append to the same file forever. So we
split the log into a list of segments. When the log grows too big, we free up
disk space by deleting old segments whose data we’ve already processed or
archived. This cleaning up of old segments can run in a background process
while our service can still produce to the active (newest) segment and consume
from other segments with no, or at least fewer, conflicts where goroutines
access the same data.

There’s always one special segment among the list of segments, and that’s
the active segment. We call it the active segment because it’s the only segment
we actively write to. When we’ve filled the active segment, we create a new
segment and make it the active segment.
Each segment comprises a store file and an index file. The segment’s store
file is where we store the record data; we continually append records to this
file. The segment’s index file is where we index each record in the store file.
The index file speeds up reads because it maps record offsets to their position
in the store file. Reading a record given its offset is a two-step process: first
you get the entry from the index file for the record, which tells you the position
of the record in the store file, and then you read the record at that position
in the store file. Since the index file requires only two small fields—the offset
and stored position of the record—the index file is much smaller than the
store file that stores all your record data. Index files are small enough that we can
memory-map ([read more](https://mecha-mind.medium.com/understanding-when-and-how-to-use-memory-mapped-files-b94707df30e9))
them and make operations on the file as fast as operating on in-memory data.

* Record—the data stored in our log.
* Store—the file we store records in.
* Index—the file we store index entries in.
* Segment—the abstraction that ties a store and an index together.
* Log—the abstraction that ties all the segments together.