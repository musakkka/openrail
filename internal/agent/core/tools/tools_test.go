// SPDX-License-Identifier: AGPL-3.0-only

package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/musakkka/openrail/pkg/proto"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		typ := byte(tar.TypeReg)
		if strings.HasSuffix(name, "/") {
			typ = tar.TypeDir
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: typ}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestToolDownloads(t *testing.T) {
	plain := []byte("#!/bin/sh\necho railpack\n")
	archive := tarGz(t, map[string]string{"./restic_0.18.0/": "", "./restic_0.18.0/restic": "restic-binary"})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/railpack":
			_, _ = w.Write(plain)
		case "/restic.tar.gz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	m := New(dir, srv.Client())
	m.Set([]proto.Tool{
		{Name: "railpack", Version: "0.40.1", URL: srv.URL + "/railpack", SHA256: sum(plain)},
		{Name: "restic", Version: "0.18.0", URL: srv.URL + "/restic.tar.gz", SHA256: "sha256:" + sum(archive), Path: "restic_0.18.0/restic"},
		{Name: "bad", Version: "1", URL: srv.URL + "/railpack", SHA256: strings.Repeat("0", 64)},
		{Name: "missing", Version: "1", URL: srv.URL + "/nope", SHA256: strings.Repeat("0", 64)},
		{Name: "nosum", Version: "1", URL: srv.URL + "/railpack"},
		{Name: "evil", Version: "../../x", URL: srv.URL + "/railpack", SHA256: sum(plain)},
		{Name: "wrongpath", Version: "1", URL: srv.URL + "/restic.tar.gz", SHA256: sum(archive), Path: "nothere"},
		{Name: "dirpath", Version: "1", URL: srv.URL + "/restic.tar.gz", SHA256: sum(archive), Path: "restic_0.18.0"},
	})
	ctx := context.Background()

	var wg sync.WaitGroup
	paths := make([]string, 5)
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := m.Path(ctx, "railpack")
			if err != nil {
				t.Error(err)
			}
			paths[i] = p
		}(i)
	}
	wg.Wait()
	got, _ := os.ReadFile(paths[0])
	if !bytes.Equal(got, plain) {
		t.Fatal("railpack content")
	}
	st, _ := os.Stat(paths[0])
	if st.Mode().Perm() != 0o755 {
		t.Fatal(st.Mode())
	}
	before := hits.Load()
	if _, err := m.Path(ctx, "railpack"); err != nil || hits.Load() != before {
		t.Fatal("a verified tool must not be downloaded again")
	}
	p, err := m.Path(ctx, "restic")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "restic-binary" {
		t.Fatal(string(b))
	}
	for name, want := range map[string]string{
		"bad":       "checksum",
		"missing":   "404",
		"nosum":     "no valid SHA-256",
		"evil":      "not allowed",
		"wrongpath": "has no file",
		"dirpath":   "not a regular file",
		"unknown":   "not configured",
	} {
		if _, err := m.Path(ctx, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", name, err, want)
		}
	}
	if _, ok := m.Known("restic"); !ok {
		t.Fatal("Known")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

// restic ships as a single bzip2 file (restic_<v>_linux_<arch>.bz2, see
// templates/tools.json). Installing those bytes as is gave containers a
// "binary" that failed with "Exec format error", so no restic backup (Redis,
// volumes) ever ran. The fixture was made with the real bzip2 tool.
func TestSingleFileBzip2DownloadIsDecompressed(t *testing.T) {
	bz, err := os.ReadFile("testdata/restic_linux_amd64.bz2")
	if err != nil {
		t.Fatal(err)
	}
	want := "#!/bin/sh\necho restic 0.19.1\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(bz) }))
	defer srv.Close()
	m := New(t.TempDir(), srv.Client())
	m.Set([]proto.Tool{{Name: "restic", Version: "v0.19.1", URL: srv.URL + "/restic.bz2", SHA256: sum(bz)}})
	p, err := m.Path(context.Background(), "restic")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != want {
		t.Fatalf("installed %q, want the decompressed binary %q", got, want)
	}
	if st, _ := os.Stat(p); st.Mode()&0o111 == 0 {
		t.Fatalf("installed tool is not executable: %v", st.Mode())
	}
}

// A host that already cached the compressed file (any agent before this fix)
// is healed on the next lookup, without downloading again.
func TestCachedCompressedToolIsHealed(t *testing.T) {
	bz, err := os.ReadFile("testdata/restic_linux_amd64.bz2")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "restic-v0.19.1")
	if err := os.WriteFile(dst, bz, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst+".sha256", []byte(sum(bz)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	m := New(dir, srv.Client())
	m.Set([]proto.Tool{{Name: "restic", Version: "v0.19.1", URL: srv.URL + "/restic.bz2", SHA256: sum(bz)}})
	p, err := m.Path(context.Background(), "restic")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(got), "#!/bin/sh") {
		t.Fatalf("cached tool is still compressed: %q", got[:min(len(got), 8)])
	}
	if hits.Load() != 0 {
		t.Fatal("healing a cached tool must not download it again")
	}
}

// A plain binary or script is never touched.
func TestUncompressedSingleFileIsInstalledAsIs(t *testing.T) {
	for _, b := range [][]byte{[]byte("#!/bin/sh\necho hi\n"), {0x7f, 'E', 'L', 'F', 2, 1, 1}} {
		f := filepath.Join(t.TempDir(), "x")
		if err := os.WriteFile(f, b, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, changed, err := decompressSingle(f, t.TempDir()); err != nil || changed {
			t.Fatalf("%q: changed=%v err=%v; an uncompressed file must be left alone", b[:4], changed, err)
		}
	}
}
