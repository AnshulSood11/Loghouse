package log

import (
	api "github.com/anshulsood11/loghouse/api/v1"
	"io"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Log struct {
	mu            sync.RWMutex //https://medium.com/bootdotdev/golang-mutexes-what-is-rwmutex-for-5360ab082626
	Dir           string       // directory is where we store the segments
	Config        Config
	activeSegment *segment
	segments      []*segment
}

func NewLog(dir string, c Config) (*Log, error) {
	if c.Segment.MaxStoreBytes == 0 {
		c.Segment.MaxStoreBytes = 1024
	}
	if c.Segment.MaxIndexBytes == 0 {
		c.Segment.MaxIndexBytes = 1024
	}
	l := &Log{
		Dir:    dir,
		Config: c,
	}
	return l, l.setup()
}

func (l *Log) setup() error {
	// Fetch the list of the segments on disk in the log's directory
	files, err := ioutil.ReadDir(l.Dir)
	if err != nil {
		return err
	}
	var baseOffsets []uint64
	for _, file := range files {
		// Trim file's extension from its name
		offStr := strings.TrimSuffix(
			file.Name(),
			path.Ext(file.Name()),
		)
		// Parse File's trimmed name to get the base offset
		off, _ := strconv.ParseUint(offStr, 10, 0)
		baseOffsets = append(baseOffsets, off)
	}
	// Sort the base offsets (in order from oldest to newest)
	sort.Slice(baseOffsets, func(i, j int) bool {
		return baseOffsets[i] < baseOffsets[j]
	})
	for i := 0; i < len(baseOffsets); i++ {
		if err = l.newSegment(baseOffsets[i]); err != nil {
			return err
		}
		// baseOffset contains dup for index and store, so we skip
		// the dup
		i++
	}
	if l.segments == nil {
		if err = l.newSegment(
			l.Config.Segment.InitialOffset,
		); err != nil {
			return err
		}
	}
	return nil
}

/*
newSegment creates a new segment with the base offset supplied, appends that segment
to the log’s slice of segments, and makes the new segment the active segment so that
subsequent append calls write to it.
*/
func (l *Log) newSegment(off uint64) error {
	s, err := newSegment(l.Dir, off, l.Config)
	if err != nil {
		return err
	}
	l.segments = append(l.segments, s)
	l.activeSegment = s
	return nil
}

/*
Append appends a record to the log. We append the record to the
active segment. Afterward, if the segment is at its max size (per the max size
configs), then we make a new active segment.
*/
func (l *Log) Append(record *api.Record) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock() // We can optimize this by making the locks per segment level
	off, err := l.activeSegment.Append(record)
	if err != nil {
		return 0, err
	}
	if l.activeSegment.IsMaxed() {
		err = l.newSegment(off + 1)
	}
	return off, err
}

/*
Read reads the record stored at the given offset
*/
func (l *Log) Read(off uint64) (*api.Record, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var s *segment

	leftPtr := 0
	rightPtr := len(l.segments) - 1
	for leftPtr < rightPtr {
		mid := leftPtr + (rightPtr-leftPtr+1)/2
		midSegment := l.segments[mid]
		if midSegment.baseOffset > off {
			rightPtr = mid - 1
		} else {
			leftPtr = mid
		}
	}
	segment := l.segments[leftPtr]
	if segment.baseOffset <= off && off < segment.nextOffset {
		s = segment
	}
	if s == nil || s.nextOffset <= off {
		return nil, api.ErrOffsetOutOfRange{Offset: off}
	}
	return s.Read(off)
}

/*
Close iterates over the segments and closes them
*/
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, segment := range l.segments {
		if err := segment.Close(); err != nil {
			return err
		}
	}
	return nil
}

/*
Remove closes the log and then removes its data
*/
func (l *Log) Remove() error {
	if err := l.Close(); err != nil {
		return err
	}
	return os.RemoveAll(l.Dir)
}

/*
Reset removes the log and then creates a new log to replace it.
*/
func (l *Log) Reset() error {
	if err := l.Remove(); err != nil {
		return err
	}
	return l.setup()
}

func (l *Log) LowestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.segments[0].baseOffset, nil
}

func (l *Log) HighestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	off := l.segments[len(l.segments)-1].nextOffset
	if off == 0 {
		return 0, nil
	}
	return off - 1, nil
}

/*
Truncate removes all segments whose highest offset is lower than
lowest. Because we don’t have disks with infinite space, we’ll periodically call
Truncate() to remove old segments whose data we (hopefully) have processed
by then and don’t need anymore.
*/
func (l *Log) Truncate(lowest uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var segments []*segment
	for _, s := range l.segments {
		if s.nextOffset <= lowest+1 {
			if err := s.Remove(); err != nil {
				return err
			}
			continue
		}
		segments = append(segments, s)
	}
	l.segments = segments
	return nil
}

/*
Reader returns an io.Reader to read the whole log. We’ll need this capability
when we implement coordinate consensus and need to support snapshots
and restoring a log.
*/
func (l *Log) Reader() io.Reader {
	l.mu.RLock()
	defer l.mu.RUnlock()
	readers := make([]io.Reader, len(l.segments))
	for i, segment := range l.segments {
		/*
			segment stores are wrapped by the originReader type for two reasons:
			First, to satisfy the io.Reader interface, so we can pass it
			into the io.MultiReader() call.
			Second, to ensure that we begin reading from the origin of the
			store and read its entire file.
		*/
		readers[i] = &originReader{segment.store, 0}
	}
	// MultiReader is used to concatenate the segments’ stores
	return io.MultiReader(readers...)
}

type originReader struct {
	*store
	off int64
}

func (o *originReader) Read(p []byte) (int, error) {
	n, err := o.ReadAt(p, o.off)
	o.off += int64(n)
	return n, err
}
