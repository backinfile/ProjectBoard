// Package runnercli contains the platform-independent ProjectBoard Runner.
// Platform adapters, such as the Windows tray executable, drive this module
// through App rather than duplicating Runner lifecycle or persistence logic.
package runnercli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const usage = "usage: projectboard-runner <connect|register|poll|claim|sync|pause|resume|agent|run-agent>"

type PollStatus string

const (
	PollStarting PollStatus = "starting"
	PollReady    PollStatus = "ready"
	PollPaused   PollStatus = "paused"
	PollError    PollStatus = "error"
	PollStopped  PollStatus = "stopped"
)

type PollEvent struct {
	Status PollStatus
	Err    error
}

type Options struct {
	ConfigDir string
	Out       io.Writer
	Err       io.Writer
	Client    *http.Client
	OnPoll    func(PollEvent)
	LookPath  func(string) (string, error)
}

type Info struct {
	ConfigDir  string
	LogPath    string
	ServerURL  string
	Paired     bool
	Paused     bool
	LocalAgent string
}

type App struct {
	configDir string
	out       io.Writer
	err       io.Writer
	client    *http.Client
	onPoll    func(PollEvent)
	lookPath  func(string) (string, error)
}

type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

type state struct {
	Server     string            `json:"server"`
	Token      string            `json:"token"`
	DeviceID   string            `json:"deviceId"`
	Mappings   map[string]string `json:"mappings"`
	LocalAgent string            `json:"localAgent,omitempty"`
}

func New(options Options) *App {
	out := options.Out
	if out == nil {
		out = io.Discard
	}
	errOut := options.Err
	if errOut == nil {
		errOut = io.Discard
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	lookPath := options.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	return &App{configDir: options.ConfigDir, out: out, err: errOut, client: client, onPoll: options.OnPoll, lookPath: lookPath}
}

func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return &UsageError{Message: usage}
	}
	switch args[0] {
	case "connect":
		return a.connect(ctx, args[1:])
	case "register":
		return a.register(args[1:])
	case "poll":
		return a.poll(ctx, args[1:])
	case "claim":
		return a.claim(ctx, args[1:])
	case "sync":
		return a.syncCommits(ctx, args[1:])
	case "pause", "resume":
		return a.setPaused(args[0] == "pause", args[1:])
	case "agent":
		return a.localAgent(args[1:])
	case "run-agent":
		if len(args) != 3 {
			return &UsageError{Message: "usage: projectboard-runner run-agent <repository-path> <prompt>"}
		}
		return a.ExecuteLocalAgent(ctx, args[1], args[2])
	default:
		return &UsageError{Message: usage}
	}
}

func (a *App) Info() (Info, error) {
	dir, err := a.dataDir()
	if err != nil {
		return Info{}, err
	}
	result := Info{ConfigDir: dir, LogPath: filepath.Join(dir, "runner.log")}
	result.LocalAgent = defaultLocalAgent
	_, pauseErr := os.Stat(filepath.Join(dir, "runner-state.json.paused"))
	result.Paused = pauseErr == nil
	s, err := a.load()
	if err == nil {
		result.Paired = s.Server != "" && s.Token != ""
		result.ServerURL = s.Server
		if s.LocalAgent != "" {
			result.LocalAgent = s.LocalAgent
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Info{}, err
	}
	return result, nil
}

func (a *App) connect(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &UsageError{Message: "usage: projectboard-runner connect <pairing-code>"}
	}
	return a.Connect(ctx, getenv("PROJECTBOARD_URL", "http://127.0.0.1:3333"), args[0])
}

// Connect exchanges a one-time Agent Key for a device-scoped Agent token and
// persists the resulting Runner session. Platform shells call this directly so
// users do not need to open a terminal for first-time setup.
func (a *App) Connect(ctx context.Context, server, agentKey string) error {
	server, err := normalizeServerURL(server)
	if err != nil {
		return err
	}
	agentKey = strings.TrimSpace(agentKey)
	if agentKey == "" {
		return errors.New("Agent Key is required")
	}
	host, _ := os.Hostname()
	digest := sha256.Sum256([]byte(host + runtime.GOOS + runtime.GOARCH))
	var response struct {
		AgentToken string `json:"agentToken"`
		DeviceID   string `json:"deviceId"`
	}
	err = a.call(ctx, server, "", "/api/agent/pair", map[string]any{
		"code": agentKey, "deviceName": host, "os": runtime.GOOS + "/" + runtime.GOARCH,
		"version": "go-rewrite", "publicKeyDigest": hex.EncodeToString(digest[:]),
	}, &response)
	if err != nil {
		return err
	}
	if response.AgentToken == "" || response.DeviceID == "" {
		return errors.New("ProjectBoard returned an invalid Runner session")
	}
	mappings := map[string]string{}
	if previous, loadErr := a.load(); loadErr == nil && previous.Server == server && previous.Mappings != nil {
		mappings = previous.Mappings
	}
	localAgent := defaultLocalAgent
	if previous, loadErr := a.load(); loadErr == nil && previous.LocalAgent != "" {
		localAgent = previous.LocalAgent
	}
	if err := a.save(state{Server: server, Token: response.AgentToken, DeviceID: response.DeviceID, Mappings: mappings, LocalAgent: localAgent}); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "runner logged in")
	return nil
}

func normalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("ProjectBoard address is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("ProjectBoard address must be a valid http or https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("ProjectBoard address must not contain credentials, query parameters, or a fragment")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (a *App) register(args []string) error {
	if len(args) != 2 {
		return &UsageError{Message: "usage: projectboard-runner register <project-id> <repository-path>"}
	}
	s, err := a.loadPaired()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(args[1])
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(filepath.Join(abs, ".git")); statErr != nil || !info.IsDir() {
		return errors.New("repository path must contain a .git directory")
	}
	if s.Mappings == nil {
		s.Mappings = map[string]string{}
	}
	s.Mappings[args[0]] = abs
	if err := a.save(s); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "registered %s\n", abs)
	return nil
}

func (a *App) poll(ctx context.Context, args []string) error {
	watch := len(args) == 1 && args[0] == "--watch"
	if len(args) > 0 && !watch {
		return &UsageError{Message: "usage: projectboard-runner poll [--watch]"}
	}
	s, err := a.loadPaired()
	if err != nil {
		return err
	}
	a.notify(PollEvent{Status: PollStarting})
	defer a.notify(PollEvent{Status: PollStopped})
	for {
		pausePath, pathErr := a.pausePath()
		if pathErr != nil {
			return pathErr
		}
		if _, paused := os.Stat(pausePath); paused == nil {
			a.notify(PollEvent{Status: PollPaused})
			if !watch {
				fmt.Fprintln(a.out, `{"paused":true}`)
				return nil
			}
		} else {
			var response any
			err = a.call(ctx, s.Server, s.Token, "/api/agent/poll", map[string]any{}, &response)
			if err == nil {
				encoded, _ := json.MarshalIndent(response, "", "  ")
				fmt.Fprintln(a.out, string(encoded))
				a.notify(PollEvent{Status: PollReady})
			} else {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				a.notify(PollEvent{Status: PollError, Err: err})
				if !watch {
					return err
				}
				fmt.Fprintf(a.err, "poll failed: %v\n", err)
			}
			if !watch {
				return nil
			}
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (a *App) claim(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return &UsageError{Message: "usage: projectboard-runner claim <work-item-id> <version>"}
	}
	s, err := a.loadPaired()
	if err != nil {
		return err
	}
	version, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}
	var response any
	if err := a.call(ctx, s.Server, s.Token, "/api/agent/assignments/"+args[0]+"/accept", map[string]any{"expectedVersion": version}, &response); err != nil {
		return err
	}
	return writeJSON(a.out, response)
}

func (a *App) syncCommits(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &UsageError{Message: "usage: projectboard-runner sync <project-id>"}
	}
	s, err := a.loadPaired()
	if err != nil {
		return err
	}
	var response any
	if err := a.call(ctx, s.Server, s.Token, "/api/agent/projects/"+args[0]+"/sync-commits", map[string]any{}, &response); err != nil {
		return err
	}
	return writeJSON(a.out, response)
}

func (a *App) setPaused(paused bool, args []string) error {
	if len(args) != 0 {
		return &UsageError{Message: "usage: projectboard-runner <pause|resume>"}
	}
	if _, err := a.loadPaired(); err != nil {
		return err
	}
	pausePath, err := a.pausePath()
	if err != nil {
		return err
	}
	if paused {
		if err := os.WriteFile(pausePath, []byte("paused\n"), 0o600); err != nil {
			return err
		}
	} else if err := os.Remove(pausePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Fprintf(a.out, "runner %s\n", map[bool]string{true: "paused", false: "resumed"}[paused])
	return nil
}

func (a *App) call(ctx context.Context, server, token, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(server, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if readErr != nil {
		return readErr
	}
	if res.StatusCode >= 300 {
		var envelope struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error.Message != "" {
			return errors.New(envelope.Error.Message)
		}
		return fmt.Errorf("server returned %s: %s", res.Status, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, output); err != nil {
		return err
	}
	return nil
}

func (a *App) dataDir() (string, error) {
	dir := a.configDir
	if dir == "" {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(dir, "ProjectBoard")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func (a *App) configPath() (string, error) {
	dir, err := a.dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "runner-state.json"), nil
}

func (a *App) pausePath() (string, error) {
	path, err := a.configPath()
	return path + ".paused", err
}

func (a *App) load() (state, error) {
	path, err := a.configPath()
	if err != nil {
		return state{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state{}, err
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return state{}, errors.New("runner state is invalid")
	}
	return s, nil
}

func (a *App) loadPaired() (state, error) {
	s, err := a.load()
	if errors.Is(err, os.ErrNotExist) {
		return state{}, errors.New("runner is not paired; run projectboard-runner connect first")
	}
	if err != nil {
		return state{}, err
	}
	if s.Server == "" || s.Token == "" {
		return state{}, errors.New("runner state is invalid")
	}
	return s, nil
}

func (a *App) save(s state) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path, err := a.configPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (a *App) notify(event PollEvent) {
	if a.onPoll != nil {
		a.onPoll(event)
	}
}

func writeJSON(w io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(encoded))
	return err
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
