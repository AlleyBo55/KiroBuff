package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ------------------------------------------------------------- fixtures

// tarGz builds a gzipped tar holding one file, matching what GoReleaser ships.
func tarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o755,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// server stands in for GitHub. Routes are the two the updater actually uses.
type server struct {
	tag      string
	archive  []byte
	sums     string
	apiCode  int
	assetGap bool
}

func (s server) start(t *testing.T) (Source, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		if s.apiCode != 0 && s.apiCode != http.StatusOK {
			w.WriteHeader(s.apiCode)
			return
		}
		fmt.Fprintf(w, `{"tag_name":%q,"published_at":"2026-07-26T08:15:00Z",`+
			`"body":"notes","html_url":"https://example.test/r/%s","draft":false}`, s.tag, s.tag)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			if s.sums == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			fmt.Fprint(w, s.sums)
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			if s.assetGap {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(s.archive)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return Source{API: ts.URL + "/api", Downloads: ts.URL + "/dl", Client: ts.Client()}, ts
}

// goodServer wires an archive and a matching checksums.txt for one tag.
func goodServer(t *testing.T, tag string, binary []byte) Source {
	t.Helper()
	archive := tarGz(t, "kirobuff", binary)
	asset := AssetName(tag, "darwin", "arm64")
	s := server{
		tag:     tag,
		archive: archive,
		sums:    fmt.Sprintf("%s  %s\n%s  other_file.tar.gz\n", sum(archive), asset, strings.Repeat("0", 64)),
	}
	src, _ := s.start(t)
	return src
}

// ------------------------------------------------------------- Latest

func TestLatestReadsTheTag(t *testing.T) {
	src := goodServer(t, "v9.9.9", []byte("binary"))
	rel, err := src.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v9.9.9" {
		t.Errorf("Tag=%q, want v9.9.9", rel.Tag)
	}
	if rel.PublishedAt != "2026-07-26T08:15:00Z" {
		t.Errorf("PublishedAt=%q", rel.PublishedAt)
	}
	if rel.URL == "" {
		t.Error("URL should be carried through for the -check output")
	}
}

func TestLatestReportsRateLimitingDistinctly(t *testing.T) {
	s := server{tag: "v1.0.0", apiCode: http.StatusForbidden}
	src, _ := s.start(t)
	_, err := src.Latest()
	if err == nil {
		t.Fatal("a 403 must be an error")
	}
	if !strings.Contains(err.Error(), "rate-limited") {
		t.Errorf("a 403 should be explained as rate limiting, got %q", err)
	}
}

func TestLatestRejectsAReleaseWithNoTag(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name":"","draft":false}`)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	src := Source{API: ts.URL + "/api", Client: ts.Client()}
	if _, err := src.Latest(); err == nil {
		t.Fatal("an empty tag must not be treated as a release")
	}
}

func TestLatestRejectsMalformedJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	src := Source{API: ts.URL + "/api", Client: ts.Client()}
	if _, err := src.Latest(); err == nil {
		t.Fatal("malformed JSON must be an error")
	}
}

// ------------------------------------------------------------- Newer

