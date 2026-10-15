package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const tlStudioReleasesAPI = "https://api.github.com/repos/pouramin/TL-Studio/releases?per_page=30"

var tlStudioUpdateHTTPClient = &http.Client{Timeout: 10 * time.Second}

type tlStudioReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type tlStudioRelease struct {
	TagName    string                 `json:"tag_name"`
	Draft      bool                   `json:"draft"`
	Prerelease bool                   `json:"prerelease"`
	HTMLURL    string                 `json:"html_url"`
	Assets     []tlStudioReleaseAsset `json:"assets"`
}

type tlStudioUpdateCandidate struct {
	CurrentVersion string
	LatestVersion  string
	ReleaseURL     string
	AssetName      string
	AssetURL       string
	SumsURL        string
}

type tlStudioUpdateStatus struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	Available      bool   `json:"available"`
	CanAutoUpdate  bool   `json:"canAutoUpdate"`
	ReleaseURL     string `json:"releaseURL,omitempty"`
	Channel        string `json:"channel"`
	Message        string `json:"message,omitempty"`
}

type tlStudioSemver struct {
	Major int
	Minor int
	Patch int
	Pre   []string
}

func parseTLStudioSemver(value string) (tlStudioSemver, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	if value == "" || value == "dev" {
		return tlStudioSemver{}, false
	}
	coreAndPre := strings.SplitN(value, "-", 2)
	core := strings.Split(coreAndPre[0], ".")
	if len(core) != 3 {
		return tlStudioSemver{}, false
	}
	numbers := make([]int, 3)
	for i, part := range core {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return tlStudioSemver{}, false
		}
		numbers[i] = n
	}
	result := tlStudioSemver{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}
	if len(coreAndPre) == 2 && strings.TrimSpace(coreAndPre[1]) != "" {
		result.Pre = strings.Split(coreAndPre[1], ".")
	}
	return result, true
}

func compareTLStudioSemver(a, b tlStudioSemver) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	if len(a.Pre) == 0 && len(b.Pre) == 0 {
		return 0
	}
	if len(a.Pre) == 0 {
		return 1
	}
	if len(b.Pre) == 0 {
		return -1
	}
	limit := len(a.Pre)
	if len(b.Pre) > limit {
		limit = len(b.Pre)
	}
	for i := 0; i < limit; i++ {
		if i >= len(a.Pre) {
			return -1
		}
		if i >= len(b.Pre) {
			return 1
		}
		ai, aErr := strconv.Atoi(a.Pre[i])
		bi, bErr := strconv.Atoi(b.Pre[i])
		switch {
		case aErr == nil && bErr == nil:
			if ai < bi {
				return -1
			}
			if ai > bi {
				return 1
			}
		case aErr == nil && bErr != nil:
			return -1
		case aErr != nil && bErr == nil:
			return 1
		default:
			if a.Pre[i] < b.Pre[i] {
				return -1
			}
			if a.Pre[i] > b.Pre[i] {
				return 1
			}
		}
	}
	return 0
}

func tlStudioReleaseSuffix(goos, goarch string) (string, bool) {
	switch {
	case goos == "windows" && goarch == "amd64":
		return "windows-x64.zip", true
	case goos == "linux" && goarch == "amd64":
		return "linux-x64.tar.gz", true
	case goos == "linux" && goarch == "arm64":
		return "linux-arm64.tar.gz", true
	case goos == "darwin" && goarch == "amd64":
		return "macos-x64.tar.gz", true
	case goos == "darwin" && goarch == "arm64":
		return "macos-arm64.tar.gz", true
	default:
		return "", false
	}
}

