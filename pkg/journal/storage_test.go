package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestFramePayloadBoundaries(t *testing.T) {
	for _, size := range []int{MaxPayloadSize - 1, MaxPayloadSize} {
		frame, err := encodeFrame(bytes.Repeat([]byte{'x'}, size))
		if err != nil || len(frame) != frameHeaderSize+size {
			t.Fatalf("size=%d len=%d err=%v", size, len(frame), err)
		}
	}
	for _, size := range []int{0, MaxPayloadSize + 1} {
		if _, err := encodeFrame(bytes.Repeat([]byte{'x'}, size)); !errors.Is(err, ErrLimit) {
			t.Fatalf("size=%d err=%v", size, err)
		}
	}
}

func TestAppendStorageFailuresPoisonHandle(t *testing.T) {
	cases := map[string]func(*Journal){
		"write": func(journal *Journal) {
			journal.ops.write = func(*os.File, []byte) error { return errors.New("injected") }
		},
		"log-sync": func(journal *Journal) { journal.ops.sync = func(*os.File) error { return errors.New("injected") } },
		"rename": func(journal *Journal) {
			journal.ops.rename = func(*os.Root, string, string) error { return errors.New("injected") }
		},
		"directory-sync": func(journal *Journal) { journal.ops.syncRoot = func(*os.Root) error { return errors.New("injected") } },
	}
	for name, inject := range cases {
		t.Run(name, func(t *testing.T) {
			journal, _, now := newTestJournal(t)
			inject(journal)
			_, err := journal.RecordApproval(testPlan(t, false), ApprovalInput{ID: "approval-a", ActorID: "operator-a", AuthorityDigest: digestA, ExpiresAt: now.Add(testHour), SecretRevisions: []SecretRevision{}})
			if !errors.Is(err, ErrStorage) {
				t.Fatalf("error=%v", err)
			}
			if _, err := journal.Operation("operation-a"); !errors.Is(err, ErrRecoveryRequired) {
				t.Fatalf("poison=%v", err)
			}
		})
	}
}

func TestAppendLimitsAndClosedHandle(t *testing.T) {
	journal, _, now := newTestJournal(t)
	journal.recordCount = MaxRecords
	_, err := journal.RecordApproval(testPlan(t, false), ApprovalInput{ID: "approval-a", ActorID: "operator-a", AuthorityDigest: digestA, ExpiresAt: now.Add(testHour), SecretRevisions: []SecretRevision{}})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("record limit=%v", err)
	}
	journal.recordCount = 0
	if err := journal.log.Truncate(MaxLogSize); err != nil {
		t.Fatal(err)
	}
	_, err = journal.RecordApproval(testPlan(t, false), ApprovalInput{ID: "approval-b", ActorID: "operator-a", AuthorityDigest: digestA, ExpiresAt: now.Add(testHour), SecretRevisions: []SecretRevision{}})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("log limit=%v", err)
	}
	_ = journal.Close()
	if _, err := journal.Operation("operation-a"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed=%v", err)
	}
}

func TestLogSizeBoundaries(t *testing.T) {
	for _, delta := range []int64{-1, 0, 1, 4096} {
		t.Run(fmt.Sprintf("delta-%d", delta), func(t *testing.T) {
			journal, _, now := newTestJournal(t)
			record := journalRecord{Kind: recordCancel, ObservedAt: now, OperationID: "operation-a"}
			enveloped := recordWithEnvelope(record, 1, zeroDigest)
			enveloped.InstallationID = "installation-a"
			payload, err := json.Marshal(enveloped)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := encodeFrame(payload)
			if err != nil {
				t.Fatal(err)
			}
			start := MaxLogSize - int64(len(frame)) + delta
			if err := journal.log.Truncate(start); err != nil {
				t.Fatal(err)
			}
			_, err = journal.appendRecord(record)
			if delta <= 0 && err != nil {
				t.Fatalf("delta=%d err=%v", delta, err)
			}
			if delta > 0 && !errors.Is(err, ErrLimit) {
				t.Fatalf("delta=%d err=%v", delta, err)
			}
		})
	}
}