func TestNewerAcrossComponents(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.2.0", "v0.3.0", true},
		{"v0.2.0", "v0.2.1", true},
		{"v0.2.0", "v1.0.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.3.0", "v0.2.9", false},
		{"v1.0.0", "v0.9.9", false},
		{"0.2.0", "v0.2.1", true},
		// A pre-release suffix is discarded by ParseSemver, so the base version
		// decides. v0.3.0-rc1 is not newer than v0.3.0.
		{"v0.3.0", "v0.3.0-rc1", false},
	}
	for _, c := range cases {
		got, err := Newer(c.current, c.latest)
		if err != nil {
			t.Errorf("Newer(%q,%q) errored: %v", c.current, c.latest, err)
			continue
		}
		if got != c.want {
			t.Errorf("Newer(%q,%q)=%v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestNewerRefusesToJudgeADevBuild(t *testing.T) {
	_, err := Newer("dev", "v1.0.0")
	if !errors.Is(err, ErrDevBuild) {
		t.Fatalf("err=%v, want ErrDevBuild", err)
	}
}

func TestNewerRejectsAnUnparseableTag(t *testing.T) {
	_, err := Newer("v1.0.0", "nightly")
	if err == nil {
		t.Fatal("an unparseable published tag must be an error")
	}
	if errors.Is(err, ErrDevBuild) {
		t.Error("the local build is fine here; the remote tag is the problem")
	}
}

// ------------------------------------------------------------- AssetName

func TestAssetNameMatchesGoReleaser(t *testing.T) {
	// name_template: {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
	if got := AssetName("v0.3.0", "darwin", "arm64"); got != "kirobuff_0.3.0_darwin_arm64.tar.gz" {
		t.Errorf("got %q", got)
	}
	if got := AssetName("0.3.0", "linux", "amd64"); got != "kirobuff_0.3.0_linux_amd64.tar.gz" {
		t.Errorf("a tag without the v prefix should give the same name, got %q", got)
	}
	if got := AssetName("v0.3.0", "windows", "amd64"); !strings.HasSuffix(got, ".zip") {
		t.Errorf("Windows ships a zip, got %q", got)
	}
}

// ------------------------------------------------------------- Fetch

func TestFetchReturnsTheVerifiedBinary(t *testing.T) {
	want := []byte("#!/bin/sh\necho new\n")
	src := goodServer(t, "v9.9.9", want)
	got, err := src.Fetch("v9.9.9", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload=%q, want %q", got, want)
	}
}

func TestFetchRefusesOnChecksumMismatch(t *testing.T) {
	archive := tarGz(t, "kirobuff", []byte("tampered"))
	s := server{
		tag:     "v9.9.9",
		archive: archive,
		sums:    fmt.Sprintf("%s  %s\n", strings.Repeat("a", 64), AssetName("v9.9.9", "darwin", "arm64")),
	}
	src, _ := s.start(t)
	_, err := src.Fetch("v9.9.9", "darwin", "arm64")
	if err == nil {
		t.Fatal("a checksum mismatch must fail")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("err=%q, want a checksum mismatch", err)
	}
}

func TestFetchRefusesWhenChecksumsAreMissing(t *testing.T) {
	// install.sh warns and continues here. An update must not: a working binary
	// already exists, so an unverifiable one is a downgrade in safety.
	s := server{tag: "v9.9.9", archive: tarGz(t, "kirobuff", []byte("x")), sums: ""}
	src, _ := s.start(t)
	_, err := src.Fetch("v9.9.9", "darwin", "arm64")
	if err == nil {
		t.Fatal("a missing checksums.txt must fail the update")
	}
	if !strings.Contains(err.Error(), "cannot verify") {
		t.Errorf("err=%q should say verification was impossible", err)
	}
}

func TestFetchRefusesWhenChecksumsOmitThisAsset(t *testing.T) {
	archive := tarGz(t, "kirobuff", []byte("x"))
	s := server{tag: "v9.9.9", archive: archive, sums: sum(archive) + "  something_else.tar.gz\n"}
	src, _ := s.start(t)
	_, err := src.Fetch("v9.9.9", "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("err=%v, want a missing-entry error", err)
	}
}

func TestFetchReportsAMissingAsset(t *testing.T) {
	s := server{tag: "v9.9.9", assetGap: true, sums: "x  y\n"}
	src, _ := s.start(t)
	if _, err := src.Fetch("v9.9.9", "darwin", "arm64"); err == nil {
		t.Fatal("a 404 on the archive must be an error")
	}
}

func TestFetchDeclinesWindows(t *testing.T) {
	src := goodServer(t, "v9.9.9", []byte("x"))
	_, err := src.Fetch("v9.9.9", "windows", "amd64")
	if !errors.Is(err, ErrNoAsset) {
		t.Fatalf("err=%v, want ErrNoAsset for Windows", err)
	}
}

func TestFetchRejectsAnArchiveWithoutTheBinary(t *testing.T) {
	archive := tarGz(t, "README.md", []byte("docs"))
	s := server{
		tag:     "v9.9.9",
		archive: archive,
		sums:    fmt.Sprintf("%s  %s\n", sum(archive), AssetName("v9.9.9", "darwin", "arm64")),
	}
	src, _ := s.start(t)
	_, err := src.Fetch("v9.9.9", "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "did not contain") {
		t.Fatalf("err=%v, want a missing-binary error", err)
	}
}

func TestFetchRejectsAnEmptyBinary(t *testing.T) {
	archive := tarGz(t, "kirobuff", []byte(""))
	s := server{
		tag:     "v9.9.9",
		archive: archive,
		sums:    fmt.Sprintf("%s  %s\n", sum(archive), AssetName("v9.9.9", "darwin", "arm64")),
	}
	src, _ := s.start(t)
	_, err := src.Fetch("v9.9.9", "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err=%v, want an empty-binary error", err)
	}
}

func TestFetchRejectsNonGzip(t *testing.T) {
	archive := []byte("this is not gzip")
	s := server{
		tag:     "v9.9.9",
		archive: archive,
		sums:    fmt.Sprintf("%s  %s\n", sum(archive), AssetName("v9.9.9", "darwin", "arm64")),
	}
	src, _ := s.start(t)
	_, err := src.Fetch("v9.9.9", "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "gzip") {
		t.Fatalf("err=%v, want a gzip error", err)
	}
}

// A tar entry that walks out of its directory must not be honoured. Fetch
// returns bytes rather than writing files, so the guard is that only the base
// name is matched.
func TestExtractIgnoresPathTraversalNames(t *testing.T) {
	archive := tarGz(t, "../../../../etc/kirobuff", []byte("evil"))
	got, err := extract(archive, "kirobuff")
	if err != nil {
		t.Fatalf("the entry should still be readable by base name: %v", err)
	}
	if !bytes.Equal(got, []byte("evil")) {
		t.Error("extract should return the entry body")
	}
	// The important property: nothing was written to the traversed path.
	if _, err := os.Stat("/etc/kirobuff"); err == nil {
		t.Fatal("extract must never write to disk")
	}
}

// ------------------------------------------------------------- checksumFor

func TestChecksumForAcceptsBinaryMarker(t *testing.T) {
	body := "abc123  *kirobuff_1.0.0_linux_amd64.tar.gz\n"
	got, err := checksumFor(body, "kirobuff_1.0.0_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Errorf("got %q", got)
	}
}

func TestChecksumForSkipsMalformedLines(t *testing.T) {
	body := "\n# a comment\nonefield\n" +
		"DEADBEEF  kirobuff_1.0.0_linux_amd64.tar.gz\n"
	got, err := checksumFor(body, "kirobuff_1.0.0_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != "deadbeef" {
		t.Errorf("got %q, want the digest lowercased", got)
	}
}

// ------------------------------------------------------------- Replace

func TestReplaceSwapsTheBinary(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "kirobuff")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	if err := Replace(dest, []byte("new")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Errorf("body=%q, want new", body)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode=%o, the replacement must stay executable", fi.Mode().Perm())
	}
}

