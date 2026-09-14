package journal

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

// replay validates the entire committed chain before returning any state. A
// torn uncommitted tail is the sole truncation case; fully framed corruption
// is never rewritten into apparent health.
func (journal *Journal) replay() (Recovery, error) {
	info, err := journal.log.Stat()
	if err != nil {
		return Recovery{}, ErrStorage
	}
	if info.Size() > MaxLogSize {
		return Recovery{}, ErrLimit
	}
	if _, err := journal.log.Seek(0, io.SeekStart); err != nil {
		return Recovery{}, ErrStorage
	}
	var recovery Recovery
	var offset int64
	sequence := uint64(0)
	digest := zeroDigest
	for offset < info.Size() {
		start := offset
		header := make([]byte, frameHeaderSize)
		read, err := io.ReadFull(journal.log, header)
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) && sequence == journal.head.Sequence && digest == journal.head.Digest {
				return journal.truncateTail(start, info.Size(), sequence, recovery)
			}
			return Recovery{}, ErrCorrupt
		}
		offset += int64(read)
		if !bytes.Equal(header[:8], frameMagic[:]) {
			return Recovery{}, ErrCorrupt
		}
		length := int(binary.BigEndian.Uint32(header[8:12]))
		if length <= 0 || length > MaxPayloadSize {
			return Recovery{}, ErrCorrupt
		}
		payload := make([]byte, length)
		read, err = io.ReadFull(journal.log, payload)
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) && sequence == journal.head.Sequence && digest == journal.head.Digest {
				return journal.truncateTail(start, info.Size(), sequence, recovery)
			}
			return Recovery{}, ErrCorrupt
		}
		offset += int64(read)
		sum := sha256.Sum256(payload)
		if !bytes.Equal(header[12:frameHeaderSize], sum[:]) {
			return Recovery{}, ErrCorrupt
		}
		var record journalRecord
		if err := decodeStrict(payload, &record); err != nil {
			return Recovery{}, err
		}
		canonical, err := json.Marshal(record)
		if err != nil || !bytes.Equal(canonical, payload) {
			return Recovery{}, ErrCorrupt
		}
		if err := record.validateShape(); err != nil {
			return Recovery{}, err
		}
		if record.Sequence != sequence+1 || record.PreviousDigest != digest || record.InstallationID != journal.installationID {
			return Recovery{}, ErrCorrupt
		}
		if err := journal.applyRecord(record); err != nil {
			return Recovery{}, ErrCorrupt
		}
		sequence = record.Sequence
		digest = "sha256:" + hex.EncodeToString(sum[:])
		if sequence == journal.head.Sequence && digest != journal.head.Digest {
			return Recovery{}, ErrCorrupt
		}
		if sequence > journal.head.Sequence+1 {
			return Recovery{}, ErrCorrupt
		}
		if sequence > MaxRecords {
			return Recovery{}, ErrLimit
		}
	}
	if sequence < journal.head.Sequence || (sequence == journal.head.Sequence && digest != journal.head.Digest) {
		return Recovery{}, ErrCorrupt
	}
	if sequence > journal.head.Sequence {
		if sequence != journal.head.Sequence+1 {
			return Recovery{}, ErrCorrupt
		}
		newHead := headFile{SchemaVersion: SchemaVersion, Sequence: sequence, Digest: digest}
		if err := journal.replaceHead(newHead); err != nil {
			return Recovery{}, ErrStorage
		}
		journal.head = newHead
		recovery.HeadRepaired = true
	}
	journal.recordCount = sequence
	recovery.RecordCount = sequence
	journal.markInterruptedSteps()
	return recovery, nil
}

// truncateTail is reachable only after replay proves the durable head names
// the complete prefix immediately before the incomplete final frame.
func (journal *Journal) truncateTail(start, size int64, sequence uint64, recovery Recovery) (Recovery, error) {
	if err := journal.ops.truncate(journal.log, start); err != nil {
		return Recovery{}, ErrStorage
	}
	if err := journal.ops.sync(journal.log); err != nil {
		return Recovery{}, ErrStorage
	}
	recovery.TruncatedBytes = size - start
	recovery.RecordCount = sequence
	journal.recordCount = sequence
	journal.markInterruptedSteps()
	return recovery, nil
}

// markInterruptedSteps changes only reconstructed in-memory state. The log
// still records the actual last durable event and therefore remains auditable.
func (journal *Journal) markInterruptedSteps() {
	for _, operation := range journal.operations {
		step := inFlight(operation)
		if step == nil {
			continue
		}
		step.Recovered = true
		if step.Mode == ModeMutation {
			operation.State = StateRecoveryRequired
		} else {
			operation.State = StateRetryRequired
		}
	}
}
