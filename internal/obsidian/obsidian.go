// Package obsidian reads and extends the desktop app's vault registry and opens vaults.
//
// Obsidian only opens vaults it already knows. It keeps that list in obsidian.json,
// reads it once at launch, and rewrites it whenever its state changes. So a folder
// is registered by: quitting Obsidian if it runs, adding one entry in the app's own
// format, relaunching, then opening the obsidian:// URI. Every write is validated,
// backed up, and atomic; when the file does not look as expected nothing is written.
package obsidian

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RegistryPath is where the desktop app keeps its vault list on this platform.
func RegistryPath() (string, error) {
	if override := os.Getenv("OBSIDIAN_CONFIG_DIR"); override != "" {
		return filepath.Join(override, "obsidian.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "obsidian", "obsidian.json"), nil
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "obsidian", "obsidian.json"), nil
	default:
		flatpak := filepath.Join(home, ".var", "app", "md.obsidian.Obsidian", "config", "obsidian", "obsidian.json")
		if _, err := os.Stat(flatpak); err == nil {
			return flatpak, nil
		}
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "obsidian", "obsidian.json"), nil
	}
}

// Registry is the app's vault list. Everything is kept as raw JSON so a write changes
// only the one entry it adds.
type Registry struct {
	path   string
	raw    map[string]json.RawMessage
	vaults map[string]json.RawMessage
	paths  map[string]string // id -> path
}

var (
	ErrNoRegistry = errors.New("Obsidian's vault registry was not found; open Obsidian once so it creates it")
	ErrUnexpected = errors.New("Obsidian's vault registry does not look as expected; refusing to change it")
)

// LoadRegistry reads and validates the registry file.
func LoadRegistry() (*Registry, error) {
	path, err := RegistryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w (looked at %s)", ErrNoRegistry, path)
	}
	if err != nil {
		return nil, err
	}
	reg := &Registry{path: path, raw: map[string]json.RawMessage{}, vaults: map[string]json.RawMessage{}, paths: map[string]string{}}
	if err := json.Unmarshal(data, &reg.raw); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrUnexpected, path, err)
	}
	if rawVaults, ok := reg.raw["vaults"]; ok {
		if err := json.Unmarshal(rawVaults, &reg.vaults); err != nil {
			return nil, fmt.Errorf("%w: %s: vaults is not an object", ErrUnexpected, path)
		}
	}
	for id, entry := range reg.vaults {
		var v struct {
			Path *string `json:"path"`
			TS   *int64  `json:"ts"`
		}
		if err := json.Unmarshal(entry, &v); err != nil || v.Path == nil || *v.Path == "" || v.TS == nil {
			return nil, fmt.Errorf("%w: %s: vault %s lacks path or ts", ErrUnexpected, path, id)
		}
		reg.paths[id] = *v.Path
	}
	return reg, nil
}

// Path is the registry file's location.
func (r *Registry) Path() string { return r.path }

// Find returns the id of a vault directory, if registered.
func (r *Registry) Find(vault string) (string, bool) {
	target := filepath.Clean(vault)
	for id, p := range r.paths {
		if filepath.Clean(p) == target {
			return id, true
		}
	}
	return "", false
}

// Register adds a vault the way the app does: a random 16-hex id, path, and timestamp.
// The previous file is kept as obsidian.json.bak and the write is atomic.
func (r *Registry) Register(vault string) (string, error) {
	if id, ok := r.Find(vault); ok {
		return id, nil
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	entry, err := json.Marshal(map[string]any{"path": filepath.Clean(vault), "ts": time.Now().UnixMilli()})
	if err != nil {
		return "", err
	}
	r.vaults[id] = entry
	r.paths[id] = filepath.Clean(vault)
	vaults, err := json.Marshal(r.vaults)
	if err != nil {
		return "", err
	}
	r.raw["vaults"] = vaults
	data, err := json.Marshal(r.raw)
	if err != nil {
		return "", err
	}
	if current, err := os.ReadFile(r.path); err == nil {
		if err := os.WriteFile(r.path+".bak", current, 0o644); err != nil {
			return "", fmt.Errorf("back up registry: %w", err)
		}
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, r.path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return id, nil
}

// OpenURI is the link that opens a registered vault by path.
func OpenURI(vault string) string {
	return "obsidian://open?path=" + url.QueryEscape(vault)
}

func launch(args ...string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", args...)
	case "windows":
		cmd = exec.Command("cmd", append([]string{"/c", "start", ""}, args...)...)
	default:
		opener, err := exec.LookPath("xdg-open")
		if err != nil {
			return errors.New("no xdg-open on PATH")
		}
		cmd = exec.Command(opener, args...)
	}
	return cmd.Run()
}

// Open asks the desktop to open a registered vault.
// OpenPath opens one file of a vault Obsidian already knows.
func OpenPath(path string) error {
	return launch(OpenURI(path))
}

func Open(vault string) error {
	if err := launch(OpenURI(vault)); err != nil {
		return fmt.Errorf("could not launch Obsidian; open this link by hand: %s", OpenURI(vault))
	}
	return nil
}

// Reveal shows the folder in the file manager, for "Open folder as vault" by hand.
func Reveal(vault string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", vault).Run()
	}
	return launch(filepath.Dir(vault))
}

// Running reports whether the desktop app has a process.
func Running() bool {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("pgrep", "-x", "Obsidian").Run() == nil
	case "linux":
		return exec.Command("pgrep", "-x", "obsidian").Run() == nil
	case "windows":
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq Obsidian.exe").Output()
		return err == nil && strings.Contains(string(out), "Obsidian.exe")
	}
	return false
}

var ErrManualRestart = errors.New("quitting Obsidian is only automated on macOS; quit it by hand and run the command again")

// Quit closes the desktop app and waits for it to exit.
func Quit() error {
	if runtime.GOOS != "darwin" {
		return ErrManualRestart
	}
	if err := exec.Command("osascript", "-e", `quit app "Obsidian"`).Run(); err != nil {
		return fmt.Errorf("quit Obsidian: %w", err)
	}
	for i := 0; i < 100 && Running(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if Running() {
		return errors.New("Obsidian did not quit; close it by hand and run the command again")
	}
	return nil
}

// Launch starts the desktop app and gives it time to read its registry.
func Launch() error {
	if runtime.GOOS != "darwin" {
		return ErrManualRestart
	}
	if err := exec.Command("open", "-a", "Obsidian").Run(); err != nil {
		return fmt.Errorf("relaunch Obsidian: %w", err)
	}
	time.Sleep(4 * time.Second)
	return nil
}

// Status says whether a vault is registered and whether the app is running.
func Status(vault string) (registered, running bool, err error) {
	reg, err := LoadRegistry()
	if err != nil {
		return false, false, err
	}
	_, registered = reg.Find(vault)
	return registered, Running(), nil
}

// RegisterAndOpen makes Obsidian know a folder, restarting the app if it runs, then opens it.
// The caller has already asked the user; this does the work in the only safe order.
func RegisterAndOpen(vault string) error {
	wasRunning := Running()
	if wasRunning {
		if err := Quit(); err != nil {
			return err
		}
	}
	reg, err := LoadRegistry()
	if err != nil {
		return err
	}
	if _, err := reg.Register(vault); err != nil {
		return err
	}
	if wasRunning {
		if err := Launch(); err != nil {
			return err
		}
	}
	return Open(vault)
}