func TestReplaceLeavesNoTempFilesBehind(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "kirobuff")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	if err := Replace(dest, []byte("new")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "kirobuff" {
			t.Errorf("leftover file %q after a successful update", e.Name())
		}
	}
}

func TestReplaceWorksWhenTheTargetIsAbsent(t *testing.T) {
	// A first install through this path has nothing to move aside.
	dest := filepath.Join(t.TempDir(), "kirobuff")
	if err := Replace(dest, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("binary not written: %v", err)
	}
}

func TestReplaceFailsOnAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	err := Replace(filepath.Join(locked, "kirobuff"), []byte("new"))
	if err == nil {
		t.Fatal("writing into a read-only directory must fail")
	}
	if !strings.Contains(err.Error(), "cannot write to") {
		t.Errorf("err=%q should name the directory problem", err)
	}
}

// ------------------------------------------------------------- Self

func TestSelfReturnsAnExistingPath(t *testing.T) {
	self, err := Self()
	if err != nil {
		t.Fatal(err)
	}
	if self == "" {
		t.Fatal("Self returned an empty path")
	}
	if _, err := os.Stat(self); err != nil {
		t.Errorf("Self returned %q which does not exist: %v", self, err)
	}
}

func TestPlatformIsNotEmpty(t *testing.T) {
	goos, goarch := Platform()
	if goos == "" || goarch == "" {
		t.Errorf("Platform()=%q,%q", goos, goarch)
	}
}

// ------------------------------------------------------------- get

func TestGetReportsNotFound(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()
	src := Source{API: ts.URL, Client: ts.Client()}
	_, err := src.get(ts.URL + "/missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err=%v, want a not-found error", err)
	}
}

func TestGetReportsUnexpectedStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	src := Source{Client: ts.Client()}
	_, err := src.get(ts.URL)
	if err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("err=%v, want an unexpected-status error", err)
	}
}

func TestGetReportsNetworkFailure(t *testing.T) {
	src := Source{Client: http.DefaultClient}
	// Port 0 is never listening.
	if _, err := src.get("http://127.0.0.1:0/nope"); err == nil {
		t.Fatal("a connection failure must be an error")
	}
}

func TestDefaultPointsAtTheRealRepo(t *testing.T) {
	src := Default()
	if !strings.Contains(src.API, Repo) {
		t.Errorf("API=%q should reference %s", src.API, Repo)
	}
	if !strings.Contains(src.Downloads, Repo) {
		t.Errorf("Downloads=%q should reference %s", src.Downloads, Repo)
	}
	if src.Client == nil || src.Client.Timeout == 0 {
		t.Error("the default client must carry a timeout; a hung update is indistinguishable from a broken one")
	}
}
