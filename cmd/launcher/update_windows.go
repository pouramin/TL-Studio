//go:build windows

package main

import "errors"

// Never kill a running Agent and copy over its only working installation.
// Release discovery still supplies a manual download link on all platforms.
func launchTLStudioUpdater(candidate tlStudioUpdateCandidate, project string) error {
	return errors.New("automatic replacement is disabled; download the release into a new folder")
}