func TestCloseAndRecoveryStorageFailuresAreReported(t *testing.T) {
	t.Run("close", func(t *testing.T) {
		journal, _, _ := newTestJournal(t)
		closeLog := journal.ops.closeLog
		journal.ops.closeLog = func(file *os.File) error {
			_ = closeLog(file)
			return errors.New("injected")
		}
		if err := journal.Close(); !errors.Is(err, ErrStorage) {
			t.Fatalf("close=%v", err)
		}
	})
	t.Run("truncate", func(t *testing.T) {
		journal, _, _ := newTestJournal(t)
		journal.ops.truncate = func(*os.File, int64) error { return errors.New("injected") }
		if _, err := journal.truncateTail(0, 1, 0, Recovery{}); !errors.Is(err, ErrStorage) {
			t.Fatalf("truncate=%v", err)
		}
	})
	t.Run("truncate-sync", func(t *testing.T) {
		journal, _, _ := newTestJournal(t)
		journal.ops.sync = func(*os.File) error { return errors.New("injected") }
		if _, err := journal.truncateTail(0, 1, 0, Recovery{}); !errors.Is(err, ErrStorage) {
			t.Fatalf("sync=%v", err)
		}
	})
}

func TestReopenAfterEveryDurabilityCheckpoint(t *testing.T) {
	cases := map[string]struct {
		inject         func(*Journal)
		wantTruncated  bool
		wantHeadRepair bool
	}{
		"partial-log-write": {
			inject: func(journal *Journal) {
				write := journal.ops.write
				journal.ops.write = func(file *os.File, value []byte) error {
					if err := write(file, value[:len(value)/2]); err != nil {
						return err
					}
					return errors.New("injected")
				}
			},
			wantTruncated: true,
		},
		"log-sync": {
			inject:         func(journal *Journal) { journal.ops.sync = func(*os.File) error { return errors.New("injected") } },
			wantHeadRepair: true,
		},
		"head-write": {
			inject: func(journal *Journal) {
				write := journal.ops.write
				calls := 0
				journal.ops.write = func(file *os.File, value []byte) error {
					calls++
					if calls == 2 {
						return errors.New("injected")
					}
					return write(file, value)
				}
			},
			wantHeadRepair: true,
		},
		"head-sync": {
			inject: func(journal *Journal) {
				sync := journal.ops.sync
				calls := 0
				journal.ops.sync = func(file *os.File) error {
					calls++
					if calls == 2 {
						return errors.New("injected")
					}
					return sync(file)
				}
			},
			wantHeadRepair: true,
		},
		"head-rename": {
			inject: func(journal *Journal) {
				journal.ops.rename = func(*os.Root, string, string) error { return errors.New("injected") }
			},
			wantHeadRepair: true,
		},
		"directory-sync": {
			inject: func(journal *Journal) { journal.ops.syncRoot = func(*os.Root) error { return errors.New("injected") } },
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			journal, root, now := newTestJournal(t)
			test.inject(journal)
			_, err := journal.RecordApproval(testPlan(t, false), ApprovalInput{ID: "approval-a", ActorID: "operator-a", AuthorityDigest: digestA, ExpiresAt: now.Add(testHour), SecretRevisions: []SecretRevision{}})
			if !errors.Is(err, ErrStorage) {
				t.Fatalf("append=%v", err)
			}
			_ = journal.Close()
			reopened, recovery, err := Open(root, "installation-a")
			if err != nil {
				t.Fatalf("reopen=%v", err)
			}
			_ = reopened.Close()
			if (recovery.TruncatedBytes > 0) != test.wantTruncated || recovery.HeadRepaired != test.wantHeadRepair {
				t.Fatalf("recovery=%#v", recovery)
			}
		})
	}
}

const testHour = 60 * 60 * 1e9
