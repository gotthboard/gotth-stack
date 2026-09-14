package journal

import (
	"testing"
	"time"
)

func FuzzStrictJSON(f *testing.F) {
	f.Add([]byte(`{"schema_version":1,"sequence":0,"digest":"` + zeroDigest + `"}`))
	f.Add([]byte(`{"schema_version":1,"schema_version":2}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > MaxPayloadSize {
			t.Skip()
		}
		var head headFile
		_ = decodeStrict(raw, &head)
	})
}

func FuzzFrameShape(f *testing.F) {
	f.Add(uint64(1), zeroDigest, "approval_recorded")
	f.Add(uint64(2), digestA, "invented")
	f.Fuzz(func(t *testing.T, sequence uint64, previous, kind string) {
		record := journalRecord{SchemaVersion: SchemaVersion, Sequence: sequence, PreviousDigest: previous, InstallationID: "installation-a", ObservedAt: testTime(), Kind: kind, Approval: &Approval{}}
		_ = record.validateShape()
	})
}

func testTime() time.Time { return time.Unix(1, 0).UTC() }