func selectTLStudioUpdate(current string, releases []tlStudioRelease, goos, goarch string) (tlStudioUpdateCandidate, bool) {
	currentVersion, ok := parseTLStudioSemver(current)
	if !ok {
		return tlStudioUpdateCandidate{}, false
	}
	suffix, ok := tlStudioReleaseSuffix(goos, goarch)
	if !ok {
		return tlStudioUpdateCandidate{}, false
	}
	includePrerelease := len(currentVersion.Pre) > 0

	var selected tlStudioUpdateCandidate
	var selectedVersion tlStudioSemver
	found := false
	for _, release := range releases {
		if release.Draft || (!includePrerelease && release.Prerelease) {
			continue
		}
		candidateVersion, ok := parseTLStudioSemver(release.TagName)
		if !ok || compareTLStudioSemver(candidateVersion, currentVersion) <= 0 {
			continue
		}
		var assetName, assetURL, sumsURL string
		for _, asset := range release.Assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), strings.ToLower(suffix)) {
				assetName = asset.Name
				assetURL = asset.BrowserDownloadURL
			}
			if asset.Name == "SHA256SUMS.txt" {
				sumsURL = asset.BrowserDownloadURL
			}
		}
		if assetURL == "" || sumsURL == "" {
			continue
		}
		if !found || compareTLStudioSemver(candidateVersion, selectedVersion) > 0 {
			selectedVersion = candidateVersion
			selected = tlStudioUpdateCandidate{
				CurrentVersion: current,
				LatestVersion:  release.TagName,
				ReleaseURL:     release.HTMLURL,
				AssetName:      assetName,
				AssetURL:       assetURL,
				SumsURL:        sumsURL,
			}
			found = true
		}
	}
	return selected, found
}

func fetchTLStudioUpdate(ctx context.Context) (tlStudioUpdateCandidate, tlStudioUpdateStatus, error) {
	currentParsed, currentOK := parseTLStudioSemver(version)
	channel := "stable"
	if !currentOK {
		return tlStudioUpdateCandidate{}, tlStudioUpdateStatus{
			CurrentVersion: version,
			Channel:        "development",
			Message:        "Development builds do not use the automatic release updater.",
		}, nil
	}
	if len(currentParsed.Pre) > 0 {
		channel = "prerelease"
	}
	if _, ok := tlStudioReleaseSuffix(runtime.GOOS, runtime.GOARCH); !ok {
		return tlStudioUpdateCandidate{}, tlStudioUpdateStatus{
			CurrentVersion: version,
			Channel:        channel,
			Message:        fmt.Sprintf("Automatic updates are not available for %s/%s.", runtime.GOOS, runtime.GOARCH),
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tlStudioReleasesAPI, nil)
	if err != nil {
		return tlStudioUpdateCandidate{}, tlStudioUpdateStatus{}, err
	}
	req.Header.Set("User-Agent", "TL-Studio/"+strings.TrimSpace(version))
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := tlStudioUpdateHTTPClient.Do(req)
	if err != nil {
		return tlStudioUpdateCandidate{}, tlStudioUpdateStatus{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return tlStudioUpdateCandidate{}, tlStudioUpdateStatus{}, fmt.Errorf("GitHub Releases returned HTTP %d", res.StatusCode)
	}
	var releases []tlStudioRelease
	if err := json.NewDecoder(res.Body).Decode(&releases); err != nil {
		return tlStudioUpdateCandidate{}, tlStudioUpdateStatus{}, err
	}

	candidate, available := selectTLStudioUpdate(version, releases, runtime.GOOS, runtime.GOARCH)
	status := tlStudioUpdateStatus{
		CurrentVersion: version,
		Available:      available,
		CanAutoUpdate:  false,
		Channel:        channel,
		Message:        "You are up to date.",
	}
	if available {
		status.LatestVersion = candidate.LatestVersion
		status.ReleaseURL = candidate.ReleaseURL
		status.Message = "A newer TL Studio release is available. Download and extract it into a new folder; your existing installation is preserved."
	}
	return candidate, status, nil
}

func registerUpdateRoutes(mux *http.ServeMux, state *appState) {
	mux.HandleFunc("GET /local/update", func(w http.ResponseWriter, r *http.Request) {
		_, status, err := fetchTLStudioUpdate(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, jsonError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /local/update", func(w http.ResponseWriter, r *http.Request) {
		candidate, status, err := fetchTLStudioUpdate(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, jsonError{Error: err.Error()})
			return
		}
		if !status.Available {
			writeJSON(w, http.StatusConflict, jsonError{Error: status.Message})
			return
		}
		if !status.CanAutoUpdate {
			writeJSON(w, http.StatusNotImplemented, jsonError{Error: "Download the release into a new folder. Automatic replacement is disabled until rollback-capable updates are validated."})
			return
		}
		if err := launchTLStudioUpdater(candidate, state.projectPath()); err != nil {
			writeJSON(w, http.StatusInternalServerError, jsonError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"started":       true,
			"latestVersion": candidate.LatestVersion,
			"releaseURL":    candidate.ReleaseURL,
		})
	})
}
