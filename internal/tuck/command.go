package tuck

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

func Run(args []string, input io.Reader, output, diagnostics io.Writer) error {
	configPath := "tuck.config.json"
	filtered := []string{}
	for index := 0; index < len(args); index++ {
		if args[index] == "--config" {
			if index+1 >= len(args) {
				return fmt.Errorf("--config requires a file path")
			}
			index++
			configPath = args[index]
		} else {
			filtered = append(filtered, args[index])
		}
	}
	args = filtered
	command := "serve"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "init":
		container := len(args) == 1 && args[0] == "--container"
		if len(args) > 0 && !container {
			return fmt.Errorf("usage: tuck init [--container] [--config path]")
		}
		_, exists := os.Stat(configPath)
		if container && os.IsNotExist(exists) {
			password, e := readPassword(input, output)
			if e != nil {
				return e
			}
			hash, e := PasswordHash(password, 12)
			if e != nil {
				return e
			}
			v := freshConfig()
			O(v["security"])["auth"] = M{"enabled": true, "username": "admin", "passwordHash": hash}
			v["server"] = M{"listenHost": "0.0.0.0", "port": 8090, "frontendDir": "/opt/tuck/frontend/dist"}
			v["storage"] = M{"dataDir": "/data", "workspaceRoot": "/data/workspaces"}
			if e = writeInitialConfig(configPath, v); e != nil {
				return e
			}
		} else if e := InitConfig(configPath); e != nil {
			return e
		}
		fmt.Fprintln(output, "Measix Tuck configuration ready")
		return nil
	case "password":
		return PasswordCommand(configPath, args, input, output)
	case "migrate":
		return MigrateCommand(configPath, args, output)
	case "config-template":
		_, e := output.Write(configTemplate())
		return e
	}
	config, e := LoadConfig(configPath)
	if e != nil {
		return fmt.Errorf("cannot load Tuck configuration; check the selected file/version or run tuck migrate --check: %w", e)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch command {
	case "engine":
		if len(args) != 1 || args[0] != "install" {
			return fmt.Errorf("usage: tuck engine install [--config path]")
		}
		if e = InstallEngine(ctx, config); e != nil {
			return e
		}
		fmt.Fprintln(output, "Official Docker Agent installed and verified")
		return nil
	case "health":
		server := O(config.Applied()["server"])
		requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		host := S(server["listenHost"])
		if host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		url := "http://" + net.JoinHostPort(host, strconv.Itoa(I(server["port"]))) + "/healthz"
		req, _ := http.NewRequestWithContext(requestCtx, "GET", url, nil)
		response, e := http.DefaultClient.Do(req)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return fmt.Errorf("Tuck is not ready (HTTP %d)", response.StatusCode)
		}
		fmt.Fprintln(output, "Measix Tuck ready")
		return nil
	case "serve":
		if len(args) > 0 {
			return fmt.Errorf("usage: tuck serve [--config path]")
		}
		engine := O(config.Applied()["engine"])
		if executable, err := os.Executable(); err == nil && S(engine["mode"]) == "managed" && S(engine["binary"]) == "" {
			bundle := filepath.Join(filepath.Dir(executable), "engine", S(engine["version"]), ExecutableName())
			if _, err = os.Stat(bundle); err == nil {
				if e = InstallEngine(ctx, config); e != nil {
					return e
				}
			}
		}
		app, e := NewApp(config)
		if e != nil {
			return e
		}
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = app.Close(closeCtx)
		}()
		if e = app.Start(ctx); e != nil {
			fmt.Fprintln(diagnostics, "Docker Agent is unavailable:", e)
		}
		fmt.Fprintln(output, "Measix Tuck listening at", app.URL)
		<-ctx.Done()
		return nil
	}
	return fmt.Errorf("usage: tuck [init|serve|migrate --check|migrate --backup directory|engine install|password login|password settings|health] [--config path]")
}
func PasswordCommand(path string, args []string, input io.Reader, output io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(output, "usage: tuck password <login|settings> [--reset] [--config path]; an empty password clears the selected credential")
		return nil
	}
	if len(args) < 1 || len(args) > 2 || !Contains([]string{"login", "settings"}, args[0]) {
		return fmt.Errorf("usage: tuck password <login|settings> [--reset] [--config path]")
	}
	target := args[0]
	reset := len(args) == 2 && args[1] == "--reset"
	if len(args) == 2 && !reset {
		return fmt.Errorf("passwords must be read from standard input, never command arguments")
	}
	if reset && target != "settings" {
		return fmt.Errorf("--reset only applies to settings protection")
	}
	lock, e := acquireInstanceLock(path + ".lock")
	if e != nil {
		return e
	}
	defer lock.Close()
	raw, e := os.ReadFile(path)
	if e != nil {
		return fmt.Errorf("cannot read configuration")
	}
	v, e := DecodeJSON(raw, 4<<20)
	if e != nil {
		return fmt.Errorf("invalid configuration JSON")
	}
	c := O(v)
	if !Integer(c["schemaVersion"], configVersion, configVersion) {
		return Problem(409, "config_version", "密码维护前请先运行 tuck migrate --check 并完成配置迁移")
	}
	security := O(c["security"])
	if len(security) == 0 {
		security = M{}
		c["security"] = security
	}
	if reset {
		protection := O(security["settingsProtection"])
		if !Has(protection, "passwordHash") {
			fmt.Fprintln(output, "Settings password already cleared")
			return nil
		}
		delete(protection, "passwordHash")
		security["settingsProtection"] = protection
	} else {
		min := 12
		if target == "settings" {
			min = 6
		}
		password, e := readPassword(input, output)
		if e != nil {
			return e
		}
		hash := ""
		if password != "" {
			hash, e = PasswordHash(password, min)
			if e != nil {
				return e
			}
		}
		if target == "login" {
			auth := O(security["auth"])
			if S(auth["username"]) == "" {
				auth["username"] = "admin"
			}
			auth["passwordHash"] = hash
			auth["enabled"] = hash != ""
			security["auth"] = auth
		} else {
			protection := O(security["settingsProtection"])
			if hash == "" {
				delete(protection, "passwordHash")
			} else {
				protection["passwordHash"] = hash
			}
			security["settingsProtection"] = protection
		}
	}
	if e = ValidateConfig(c, false); e != nil {
		return e
	}
	before, e := os.ReadFile(path)
	if e != nil || Hash(before) != Hash(raw) {
		return Problem(409, "settings_changed", "配置已改变，请重新读取后保存")
	}
	formatted, e := marshalConfig(c)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	if e = AtomicWrite(path, formatted, 0600); e != nil {
		return e
	}
	fmt.Fprintln(output, "Password configuration saved; restart Tuck to apply")
	return nil
}

func readPassword(input io.Reader, output io.Writer) (string, error) {
	password := ""
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprint(output, "New password: ")
		first, e := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(output)
		if e != nil {
			return "", e
		}
		fmt.Fprint(output, "Repeat password: ")
		second, e := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(output)
		if e != nil {
			return "", e
		}
		if !Equal(string(first), string(second)) {
			return "", fmt.Errorf("password confirmation differs")
		}
		password = string(first)
	} else {
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 1025), 2048)
		if !scanner.Scan() {
			return "", fmt.Errorf("read a password line from standard input")
		}
		password = strings.TrimSuffix(scanner.Text(), "\r")
	}
	return password, nil
}
