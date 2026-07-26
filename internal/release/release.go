// Package release checks for a newer published release and replaces the running
// binary with it.
//
// It fails closed on verification. install.sh warns and continues when
// checksums.txt is missing, because a first install with no binary at all is
// worse than an unverified one. An update is the opposite case: there is already
// a working binary on disk, so an unverifiable download has nothing to offer and
// is refused.
//
// Stdlib only, like the rest of the project. Nothing here needs a dependency.
package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AlleyBo55/KiroBuff/semver"
)

// Repo is the upstream project. Kept as a constant rather than a flag: pointing
// a self-updater at an arbitrary repository is a way to install someone else's
// binary under this one's name.
const Repo = "AlleyBo55/KiroBuff"

// requestTimeout bounds every network call. An update that hangs is
// indistinguishable from one that is broken, and this runs interactively.
const requestTimeout = 30 * time.Second

// maxDownload caps what will be read from the network. The real archives are
// about 3 MB; 64 MB leaves generous headroom while stopping a malicious or
// misconfigured endpoint from filling the disk.
const maxDownload = 64 << 20

// ErrNoAsset means the release exists but carries nothing for this platform.
var ErrNoAsset = errors.New("no release asset for this platform")

// ErrDevBuild means the running binary has no release version to compare
// against, so "newer" is not a question that can be answered.
var ErrDevBuild = errors.New("this is a development build, not a released version")

// Source is where releases are read from. The zero value is not useful; call
// Default. The fields exist so tests can point at a local server instead of
// GitHub.
type Source struct {
	// API is the repository API root, without a trailing slash.
	API string
	// Downloads is the release-asset root, without a trailing slash.
	Downloads string
	// Client performs the requests. nil means a client with a sane timeout.
	Client *http.Client
}

// Default returns the upstream source.
func Default() Source {
	return Source{
		API:       "https://api.github.com/repos/" + Repo,
		Downloads: "https://github.com/" + Repo + "/releases/download",
		Client:    &http.Client{Timeout: requestTimeout},
	}
}

// Release is a published release, reduced to what an update needs.
type Release struct {
	Tag         string
	PublishedAt string
	Notes       string
	URL         string
}

// Latest returns the most recent published release.
//
// The unauthenticated API is rate-limited per IP. A 403 or 429 is reported as
// such rather than as a generic failure, because the difference decides whether
// retrying helps.
func (s Source) Latest() (Release, error) {
	body, err := s.get(s.API + "/releases/latest")
	if err != nil {
		return Release{}, err
	}
	var payload struct {
		TagName     string `json:"tag_name"`
		PublishedAt string `json:"published_at"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
		Draft       bool   `json:"draft"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, fmt.Errorf("parse release response: %w", err)
	}
	if payload.TagName == "" {
		return Release{}, errors.New("the latest release has no tag; the repository may have no releases yet")
	}
	return Release{
		Tag:         payload.TagName,
		PublishedAt: payload.PublishedAt,
		Notes:       payload.Body,
		URL:         payload.HTMLURL,
	}, nil
}

// Newer reports whether latest is a higher version than current.
//
// An unparseable current version returns ErrDevBuild: a locally built binary
// reports "dev", and treating that as older than everything would let a stray
// `kirobuff update` overwrite a build someone is actively debugging.
func Newer(current, latest string) (bool, error) {
	cur, err := semver.ParseSemver(current)
	if err != nil {
		return false, ErrDevBuild
	}
	next, err := semver.ParseSemver(latest)
	if err != nil {
		return false, fmt.Errorf("the published tag %q is not a semver", latest)
	}
	switch {
	case next.Major != cur.Major:
		return next.Major > cur.Major, nil
	case next.Minor != cur.Minor:
		return next.Minor > cur.Minor, nil
	default:
		return next.Patch > cur.Patch, nil
	}
}

