package journal

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplayRejectsFramingAndSemanticCorruption(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"magic":    func(t *testing.T, root string) { mutateLogByte(t, root, 0) },
		"checksum": func(t *testing.T, root string) { mutateLogByte(t, root, frameHeaderSize) },
		"oversize": func(t *testing.T, root string) {
			path := filepath.Join(root, "journal.log")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			binary.BigEndian.PutUint32(data[8:12], MaxPayloadSize+1)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"sequence-gap": func(t *testing.T, root string) {
			rewriteFirstRecord(t, root, func(record *journalRecord) { record.Sequence = 2 })
		},
		"identity": func(t *testing.T, root string) {
			rewriteFirstRecord(t, root, func(record *journalRecord) { record.InstallationID = "other" })
		},
		"schema": func(t *testing.T, root string) {
			rewriteFirstRecord(t, root, func(record *journalRecord) { record.SchemaVersion = 2 })
		},
		"noncanonical": rewriteFirstRecordNonCanonical,
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			journal, root, now := newTestJournal(t)
			recordTestApproval(t, journal, testPlan(t, false), now)
			_ = journal.Close()
			mutate(t, root)
			if _, _, err := Open(root, "installation-a"); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func rewriteFirstRecordNonCanonical(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "journal.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	length := int(binary.BigEndian.Uint32(data[8:12]))
	var record journalRecord
	if err := json.Unmarshal(data[frameHeaderSize:frameHeaderSize+length], &record); err != nil {
		t.Fatal(err)
	}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := encodeFrame(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, frame, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	writeTestHead(t, root, headFile{SchemaVersion: SchemaVersion, Sequence: record.Sequence, Digest: "sha256:" + hex.EncodeToString(sum[:])})
}

func TestReplayRejectsValidlyFramedInvalidTransition(t *testing.T) {
	journal, root, now := newTestJournal(t)
	plan := testPlan(t, false)
	recordTestApproval(t, journal, plan, now)
	operation := startTestOperation(t, journal, plan)
	head := journal.head
	_ = journal.Close()
	step := Step{
		OperationID: operation.ID, StepID: "apply-too-early", ComponentID: "database",
		Phase: PhaseApply, Mode: ModeMutation, Attempt: 1, IdempotencyDigest: digestA,
		Rollback: RollbackPolicy{ReferenceDigest: digestB}, StartedAt: now, Status: StepRunning,
	}
	record := journalRecord{
		SchemaVersion: SchemaVersion, Sequence: head.Sequence + 1, PreviousDigest: head.Digest,
		InstallationID: "installation-a", ObservedAt: now, Kind: recordStepStart, StepStart: &step,
	}
	appendRawRecord(t, root, record)
	if _, _, err := Open(root, "installation-a"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("error=%v", err)
	}
}

func TestReplayRejectsBrokenChainAndMismatchedStaleHead(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "chain", mutate: func(t *testing.T, root string) {
			rewriteLastRecord(t, root, func(record *journalRecord) { record.PreviousDigest = digestA })
		}},
		{name: "stale-head-digest", mutate: func(t *testing.T, root string) {
			writeTestHead(t, root, headFile{SchemaVersion: SchemaVersion, Sequence: 1, Digest: digestA})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal, root, now := newTestJournal(t)
			plan := testPlan(t, false)
			recordTestApproval(t, journal, plan, now)
			startTestOperation(t, journal, plan)
			_ = journal.Close()
			test.mutate(t, root)
			if _, _, err := Open(root, "installation-a"); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func mutateLogByte(t *testing.T, root string, offset int) {
	t.Helper()
	path := filepath.Join(root, "journal.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[offset] ^= 0xff
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func rewriteFirstRecord(t *testing.T, root string, mutate func(*journalRecord)) {
	t.Helper()
	path := filepath.Join(root, "journal.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	length := int(binary.BigEndian.Uint32(data[8:12]))
	var record journalRecord
	if err := json.Unmarshal(data[frameHeaderSize:frameHeaderSize+length], &record); err != nil {
		t.Fatal(err)
	}
	mutate(&record)
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := encodeFrame(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, frame, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	head := headFile{SchemaVersion: SchemaVersion, Sequence: record.Sequence, Digest: "sha256:" + hex.EncodeToString(sum[:])}
	writeTestHead(t, root, head)
}

func rewriteLastRecord(t *testing.T, root string, mutate func(*journalRecord)) {
	t.Helper()
	path := filepath.Join(root, "journal.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	offset := 0
	last := 0
	for offset < len(data) {
		last = offset
		length := int(binary.BigEndian.Uint32(data[offset+8 : offset+12]))
		offset += frameHeaderSize + length
	}
	length := int(binary.BigEndian.Uint32(data[last+8 : last+12]))
	var record journalRecord
	if err := json.Unmarshal(data[last+frameHeaderSize:last+frameHeaderSize+length], &record); err != nil {
		t.Fatal(err)
	}
	mutate(&record)
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := encodeFrame(payload)
	if err != nil {
		t.Fatal(err)
	}
	data = append(append([]byte{}, data[:last]...), frame...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	writeTestHead(t, root, headFile{SchemaVersion: SchemaVersion, Sequence: record.Sequence, Digest: "sha256:" + hex.EncodeToString(sum[:])})
}

func appendRawRecord(t *testing.T, root string, record journalRecord) {
	t.Helper()
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := encodeFrame(payload)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(root, "journal.log"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(frame); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	sum := sha256.Sum256(payload)
	writeTestHead(t, root, headFile{SchemaVersion: SchemaVersion, Sequence: record.Sequence, Digest: "sha256:" + hex.EncodeToString(sum[:])})
}

func writeTestHead(t *testing.T, root string, head headFile) {
	t.Helper()
	encoded, err := json.Marshal(head)
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(filepath.Join(root, "journal.head"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}
