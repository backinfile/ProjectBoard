package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type state struct {
	Server   string            `json:"server"`
	Token    string            `json:"token"`
	DeviceID string            `json:"deviceId"`
	Mappings map[string]string `json:"mappings"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "connect":
		connect(os.Args[2:])
	case "register":
		register(os.Args[2:])
	case "poll":
		poll(os.Args[2:])
	case "claim":
		claim(os.Args[2:])
	case "pause", "resume":
		setPaused(os.Args[1] == "pause")
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: projectboard-runner <connect|register|poll|claim|pause|resume>")
	os.Exit(2)
}

func connect(args []string) {
	if len(args) != 1 {
		log.Fatal("usage: projectboard-runner connect <pairing-code>")
	}
	server := getenv("PROJECTBOARD_URL", "http://127.0.0.1:3333")
	host, _ := os.Hostname()
	digest := sha256.Sum256([]byte(host + runtime.GOOS + runtime.GOARCH))
	var response struct {
		AgentToken string `json:"agentToken"`
		DeviceID   string `json:"deviceId"`
	}
	call(server, "", "/api/agent/pair", map[string]any{"code": args[0], "deviceName": host, "os": runtime.GOOS + "/" + runtime.GOARCH, "version": "go-rewrite", "publicKeyDigest": hex.EncodeToString(digest[:])}, &response)
	save(state{Server: server, Token: response.AgentToken, DeviceID: response.DeviceID, Mappings: map[string]string{}})
	fmt.Println("runner paired")
}

func register(args []string) {
	if len(args) != 2 {
		log.Fatal("usage: projectboard-runner register <project-id> <repository-path>")
	}
	s := load()
	abs, err := filepath.Abs(args[1])
	if err != nil {
		log.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(abs, ".git")); err != nil || !info.IsDir() {
		log.Fatal("repository path must contain a .git directory")
	}
	if s.Mappings == nil {
		s.Mappings = map[string]string{}
	}
	s.Mappings[args[0]] = abs
	save(s)
	fmt.Printf("registered %s\n", abs)
}

func poll(args []string) {
	s := load()
	watch := len(args) == 1 && args[0] == "--watch"
	for {
		var response any
		call(s.Server, s.Token, "/api/agent/poll", map[string]any{}, &response)
		encoded, _ := json.MarshalIndent(response, "", "  ")
		fmt.Println(string(encoded))
		if !watch {
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func claim(args []string) {
	if len(args) != 2 {
		log.Fatal("usage: projectboard-runner claim <work-item-id> <version>")
	}
	s := load()
	var version int64
	if _, err := fmt.Sscan(args[1], &version); err != nil {
		log.Fatal(err)
	}
	var response any
	call(s.Server, s.Token, "/api/agent/assignments/"+args[0]+"/accept", map[string]any{"expectedVersion": version}, &response)
	encoded, _ := json.MarshalIndent(response, "", "  ")
	fmt.Println(string(encoded))
}

func setPaused(paused bool) {
	s := load()
	marker := configPath() + ".paused"
	if paused {
		if err := os.WriteFile(marker, []byte("paused\n"), 0o600); err != nil {
			log.Fatal(err)
		}
	} else {
		_ = os.Remove(marker)
	}
	_ = s
	fmt.Printf("runner %s\n", map[bool]string{true: "paused", false: "resumed"}[paused])
}

func call(server, token, path string, input, output any) {
	body, _ := json.Marshal(input)
	req, err := http.NewRequest(http.MethodPost, server+path, bytes.NewReader(body))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		log.Fatalf("server returned %s: %s", res.Status, data)
	}
	if err = json.Unmarshal(data, output); err != nil {
		log.Fatal(err)
	}
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	dir = filepath.Join(dir, "ProjectBoard")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		log.Fatal(err)
	}
	return filepath.Join(dir, "runner-state.json")
}
func load() state {
	data, err := os.ReadFile(configPath())
	if err != nil {
		log.Fatal("runner is not paired; run connect first")
	}
	var s state
	if json.Unmarshal(data, &s) != nil {
		log.Fatal("runner state is invalid")
	}
	return s
}
func save(s state) {
	data, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(configPath(), data, 0o600); err != nil {
		log.Fatal(err)
	}
}
func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
