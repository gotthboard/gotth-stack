package godaddydns

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"strings"
)

type artifactMembers struct {
	Executable, Manifest, ConfigMetadata, License, Checksums []byte
}

func verifyArtifact(reader io.Reader, pins artifactPins) (artifactMembers, error) {
	if reader == nil {
		return artifactMembers{}, ErrInvalidArtifact
	}
	compressed, err := io.ReadAll(io.LimitReader(reader, maxArchiveBytes+1))
	if err != nil || int64(len(compressed)) > maxArchiveBytes || digest(compressed) != pins.Archive {
		return artifactMembers{}, ErrInvalidArtifact
	}
	raw := bytes.NewReader(compressed)
	gz, err := gzip.NewReader(raw)
	if err != nil {
		return artifactMembers{}, ErrInvalidArtifact
	}
	gz.Multistream(false)
	tr := tar.NewReader(gz)
	root := "gotth-extension-godaddy-dns-" + ExpectedVersion + "-linux-amd64"
	expected := map[string]struct {
		mode  int64
		dir   bool
		limit int64
	}{
		root + "/":                            {0o755, true, 0},
		root + "/gotth-extension-godaddy-dns": {0o755, false, 32 << 20},
		root + "/manifest.json":               {0o644, false, 64 << 10},
		root + "/configuration-metadata.json": {0o644, false, 64 << 10},
		root + "/LICENSE":                     {0o644, false, 64 << 10},
		root + "/SHA256SUMS":                  {0o644, false, 64 << 10},
	}
	seen := map[string]bool{}
	files := map[string][]byte{}
	var expanded int64
	for {
		header, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			_ = gz.Close()
			return artifactMembers{}, ErrInvalidArtifact
		}
		spec, ok := expected[header.Name]
		if !ok || seen[header.Name] || path.Clean(header.Name) != strings.TrimSuffix(header.Name, "/") || header.Linkname != "" || len(header.PAXRecords) != 0 || len(header.Xattrs) != 0 || header.Uid != 0 || header.Gid != 0 || header.Mode != spec.mode || header.Format != tar.FormatUSTAR {
			_ = gz.Close()
			return artifactMembers{}, ErrInvalidArtifact
		}
		seen[header.Name] = true
		if spec.dir {
			if header.Typeflag != tar.TypeDir || header.Size != 0 {
				_ = gz.Close()
				return artifactMembers{}, ErrInvalidArtifact
			}
			continue
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size < 1 || header.Size > spec.limit || expanded+header.Size > maxExpandedBytes {
			_ = gz.Close()
			return artifactMembers{}, ErrInvalidArtifact
		}
		value, readErr := io.ReadAll(io.LimitReader(tr, spec.limit+1))
		if readErr != nil || int64(len(value)) != header.Size {
			_ = gz.Close()
			return artifactMembers{}, ErrInvalidArtifact
		}
		expanded += header.Size
		files[header.Name] = value
	}
	padding, paddingErr := io.ReadAll(io.LimitReader(gz, 10*1024+1))
	if paddingErr != nil || len(padding) > 10*1024 || !allZero(padding) || gz.Close() != nil || raw.Len() != 0 || len(seen) != len(expected) {
		return artifactMembers{}, ErrInvalidArtifact
	}
	result := artifactMembers{Executable: files[root+"/gotth-extension-godaddy-dns"], Manifest: files[root+"/manifest.json"], ConfigMetadata: files[root+"/configuration-metadata.json"], License: files[root+"/LICENSE"], Checksums: files[root+"/SHA256SUMS"]}
	if digest(result.Executable) != pins.Executable || digest(result.Manifest) != pins.Manifest || digest(result.ConfigMetadata) != pins.ConfigMetadata || digest(result.License) != pins.License {
		return artifactMembers{}, ErrInvalidArtifact
	}
	expectedSums := pins.License + "  LICENSE\n" + pins.ConfigMetadata + "  configuration-metadata.json\n" + pins.Executable + "  gotth-extension-godaddy-dns\n" + pins.Manifest + "  manifest.json\n"
	if !bytes.Equal(result.Checksums, []byte(expectedSums)) {
		return artifactMembers{}, ErrInvalidArtifact
	}
	return result, nil
}

func digest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func allZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}