// AssetName returns the archive filename GoReleaser produces for a platform.
// It must stay in step with the name_template in .goreleaser.yaml.
func AssetName(tag, goos, goarch string) string {
	num := strings.TrimPrefix(tag, "v")
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("kirobuff_%s_%s_%s.%s", num, goos, goarch, ext)
}

// Fetch downloads the release binary for a platform and returns its bytes.
//
// The archive's checksum is verified against checksums.txt before anything is
// extracted. A mismatch, a missing checksums.txt, or an absent entry for this
// archive is an error, not a warning.
func (s Source) Fetch(tag, goos, goarch string) ([]byte, error) {
	if goos == "windows" {
		// GoReleaser ships Windows as a zip. Extracting one is a different code
		// path that nothing here has been able to test, and a self-updater that
		// silently mishandles an archive format is worse than one that declines.
		return nil, fmt.Errorf("%w: Windows archives are zip, which this updater "+
			"does not extract; reinstall from %s/releases/tag/%s",
			ErrNoAsset, "https://github.com/"+Repo, tag)
	}
	asset := AssetName(tag, goos, goarch)
	base := s.Downloads + "/" + tag

	archive, err := s.get(base + "/" + asset)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset, err)
	}

	sums, err := s.get(base + "/checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("cannot verify the download: checksums.txt is unavailable (%w)", err)
	}
	want, err := checksumFor(string(sums), asset)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("checksum mismatch for %s: got %s, want %s", asset, got, want)
	}

	return extract(archive, "kirobuff")
}

// checksumFor finds one archive's digest in a checksums.txt body.
func checksumFor(body, asset string) (string, error) {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		// GoReleaser writes "<sum>  <name>"; some tools prefix the name with *.
		if strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", asset)
}

// extract pulls one named file out of a gzipped tar.
func extract(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("not a gzip archive: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("archive did not contain %s", name)
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		// Compare the base name only, and never join the archive's path onto a
		// destination: a tar entry named ../../x is how an archive escapes the
		// directory it is extracted into.
		if filepath.Base(h.Name) != name {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(tr, maxDownload))
		if err != nil {
			return nil, fmt.Errorf("read %s from archive: %w", name, err)
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("%s in the archive is empty", name)
		}
		return body, nil
	}
}

// Replace overwrites dest with payload.
//
// The new file is written beside the target so the final step is a rename on the
// same filesystem, which is atomic: an interrupted update leaves either the old
// binary or the new one, never a half-written file. The old binary is moved
// aside first because Windows refuses to rename over a running executable.
func Replace(dest string, payload []byte) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".kirobuff-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil { //nolint:gosec // an executable must be executable
		return err
	}

	old := dest + ".old"
	_ = os.Remove(old)
	if err := os.Rename(dest, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot move the current binary aside: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		// Put the working binary back rather than leaving nothing installed.
		_ = os.Rename(old, dest)
		return fmt.Errorf("cannot install the new binary: %w", err)
	}
	// Succeeds on Unix. On Windows the running image stays locked, so the file
	// lingers until the next update; that is why it has a predictable name.
	_ = os.Remove(old)
	return nil
}

// Self returns the path of the running executable, with symlinks resolved.
//
// Resolving matters: a Homebrew or /usr/local/bin symlink points at the real
// binary, and replacing the link instead of its target would leave the old
// binary in place and the link no longer shared.
func Self() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// Platform returns the GOOS and GOARCH the running binary was built for.
func Platform() (string, string) { return runtime.GOOS, runtime.GOARCH }

// get performs one GET and returns the body, with HTTP status mapped to an
// error a person can act on.
func (s Source) get(url string) ([]byte, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	// The context bounds DNS and connection setup as well, which Client.Timeout
	// alone does not make obvious.
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kirobuff/"+semver.Get().Version)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("not found: %s", url)
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New("GitHub rate-limited this request; wait a few minutes and try again")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("unexpected status %s from %s", resp.Status, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
}
