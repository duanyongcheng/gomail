package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/app"
	"github.com/egoist/mygo"
)

const (
	updateCheckTimeout    = 15 * time.Second
	updateInstallTimeout  = 15 * time.Minute
	maxGitHubReleaseBytes = 256 << 10
	maxReleaseNotesRunes  = 4096
)

// NativeUpdate abstracts *mygo.Update so UpdateService can be unit-tested
// without touching the network or modifying the running binary.
type NativeUpdate interface {
	Version() string
	Notes() string
	Date() time.Time
	Install(ctx context.Context, progress func(downloaded, total int64)) error
}

type mygoNativeUpdate struct {
	up *mygo.Update
}

func (m mygoNativeUpdate) Version() string { return m.up.Version }
func (m mygoNativeUpdate) Notes() string   { return m.up.Notes }
func (m mygoNativeUpdate) Date() time.Time { return m.up.Date }
func (m mygoNativeUpdate) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	return m.up.Install(ctx, progress)
}

// GitHubReleaseInfo represents a verified latest release from GitHub's public API
// when native self-updates are unavailable (package-manager, read-only, or dev builds).
type GitHubReleaseInfo struct {
	Version string
	Notes   string
	Date    string
	URL     string
}

// UpdateProgress streams download and install progress to the frontend.
type UpdateProgress struct {
	Phase      string `json:"phase"`
	Version    string `json:"version"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Percent    int    `json:"percent"`
}

// UpdateStatus is the full thread-safe snapshot exposed to the frontend.
type UpdateStatus struct {
	CurrentVersion   string `json:"currentVersion"`
	CanAutoUpdate    bool   `json:"canAutoUpdate"`
	State            string `json:"state"`
	AvailableVersion string `json:"availableVersion,omitempty"`
	ReleaseNotes     string `json:"releaseNotes,omitempty"`
	ReleaseDate      string `json:"releaseDate,omitempty"`
	ReleaseURL       string `json:"releaseUrl,omitempty"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	Downloaded       int64  `json:"downloaded,omitempty"`
	Total            int64  `json:"total,omitempty"`
	Percent          int    `json:"percent,omitempty"`
	Error            string `json:"error,omitempty"`
}

// UpdateService manages update checks, signed delta/archive installation via
// mygo.Updater, and read-only GitHub release fallback when self-update is unavailable.
type UpdateService struct {
	OnStatus   func(UpdateStatus)
	OnProgress func(UpdateProgress)

	ctx            context.Context
	repo           string
	disabled       bool
	currentVersion func() string
	canAutoUpdate  func() bool
	checkNative    func(ctx context.Context) (NativeUpdate, error)
	checkGitHub    func(ctx context.Context, repo, current string) (*GitHubReleaseInfo, error)
	relaunchApp    func()

	mu         sync.Mutex
	status     UpdateStatus
	pending    NativeUpdate
	checking   bool
	installing bool
}

// NewUpdateService constructs a production UpdateService wired to mygo.Updater.
func NewUpdateService(ctx context.Context, repo string, disabled bool) *UpdateService {
	if ctx == nil {
		ctx = context.Background()
	}
	s := &UpdateService{
		ctx:      ctx,
		repo:     repo,
		disabled: disabled,
		currentVersion: func() string {
			return normalizeVersion(mygo.App.Version())
		},
		canAutoUpdate: func() bool {
			return mygo.Updater.Enabled()
		},
		checkNative: func(checkCtx context.Context) (NativeUpdate, error) {
			up, err := mygo.Updater.Check(checkCtx)
			if err != nil || up == nil {
				return nil, err
			}
			return mygoNativeUpdate{up: up}, nil
		},
		checkGitHub: fetchLatestGitHubRelease,
		relaunchApp: mygo.App.Relaunch,
	}
	s.status = UpdateStatus{
		State: "idle",
	}
	return s
}

func (s *UpdateService) baseStatusLocked() UpdateStatus {
	st := s.status
	if s.currentVersion != nil {
		st.CurrentVersion = normalizeVersion(s.currentVersion())
	}
	if s.disabled {
		st.CanAutoUpdate = false
	} else if st.State != "available" && st.State != "downloading" && st.State != "installed" {
		if s.canAutoUpdate != nil {
			st.CanAutoUpdate = s.canAutoUpdate()
		}
	}
	return st
}

func (s *UpdateService) emitStatus(st UpdateStatus) {
	if s.OnStatus != nil {
		s.OnStatus(st)
	}
}

func (s *UpdateService) emitProgress(p UpdateProgress) {
	if s.OnProgress != nil {
		s.OnProgress(p)
	}
}

// Status returns the current update state and capabilities without making network requests.
func (s *UpdateService) Status(app.Empty) UpdateStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.baseStatusLocked()
}

