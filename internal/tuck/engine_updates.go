package tuck

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed contract/engine-release.json
var engineManifest []byte

func ExecutableName() string {
	if runtime.GOOS == "windows" {
		return "docker-agent.exe"
	}
	return "docker-agent"
}

type EngineRelease struct {
	Version, URL, SHA256, PublishedAt, ReleaseURL string
	Size                                          int64
}

func BaselineRelease() (EngineRelease, error) {
	v, e := DecodeJSON(engineManifest, 16<<10)
	if e != nil {
		return EngineRelease{}, e
	}
	target := map[string]string{"windows": "win32", "linux": "linux", "darwin": "darwin"}[runtime.GOOS] + "-" + map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	m := O(v)
	asset := O(O(m["targets"])[target])
	if len(asset) == 0 {
		return EngineRelease{}, fmt.Errorf("unsupported official engine target %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return EngineRelease{Version: S(m["version"]), URL: S(asset["url"]), SHA256: S(asset["sha256"]), ReleaseURL: "https://github.com/docker/docker-agent/releases/tag/" + S(m["version"])}, nil
}
func FileHash(path string) (string, error) {
	file, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer file.Close()
	hash := sha256.New()
	if _, e = io.Copy(hash, file); e != nil {
		return "", e
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func LatestRelease(ctx context.Context, client *http.Client) (EngineRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/docker/docker-agent/releases/latest", nil)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Measix-Tuck-engine-update")
	r, e := client.Do(request)
	if e != nil {
		return EngineRelease{}, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return EngineRelease{}, fmt.Errorf("official release check HTTP %d", r.StatusCode)
	}
	value, e := ReadJSON(r.Body, 4<<20)
	if e != nil {
		return EngineRelease{}, e
	}
	version := S(value["tag_name"])
	name := "docker-agent-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if !stableVersion.MatchString(version) || B(value["draft"]) || B(value["prerelease"]) {
		return EngineRelease{}, fmt.Errorf("official stable release required")
	}
	for _, v := range A(value["assets"]) {
		asset := O(v)
		if S(asset["name"]) != name {
			continue
		}
		digest := S(asset["digest"])
		expected := "https://github.com/docker/docker-agent/releases/download/" + version + "/" + name
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 || S(asset["browser_download_url"]) != expected {
			return EngineRelease{}, fmt.Errorf("official artifact URL or SHA256 missing")
		}
		if _, e = hex.DecodeString(digest[7:]); e != nil {
			return EngineRelease{}, e
		}
		return EngineRelease{Version: version, URL: expected, SHA256: digest[7:], Size: int64(N(asset["size"])), PublishedAt: S(value["published_at"]), ReleaseURL: "https://github.com/docker/docker-agent/releases/tag/" + version}, nil
	}
	return EngineRelease{}, fmt.Errorf("official release has no artifact for this host")
}
func DownloadEngine(ctx context.Context, client *http.Client, release EngineRelease, destination string) error {
	u, e := url.Parse(release.URL)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || !strings.HasPrefix(u.Path, "/docker/docker-agent/releases/download/"+release.Version+"/") || !stableVersion.MatchString(release.Version) || len(release.SHA256) != 64 {
		return fmt.Errorf("invalid official release metadata")
	}
	if _, e = hex.DecodeString(release.SHA256); e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(destination), 0700); e != nil {
		return e
	}
	file, e := os.CreateTemp(filepath.Dir(destination), ".engine-*")
	if e != nil {
		return e
	}
	pending := file.Name()
	defer os.Remove(pending)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", release.URL, nil)
	response, e := client.Do(request)
	if e != nil {
		file.Close()
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		file.Close()
		return fmt.Errorf("official artifact HTTP %d", response.StatusCode)
	}
	limit := release.Size
	if limit <= 0 {
		limit = 512 << 20
	}
	hash := sha256.New()
	n, e := io.CopyBuffer(io.MultiWriter(file, hash), io.LimitReader(response.Body, limit+1), make([]byte, 128<<10))
	if e == nil {
		e = file.Sync()
	}
	file.Close()
	if e != nil {
		return e
	}
	if n > limit || release.Size > 0 && n != release.Size || hex.EncodeToString(hash.Sum(nil)) != release.SHA256 {
		return fmt.Errorf("official artifact SHA256 or size mismatch")
	}
	if e = os.Chmod(pending, 0755); e != nil {
		return e
	}
	version, e := EngineVersion(ctx, pending)
	if e != nil || version != release.Version {
		return fmt.Errorf("official program version mismatch")
	}
	return replaceFile(pending, destination)
}
func InstallEngine(ctx context.Context, config *Config) error {
	engine := O(config.Applied()["engine"])
	if S(engine["mode"]) == "external" || S(engine["binary"]) != "" {
		return Problem(409, "external_engine", "此引擎由外部管理")
	}
	release, e := BaselineRelease()
	if e != nil {
		return e
	}
	client := &http.Client{Transport: newTransport(30 * time.Second)}
	defer client.CloseIdleConnections()
	if release.Version != S(engine["version"]) {
		release, e = LatestRelease(ctx, client)
		if e != nil {
			return e
		}
		if release.Version != S(engine["version"]) {
			return fmt.Errorf("configured release is not the pinned baseline or current official release")
		}
	}
	destination := filepath.Join(config.DataDir(), "engines", release.Version, ExecutableName())
	if hash, e := FileHash(destination); e == nil && hash == release.SHA256 {
		return nil
	}
	if executable, e := os.Executable(); e == nil {
		bundle := filepath.Join(filepath.Dir(executable), "engine", release.Version, ExecutableName())
		if _, e = os.Stat(bundle); e == nil {
			hash, e := FileHash(bundle)
			if e != nil || hash != release.SHA256 {
				return fmt.Errorf("bundled official artifact SHA256 mismatch")
			}
			if e = os.MkdirAll(filepath.Dir(destination), 0700); e != nil {
				return e
			}
			pending, e := os.CreateTemp(filepath.Dir(destination), ".engine-*")
			if e != nil {
				return e
			}
			path := pending.Name()
			pending.Close()
			defer os.Remove(path)
			if e = copyFileSync(bundle, path); e != nil {
				return e
			}
			if e = os.Chmod(path, 0755); e != nil {
				return e
			}
			return replaceFile(path, destination)
		}
	}
	return DownloadEngine(ctx, client, release, destination)
}
func copyFileSync(source, target string) error {
	in, e := os.Open(source)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = io.CopyBuffer(out, in, make([]byte, 128<<10))
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}

type EngineUpdates struct {
	App                *App
	Client             *http.Client
	mu                 sync.Mutex
	busy               string
	latest, downloaded *EngineRelease
	path               string
	ctx                context.Context
	cancel             context.CancelFunc
	Inspect            func(context.Context, string) (string, error)
}

func NewEngineUpdates(app *App) *EngineUpdates {
	ctx, cancel := context.WithCancel(context.Background())
	return &EngineUpdates{App: app, Client: &http.Client{Transport: newTransport(30 * time.Second)}, ctx: ctx, cancel: cancel, Inspect: EngineVersion}
}
func (u *EngineUpdates) managed() bool {
	return !u.App.Engine.Options().External && S(O(u.App.Models.Config()["engine"])["binary"]) == ""
}
func (u *EngineUpdates) View() M {
	u.mu.Lock()
	defer u.mu.Unlock()
	state := u.App.Engine.State()
	v := M{"managed": u.managed(), "currentVersion": state["version"], "state": state["state"], "busy": nil, "latest": nil, "downloadedVersion": nil}
	if u.busy != "" {
		v["busy"] = u.busy
	}
	if u.latest != nil {
		v["latest"] = M{"version": u.latest.Version, "publishedAt": u.latest.PublishedAt, "releaseUrl": u.latest.ReleaseURL}
	}
	if u.downloaded != nil {
		v["downloadedVersion"] = u.downloaded.Version
	}
	return v
}
func (u *EngineUpdates) exclusive(kind string, fn func() error) error {
	u.mu.Lock()
	if u.busy != "" || u.ctx.Err() != nil {
		u.mu.Unlock()
		return Problem(409, "engine_busy", "引擎操作正在进行或服务正在关闭")
	}
	u.busy = kind
	u.mu.Unlock()
	defer func() { u.mu.Lock(); u.busy = ""; u.mu.Unlock() }()
	return fn()
}
func (u *EngineUpdates) Check(ctx context.Context) error {
	return u.exclusive("checking", func() error {
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(u.ctx, cancel)
		defer stop()
		defer cancel()
		release, e := LatestRelease(ctx, u.Client)
		if e != nil {
			return e
		}
		u.mu.Lock()
		u.latest = &release
		u.mu.Unlock()
		return nil
	})
}
func (u *EngineUpdates) Download(ctx context.Context, version string) error {
	if !u.managed() {
		return Problem(409, "external_engine", "此引擎由外部管理，请在宿主更新")
	}
	return u.exclusive("downloading", func() error {
		u.mu.Lock()
		release := u.latest
		previous := u.path
		u.mu.Unlock()
		if release == nil || release.Version != version {
			return Problem(409, "engine_release_required", "请先检查官方更新")
		}
		if version == S(u.App.Engine.State()["version"]) {
			return Problem(409, "engine_current", "当前已使用此版本")
		}
		directory, e := os.MkdirTemp("", "tuck-engine-download-")
		if e != nil {
			return e
		}
		path := filepath.Join(directory, ExecutableName())
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(u.ctx, cancel)
		defer stop()
		defer cancel()
		if e = DownloadEngine(ctx, u.Client, *release, path); e != nil {
			os.RemoveAll(directory)
			return e
		}
		u.mu.Lock()
		u.downloaded = release
		u.path = path
		u.mu.Unlock()
		if previous != "" {
			os.RemoveAll(filepath.Dir(previous))
		}
		return nil
	})
}
func (u *EngineUpdates) Verify(version string) error {
	u.mu.Lock()
	release, path := u.downloaded, u.path
	u.mu.Unlock()
	if !u.managed() || release == nil || release.Version != version {
		return Problem(409, "engine_download_required", "请先下载此版本")
	}
	hash, e := FileHash(path)
	if e != nil || hash != release.SHA256 {
		return Problem(409, "engine_corrupt", "已下载程序校验失败，请重新下载")
	}
	return nil
}
func (u *EngineUpdates) Apply(ctx context.Context, version string, authorize func() error) error {
	return u.exclusive("applying", func() error {
		if e := u.Verify(version); e != nil {
			return e
		}
		a := u.App
		current, revision, e := a.Config.Read()
		if e != nil {
			return e
		}
		pending := a.Config.Pending()
		next := Clone(current)
		O(next["engine"])["version"] = version
		previous := a.Engine.Options()
		previousApplied := a.Models.Config()
		ownRevision := ""
		destination := filepath.Join(a.Config.DataDir(), "engines", version, ExecutableName())
		if e = os.MkdirAll(filepath.Dir(destination), 0700); e != nil {
			return e
		}
		u.mu.Lock()
		staged := u.path
		u.mu.Unlock()
		if e = copyFileSync(staged, destination); e != nil {
			return e
		}
		if e = os.Chmod(destination, 0755); e != nil {
			return e
		}
		backup, e := os.MkdirTemp(filepath.Join(a.Config.DataDir(), "engines"), ".recovery-")
		if e != nil {
			return e
		}
		restored := false
		stopped, started := false, false
		saved := map[string]bool{}
		defer func() {
			if restored || !stopped || S(a.Engine.State()["state"]) == "ready" {
				os.RemoveAll(backup)
			}
		}()
		applyErr := func() error {
			if e = authorize(); e != nil {
				return e
			}
			if e = a.Engine.Close(); e != nil {
				return e
			}
			stopped = true
			for _, name := range []string{"docker-sessions.db", "docker-sessions.db-wal", "docker-sessions.db-shm"} {
				source := filepath.Join(previous.DataDir, name)
				if _, e = os.Stat(source); os.IsNotExist(e) {
					continue
				}
				if e != nil {
					return e
				}
				if e = copyFileSync(source, filepath.Join(backup, name)); e != nil {
					return e
				}
				saved[name] = true
			}
			options := previous
			options.Binary = destination
			if e = a.Engine.Configure(options); e != nil {
				return e
			}
			started = true
			if e = a.Engine.Start(ctx); e != nil {
				return e
			}
			if _, e = a.Engine.Request(ctx, "GET", "/api/agents/agent", nil); e != nil {
				return e
			}
			if e = authorize(); e != nil {
				return e
			}
			if ownRevision, e = a.Config.Save(revision, next); e != nil {
				return e
			}
			applied := a.Models.Config()
			O(applied["engine"])["version"] = version
			if e = a.Models.SetConfig(applied); e != nil {
				return e
			}
			if !pending {
				a.Config.MarkAppliedVersion(next, ownRevision)
			} else {
				a.Config.MarkAppliedVersion(applied, "")
			}
			return nil
		}()
		if applyErr != nil {
			if ownRevision != "" {
				if restoreErr := a.Config.Restore(ownRevision, current); restoreErr != nil {
					applyErr = fmt.Errorf("%w; saved configuration changed externally and was preserved", applyErr)
				}
			}
			_ = a.Models.SetConfig(previousApplied)
			if stopped {
				_ = a.Engine.Close()
				if started {
					for _, name := range []string{"docker-sessions.db", "docker-sessions.db-wal", "docker-sessions.db-shm"} {
						target := filepath.Join(previous.DataDir, name)
						if saved[name] {
							if e = copyFileSync(filepath.Join(backup, name), target); e != nil {
								return Problem(503, "engine_restore_failed", "更新失败，会话备份保留在 "+backup)
							}
						} else {
							if e = os.Remove(target); e != nil && !os.IsNotExist(e) {
								return e
							}
						}
					}
				}
				_ = a.Engine.Configure(previous)
				restoreCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if e = a.Engine.Start(restoreCtx); e != nil {
					return Problem(503, "engine_restore_failed", "更新失败，原引擎未能恢复。会话备份保留在 "+backup)
				}
				restored = true
			}
			return Problem(502, "engine_update_failed", "更新未应用，已保留原版本。"+applyErr.Error())
		}
		u.mu.Lock()
		path := u.path
		u.path = ""
		u.downloaded = nil
		u.mu.Unlock()
		if path != "" {
			os.RemoveAll(filepath.Dir(path))
		}
		return nil
	})
}
func (u *EngineUpdates) Close() {
	u.cancel()
	u.Client.CloseIdleConnections()
	u.mu.Lock()
	path := u.path
	u.path = ""
	u.mu.Unlock()
	if path != "" {
		os.RemoveAll(filepath.Dir(path))
	}
}
func (a *App) engineSettings(w http.ResponseWriter, r *http.Request) error {
	if e := a.Protection.Require(r, "engine"); e != nil {
		return e
	}
	if r.URL.Path == "/api/settings/engine" && r.Method == "GET" {
		WriteJSON(w, 200, a.Updates.View())
		return nil
	}
	if r.Method != "POST" {
		return Problem(405, "method_not_allowed", "Method not allowed")
	}
	data, e := ReadJSON(r.Body, 4096)
	if e != nil {
		return e
	}
	authorize := func() error { return a.Protection.Require(r, "engine") }
	if e = authorize(); e != nil {
		return e
	}
	if a.changing.Load() {
		return Problem(409, "configuration_changing", "配置或引擎正在应用，请稍后重试")
	}
	switch r.URL.Path {
	case "/api/settings/engine/check":
		e = a.Updates.Check(r.Context())
	case "/api/settings/engine/download":
		e = a.Updates.Download(r.Context(), S(data["version"]))
	case "/api/settings/engine/apply":
		if !a.changeMu.TryLock() {
			return Problem(409, "configuration_changing", "配置或引擎正在应用，请稍后重试")
		}
		defer a.changeMu.Unlock()
		if e = a.Updates.Verify(S(data["version"])); e != nil {
			return e
		}
		a.changing.Store(true)
		a.Manager.SetConfigurationChanging(true)
		defer func() { a.Workspace.Invalidate(); a.changing.Store(false); a.Manager.SetConfigurationChanging(false) }()
		if a.Manager.Active() && !B(data["interrupt"]) {
			return Problem(409, "engine_interrupt_required", "有任务正在执行或等待交互，应用更新会中断它们")
		}
		a.Workspace.Close()
		if e = authorize(); e != nil {
			return e
		}
		for _, task := range a.Manager.List() {
			if a.Manager.Running(S(task["id"])) || S(task["status"]) != "idle" {
				if e = a.Manager.Abort(r.Context(), S(task["id"])); e != nil {
					return e
				}
			}
		}
		e = a.Updates.Apply(r.Context(), S(data["version"]), authorize)
		if e == nil {
			e = a.publishConnection()
		}
	default:
		return Problem(404, "not_found", "Route not found")
	}
	if e != nil {
		return e
	}
	if e = authorize(); e != nil {
		return e
	}
	WriteJSON(w, 200, a.Updates.View())
	return nil
}
