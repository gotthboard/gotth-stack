package mailruntime

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"time"
)

const (
	MaxConfigurationArchiveBytes  int64 = 64 << 20
	MaxConfigurationMemberBytes   int64 = 16 << 20
	MaxConfigurationExpandedBytes int64 = 128 << 20
	configurationMode                   = 0o440
)

// VerifyConfigurationArchive proves that an archive is the exact deterministic
// regular-file set bound by a parsed release manifest. It does not extract.
func VerifyConfigurationArchive(reader io.Reader, manifest ReleaseManifest) error {
	if nilReader(reader) || !validReleaseManifest(manifest) {
		return ErrInvalidArchive
	}
	limited := &io.LimitedReader{R: reader, N: manifest.ConfigurationArchive.Size + 1}
	archiveHash := sha256.New()
	counted := &countingReader{reader: io.TeeReader(limited, archiveHash)}
	stream := tar.NewReader(counted)
	for index := range manifest.ConfigurationMembers {
		header, err := stream.Next()
		if err != nil || !validConfigurationHeader(header, manifest.ConfigurationMembers[index]) {
			return ErrInvalidArchive
		}
		memberHash := sha256.New()
		written, err := io.Copy(memberHash, stream)
		if err != nil || written != header.Size || digestHash(memberHash) != manifest.ConfigurationMembers[index].Digest {
			return ErrInvalidArchive
		}
	}
	if header, err := stream.Next(); err != io.EOF || header != nil {
		return ErrInvalidArchive
	}
	if err := drainZeroPadding(counted); err != nil {
		return err
	}
	if limited.N != 1 || counted.count != manifest.ConfigurationArchive.Size || digestHash(archiveHash) != manifest.ConfigurationArchive.Digest {
		return ErrInvalidArchive
	}
	return nil
}

func validConfigurationHeader(header *tar.Header, expected FileArtifact) bool {
	return header != nil && header.Format == tar.FormatUSTAR && header.Typeflag == tar.TypeReg && header.Name == expected.Name && header.Linkname == "" && header.Size == expected.Size && header.Mode == configurationMode && header.Uid == 0 && header.Gid == 0 && header.Uname == "" && header.Gname == "" && header.Devmajor == 0 && header.Devminor == 0 && header.ModTime.Equal(time.Unix(0, 0)) && header.AccessTime.IsZero() && header.ChangeTime.IsZero() && len(header.PAXRecords) == 0 && len(header.Xattrs) == 0
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (reader *countingReader) Read(value []byte) (int, error) {
	count, err := reader.reader.Read(value)
	reader.count += int64(count)
	return count, err
}

func drainZeroPadding(reader io.Reader) error {
	buffer := make([]byte, 32<<10)
	for {
		count, err := reader.Read(buffer)
		for _, value := range buffer[:count] {
			if value != 0 {
				return ErrInvalidArchive
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return ErrInvalidArchive
		}
	}
}

func digestHash(value hash.Hash) string {
	return "sha256:" + hex.EncodeToString(value.Sum(nil))
}