// Check queries for a newer version. Uses mygo.Updater when self-update is enabled,
// and falls back to read-only GitHub Releases lookup otherwise.
func (s *UpdateService) Check(app.Empty) (UpdateStatus, error) {
	s.mu.Lock()
	if s.disabled {
		st := s.baseStatusLocked()
		st.State = "up_to_date"
		st.Error = ""
		s.status = st
		s.mu.Unlock()
		return st, nil
	}
	if s.installing || s.status.State == "installed" || s.checking {
		st := s.baseStatusLocked()
		s.mu.Unlock()
		return st, nil
	}
	s.checking = true
	current := ""
	if s.currentVersion != nil {
		current = normalizeVersion(s.currentVersion())
	}
	canAuto := s.canAutoUpdate != nil && s.canAutoUpdate()
	s.status.CurrentVersion = current
	s.status.CanAutoUpdate = canAuto
	s.status.State = "checking"
	s.status.Error = ""
	checkingSnapshot := s.status
	s.mu.Unlock()

	s.emitStatus(checkingSnapshot)

	checkCtx, cancel := context.WithTimeout(s.ctx, updateCheckTimeout)
	defer cancel()

	var (
		nativeUp   NativeUpdate
		githubInfo *GitHubReleaseInfo
		checkErr   error
	)

	if canAuto && s.checkNative != nil {
		nativeUp, checkErr = s.checkNative(checkCtx)
		if checkErr != nil && isNotFoundError(checkErr) && s.checkGitHub != nil {
			// A release without signed update manifests (for example, a legacy release)
			// still falls back cleanly to read-only notification + manual download.
			slog.Warn("update manifest not found; falling back to GitHub release check")
			canAuto = false
			githubInfo, checkErr = s.checkGitHub(checkCtx, s.repo, current)
		}
	} else if s.checkGitHub != nil {
		canAuto = false
		githubInfo, checkErr = s.checkGitHub(checkCtx, s.repo, current)
	}

	s.mu.Lock()
	s.checking = false
	if s.installing || s.status.State == "installed" {
		st := s.baseStatusLocked()
		s.mu.Unlock()
		return st, nil
	}

	if checkErr != nil {
		slog.Warn("update check failed", "can_auto_update", canAuto)
		s.status = UpdateStatus{
			CurrentVersion: current,
			CanAutoUpdate:  canAuto,
			State:          "error",
			Error:          "Could not check for updates. Check your network connection and try again.",
		}
		st := s.status
		s.mu.Unlock()
		s.emitStatus(st)
		return st, errors.New(st.Error)
	}

	if nativeUp != nil {
		ver := normalizeVersion(nativeUp.Version())
		dateStr := ""
		if !nativeUp.Date().IsZero() {
			dateStr = nativeUp.Date().UTC().Format(time.RFC3339)
		}
		s.pending = nativeUp
		s.status = UpdateStatus{
			CurrentVersion:   current,
			CanAutoUpdate:    true,
			State:            "available",
			AvailableVersion: ver,
			ReleaseNotes:     trimReleaseNotes(nativeUp.Notes()),
			ReleaseDate:      dateStr,
			ReleaseURL:       releaseTagURL(s.repo, ver),
		}
		st := s.status
		s.mu.Unlock()
		s.emitStatus(st)
		return st, nil
	}

	if githubInfo != nil {
		ver := normalizeVersion(githubInfo.Version)
		s.pending = nil
		s.status = UpdateStatus{
			CurrentVersion:   current,
			CanAutoUpdate:    false,
			State:            "available",
			AvailableVersion: ver,
			ReleaseNotes:     trimReleaseNotes(githubInfo.Notes),
			ReleaseDate:      githubInfo.Date,
			ReleaseURL:       sanitizeReleaseURL(s.repo, githubInfo.URL, ver),
		}
		st := s.status
		s.mu.Unlock()
		s.emitStatus(st)
		return st, nil
	}

	s.pending = nil
	s.status = UpdateStatus{
		CurrentVersion: current,
		CanAutoUpdate:  canAuto,
		State:          "up_to_date",
	}
	st := s.status
	s.mu.Unlock()
	s.emitStatus(st)
	return st, nil
}

