package journal

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

const (
	frameHeaderSize = 8 + 4 + sha256.Size
	zeroDigest      = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

var frameMagic = [8]byte{'G', 'T', 'S', 'J', 'R', 'N', '0', '1'}

type storageOps struct {
	write    func(*os.File, []byte) error
	sync     func(*os.File) error
	truncate func(*os.File, int64) error
	closeLog func(*os.File) error
	rename   func(*os.Root, string, string) error
	syncRoot func(*os.Root) error
}

type Journal struct {
	mu             sync.Mutex
	root           *os.Root
	lock           *os.File
	log            *os.File
	installationID string
	now            func() time.Time
	ops            storageOps
	head           headFile
	recordCount    uint64
	approvals      map[string]Approval
	operations     map[string]*Operation
	poisoned       bool
	closed         bool
}

func defaultStorageOps() storageOps {
	return storageOps{
		write: func(file *os.File, value []byte) error {
			for len(value) > 0 {
				written, err := file.Write(value)
				if err != nil {
					return err
				}
				if written == 0 {
					return io.ErrShortWrite
				}
				value = value[written:]
			}
			return nil
		},
		sync:     func(file *os.File) error { return file.Sync() },
		truncate: func(file *os.File, size int64) error { return file.Truncate(size) },
		closeLog: func(file *os.File) error { return file.Close() },
		rename:   func(root *os.Root, oldName, newName string) error { return root.Rename(oldName, newName) },
		syncRoot: syncRoot,
	}
}

// appendRecord implements write-before-effect durability. Any storage failure
// poisons this handle because the caller cannot know which checkpoint reached
// stable storage until the journal is closed and replayed.
func (journal *Journal) appendRecord(record journalRecord) (journalRecord, error) {
	if journal.poisoned {
		return journalRecord{}, ErrRecoveryRequired
	}
	if journal.closed {
		return journalRecord{}, ErrClosed
	}
	if journal.recordCount >= MaxRecords {
		return journalRecord{}, ErrLimit
	}
	record.SchemaVersion = SchemaVersion
	record.Sequence = journal.head.Sequence + 1
	record.PreviousDigest = journal.head.Digest
	record.InstallationID = journal.installationID
	if record.ObservedAt.IsZero() {
		record.ObservedAt = observedTime(journal.now())
	} else {
		record.ObservedAt = observedTime(record.ObservedAt)
	}
	if err := record.validateShape(); err != nil {
		return journalRecord{}, ErrInvalidInput
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return journalRecord{}, ErrLimit
	}
	frame, err := encodeFrame(payload)
	if err != nil {
		return journalRecord{}, ErrLimit
	}
	digest := sha256.Sum256(payload)
	position, err := journal.log.Seek(0, io.SeekEnd)
	if err != nil || position+int64(len(frame)) > MaxLogSize {
		if err != nil {
			journal.poisoned = true
			return journalRecord{}, ErrStorage
		}
		return journalRecord{}, ErrLimit
	}
	if err := journal.ops.write(journal.log, frame); err != nil {
		journal.poisoned = true
		return journalRecord{}, ErrStorage
	}
	if err := journal.ops.sync(journal.log); err != nil {
		journal.poisoned = true
		return journalRecord{}, ErrStorage
	}
	newHead := headFile{SchemaVersion: SchemaVersion, Sequence: record.Sequence, Digest: "sha256:" + hex.EncodeToString(digest[:])}
	if err := journal.replaceHead(newHead); err != nil {
		journal.poisoned = true
		return journalRecord{}, ErrStorage
	}
	journal.head = newHead
	journal.recordCount++
	return record, nil
}

// encodeFrame applies the on-disk payload bound before allocating the frame.
// The checksum covers only the canonical JSON payload named by the header.
func encodeFrame(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > MaxPayloadSize {
		return nil, ErrLimit
	}
	digest := sha256.Sum256(payload)
	frame := make([]byte, frameHeaderSize+len(payload))
	copy(frame[:8], frameMagic[:])
	binary.BigEndian.PutUint32(frame[8:12], uint32(len(payload)))
	copy(frame[12:frameHeaderSize], digest[:])
	copy(frame[frameHeaderSize:], payload)
	return frame, nil
}

// replaceHead uses the documented file-sync, atomic rename, directory-sync
// sequence. The fixed temporary name is safe because the journal lock gives a
// single writer and stale temporary files are removed only after validation.
func (journal *Journal) replaceHead(head headFile) error {
	encoded, err := json.Marshal(head)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_ = journal.root.Remove("journal.head.tmp")
	file, err := journal.root.OpenFile("journal.head.tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	ok := false
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		if !ok {
			_ = journal.root.Remove("journal.head.tmp")
		}
	}()
	if err := journal.ops.write(file, encoded); err != nil {
		return err
	}
	if err := journal.ops.sync(file); err != nil {
		return err
	}
	closed = true
	if err := file.Close(); err != nil {
		return err
	}
	if err := journal.ops.rename(journal.root, "journal.head.tmp", "journal.head"); err != nil {
		return err
	}
	if err := journal.ops.syncRoot(journal.root); err != nil {
		return err
	}
	ok = true
	return nil
}

func syncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
