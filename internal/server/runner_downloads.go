package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

type runnerDownload struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
	path     string
}

var runnerDownloadCandidates = []struct {
	Names    []string
	Platform string
	Arch     string
}{
	{[]string{"projectboard-runner-windows-amd64.exe", "projectboard-runner.exe"}, "Windows", "x64"},
	{[]string{"projectboard-runner-windows-arm64.exe"}, "Windows", "ARM64"},
	{[]string{"projectboard-runner-cli-windows-amd64.exe", "projectboard-runner-cli.exe"}, "Windows CLI", "x64"},
	{[]string{"projectboard-runner-cli-windows-arm64.exe"}, "Windows CLI", "ARM64"},
	{[]string{"projectboard-runner-linux-amd64"}, "Linux", "x64"},
	{[]string{"projectboard-runner-linux-arm64"}, "Linux", "ARM64"},
	{[]string{"projectboard-runner-darwin-amd64"}, "macOS", "Intel"},
	{[]string{"projectboard-runner-darwin-arm64"}, "macOS", "Apple Silicon"},
}

func (s *Server) runnerDownloadDirectory() string {
	if s.config.RunnerDownloadDir != "" {
		return s.config.RunnerDownloadDir
	}
	executable, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(executable)
}

func (s *Server) availableRunnerDownloads() []runnerDownload {
	directory := s.runnerDownloadDirectory()
	downloads := make([]runnerDownload, 0, len(runnerDownloadCandidates))
	for _, candidate := range runnerDownloadCandidates {
		for _, name := range candidate.Names {
			path := filepath.Join(directory, name)
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			downloads = append(downloads, runnerDownload{Name: name, Platform: candidate.Platform, Arch: candidate.Arch, Size: info.Size(), URL: "/api/runner/downloads/" + name, path: path})
			break
		}
	}
	return downloads
}

func (s *Server) listRunnerDownloads(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.human(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"downloads": s.availableRunnerDownloads()})
}

func (s *Server) downloadRunner(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.human(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	for _, download := range s.availableRunnerDownloads() {
		if download.Name != name {
			continue
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", download.Name))
		http.ServeFile(w, r, download.path)
		return
	}
	writeError(w, &domainError{http.StatusNotFound, "RUNNER_DOWNLOAD_NOT_FOUND", "Runner download not found", nil})
}