// Install downloads, verifies, and stages the pending signed update (preferring
// bsdiff delta patches automatically when available via mygo.Updater).
func (s *UpdateService) Install(app.Empty) (UpdateStatus, error) {
	s.mu.Lock()
	if s.disabled {
		st := s.baseStatusLocked()
		s.mu.Unlock()
		return st, errors.New("updates are disabled in selftest mode")
	}
	if s.installing {
		st := s.baseStatusLocked()
		s.mu.Unlock()
		return st, nil
	}
	if s.status.State == "installed" && s.status.InstalledVersion != "" {
		st := s.baseStatusLocked()
		s.mu.Unlock()
		return st, nil
	}
	pending := s.pending
	if pending == nil {
		st := s.baseStatusLocked()
		s.mu.Unlock()
		return st, errors.New("no installable update is ready; check for updates first")
	}
	targetVersion := normalizeVersion(pending.Version())
	s.installing = true
	s.status.State = "downloading"
	s.status.Downloaded = 0
	s.status.Total = 0
	s.status.Percent = 0
	s.status.Error = ""
	startSnapshot := s.status
	s.mu.Unlock()

	s.emitStatus(startSnapshot)
	s.emitProgress(UpdateProgress{
		Phase:   "downloading",
		Version: targetVersion,
	})

	installCtx, cancel := context.WithTimeout(s.ctx, updateInstallTimeout)
	defer cancel()

	err := pending.Install(installCtx, func(downloaded, total int64) {
		percent := 0
		if total > 0 {
			percent = int((downloaded * 100) / total)
			if percent > 100 {
				percent = 100
			}
			if percent < 0 {
				percent = 0
			}
		}
		s.mu.Lock()
		s.status.Downloaded = downloaded
		s.status.Total = total
		s.status.Percent = percent
		s.mu.Unlock()

		s.emitProgress(UpdateProgress{
			Phase:      "downloading",
			Version:    targetVersion,
			Downloaded: downloaded,
			Total:      total,
			Percent:    percent,
		})
	})

	s.mu.Lock()
	s.installing = false
	if err != nil {
		slog.Warn("update install failed", "target_version", targetVersion)
		s.status.State = "error"
		s.status.Error = "Could not download or verify the update. You can retry or download manually from GitHub Releases."
		st := s.status
		s.mu.Unlock()
		s.emitProgress(UpdateProgress{
			Phase:   "error",
			Version: targetVersion,
		})
		s.emitStatus(st)
		return st, errors.New(st.Error)
	}

	s.pending = nil
	s.status.State = "installed"
	s.status.InstalledVersion = targetVersion
	s.status.Percent = 100
	s.status.Error = ""
	st := s.status
	s.mu.Unlock()

	s.emitProgress(UpdateProgress{
		Phase:      "installed",
		Version:    targetVersion,
		Downloaded: st.Total,
		Total:      st.Total,
		Percent:    100,
	})
	s.emitStatus(st)
	return st, nil
}

// Relaunch restarts Pebble Go into an already-installed update.
func (s *UpdateService) Relaunch(app.Empty) (app.Empty, error) {
	s.mu.Lock()
	installed := s.status.State == "installed" && s.status.InstalledVersion != ""
	relaunch := s.relaunchApp
	s.mu.Unlock()

	if !installed {
		return app.Empty{}, errors.New("no installed update is pending restart")
	}
	if relaunch != nil {
		time.AfterFunc(80*time.Millisecond, relaunch)
	}
	return app.Empty{}, nil
}

type githubLatestReleaseResponse struct {
	TagName     string `json:"tag_name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

func fetchLatestGitHubRelease(ctx context.Context, repo, current string) (*GitHubReleaseInfo, error) {
	return fetchLatestGitHubReleaseFromURL(ctx, "https://api.github.com/repos/"+repo+"/releases/latest", repo, current, nil)
}

func fetchLatestGitHubReleaseFromURL(ctx context.Context, endpoint, repo, current string, client *http.Client) (*GitHubReleaseInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: updateCheckTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Pebble-Go/"+normalizeVersion(current))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github release check failed with status %d", resp.StatusCode)
	}

	var payload githubLatestReleaseResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGitHubReleaseBytes)).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Draft || payload.Prerelease || strings.TrimSpace(payload.TagName) == "" {
		return nil, nil
	}
	latest := normalizeVersion(payload.TagName)
	if CompareVersions(latest, current) <= 0 {
		return nil, nil
	}
	return &GitHubReleaseInfo{
		Version: latest,
		Notes:   trimReleaseNotes(payload.Body),
		Date:    payload.PublishedAt,
		URL:     sanitizeReleaseURL(repo, payload.HTMLURL, latest),
	}, nil
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

// CompareVersions compares two SemVer-style version strings (with or without a
// leading "v"). Returns 1 if a > b, -1 if a < b, and 0 if equal.
func CompareVersions(a, b string) int {
	aCore, aPre := splitVersion(a)
	bCore, bPre := splitVersion(b)

	aParts := strings.Split(aCore, ".")
	bParts := strings.Split(bCore, ".")
	n := max(len(aParts), len(bParts))
	for i := 0; i < n; i++ {
		av := parseSegment(aParts, i)
		bv := parseSegment(bParts, i)
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	if aPre == bPre {
		return 0
	}
	if aPre == "" {
		return 1
	}
	if bPre == "" {
		return -1
	}
	if aPre > bPre {
		return 1
	}
	return -1
}

func splitVersion(v string) (core, pre string) {
	v = normalizeVersion(v)
	if idx := strings.IndexByte(v, '+'); idx >= 0 {
		v = v[:idx]
	}
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

func parseSegment(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[index])
	if err != nil {
		return 0
	}
	return n
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(strings.ToLower(msg), "not found")
}

func releaseTagURL(repo, version string) string {
	if version == "" {
		return "https://github.com/" + repo + "/releases"
	}
	return "https://github.com/" + repo + "/releases/tag/v" + version
}

func sanitizeReleaseURL(repo, rawURL, version string) string {
	prefix := "https://github.com/" + repo + "/releases/"
	if strings.HasPrefix(rawURL, prefix) {
		return rawURL
	}
	return releaseTagURL(repo, version)
}

func trimReleaseNotes(notes string) string {
	notes = strings.TrimSpace(notes)
	runes := []rune(notes)
	if len(runes) <= maxReleaseNotesRunes {
		return notes
	}
	return string(runes[:maxReleaseNotesRunes]) + "\n..."
}
