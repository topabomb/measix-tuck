package tuck

// This bridge exists only in the Go test executable. Production has no fixture API.
import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFixtureDefaultConfig(t *testing.T) {
	if os.Getenv("TUCK_FIXTURE_DEFAULT_CONFIG") != "1" {
		return
	}
	_, _ = os.Stdout.Write(JSON(defaultConfig()))
	os.Exit(0)
}

func TestFixtureProcess(t *testing.T) {
	path := os.Getenv("TUCK_FIXTURE_CONFIG")
	if path == "" {
		return
	}
	config, e := LoadConfig(path)
	if e != nil {
		t.Fatal(e)
	}
	app, e := NewApp(config)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		app.Close(ctx)
	}()
	token := os.Getenv("TUCK_FIXTURE_TOKEN")
	original := app.Server.Handler
	var connectionsMu sync.Mutex
	connections := map[net.Conn]bool{}
	app.Server.ConnState = func(c net.Conn, state http.ConnState) {
		connectionsMu.Lock()
		defer connectionsMu.Unlock()
		if state == http.StateClosed || state == http.StateHijacked {
			delete(connections, c)
		} else {
			connections[c] = true
		}
	}
	app.Server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__fixture" && r.URL.Path != "/__fixture/events" {
			original.ServeHTTP(w, r)
			return
		}
		if !Equal(r.Header.Get("Authorization"), "Bearer "+token) {
			WriteError(w, Problem(403, "fixture_auth", "fixture authentication required"))
			return
		}
		if r.URL.Path == "/__fixture/events" {
			_ = app.events(w, r, nil)
			return
		}
		data, e := ReadJSON(r.Body, 128<<20)
		if e != nil {
			WriteError(w, e)
			return
		}
		if S(data["op"]) == "closeConnections" {
			WriteJSON(w, 200, nil)
			connectionsMu.Lock()
			for c := range connections {
				if c.RemoteAddr().String() != r.RemoteAddr {
					c.Close()
				}
			}
			connectionsMu.Unlock()
			return
		}
		value, e := fixtureOperation(app, r.Context(), data)
		if e != nil {
			WriteError(w, e)
			return
		}
		WriteJSON(w, 200, value)
	})
	if e = app.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	fmt.Fprintln(os.Stdout, "TUCK_FIXTURE_READY "+app.URL)
	_, _ = io.Copy(io.Discard, os.Stdin)
}
func fixtureOperation(app *App, ctx context.Context, data M) (any, error) {
	id := S(data["id"])
	manager := app.Manager
	switch S(data["op"]) {
	case "baseTask":
		v := O(data["input"])
		return BaseTask(S(v["id"]), S(v["nativeSessionId"]), S(v["title"]), S(v["workspace"]), firstString(v["agent"], "root"), "agent"), nil
	case "interactions":
		manager.mu.Lock()
		defer manager.mu.Unlock()
		items := []any{}
		for _, v := range manager.interactions[id] {
			items = append(items, M{"pending": Clone(v.pending)})
		}
		return items, nil
	case "steer":
		return manager.Steer(ctx, id, S(data["prompt"]), Strings(data["attachmentIds"]), OOrNil(data["source"]))
	case "create":
		return manager.Create(ctx, O(data["input"]))
	case "get":
		return manager.Get(id)
	case "selectModel":
		return manager.SelectModel(id, S(data["modelId"]), S(data["effort"]))
	case "removeModel":
		app.Models.mu.Lock()
		defer app.Models.mu.Unlock()
		filtered := []M{}
		for _, m := range app.Models.catalog {
			if m["modelId"] != data["modelId"] {
				filtered = append(filtered, m)
			}
		}
		app.Models.catalog = filtered
		return nil, nil
	case "mcpCacheCount":
		var count int
		e := app.Store.DB.QueryRow("SELECT count(*) FROM mcp_catalog WHERE resource_key=?", S(data["key"])).Scan(&count)
		return count, e
	case "seedHistory":
		manager.mu.Lock()
		defer manager.mu.Unlock()
		for i := 0; i < I(data["count"]); i++ {
			id := fmt.Sprintf("list-%d", i)
			task := BaseTask(id, fmt.Sprintf("list-native-%d", i), fmt.Sprintf("历史会话 %04d", i), S(data["workspace"]), "root", "agent")
			task["createdAt"] = time.Date(2025, 1, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339Nano)
			manager.tasks[id] = task
			if _, e := manager.appendLocked(task, M{"role": "assistant", "turnId": "history", "blocks": []any{M{"id": "old", "type": "markdown", "data": M{"markdown": "历史内容"}}}}); e != nil {
				return nil, e
			}
		}
		return nil, nil
	case "tasks":
		return manager.List(), nil
	case "running":
		return M{"running": manager.Running(id)}, nil
	case "defaults":
		return app.Models.Config(), nil
	case "setDefaults":
		return nil, app.Models.SetConfig(O(data["config"]))
	case "setBinding":
		value := app.Platform.Data()
		value["binding"] = data["binding"]
		if e := app.Platform.persist(value); e != nil {
			return nil, e
		}
		if e := app.Models.Rebuild(); e != nil {
			return nil, e
		}
		return nil, nil
	case "binding":
		return app.Platform.Binding(), nil
	case "platformEnroll":
		if e := app.Platform.Enroll(ctx, S(data["material"])); e != nil {
			return nil, e
		}
		if e := app.Models.Rebuild(); e != nil {
			return nil, e
		}
		if e := app.Engine.Close(); e != nil {
			return nil, e
		}
		if e := app.configureEngine(); e != nil {
			return nil, e
		}
		return nil, app.Engine.Start(ctx)
	case "platformSync":
		return nil, app.Platform.Sync(ctx)
	case "platformLogout":
		return nil, app.Platform.Logout(ctx)
	case "rebuild":
		return nil, app.Models.Rebuild()
	case "connection":
		return app.Models.Summary(), nil
	case "native":
		return app.Engine.Request(ctx, firstString(data["method"], "GET"), S(data["path"]), data["body"])
	case "submit":
		return manager.Submit(id, S(data["prompt"]), Strings(data["attachmentIds"]), data["expectedTurnCount"], OOrNil(data["source"]))
	case "forkOptions":
		return manager.ForkOptions(ctx, id)
	case "fork":
		return manager.Fork(ctx, id, I(data["index"]))
	case "event":
		return nil, manager.HandleEvent(id, O(data["event"]))
	case "message":
		return app.Store.Message(id, I(data["index"]))
	case "messages":
		return app.Store.Messages(id, I(data["start"]), max(1, I(data["count"])))
	case "orchestration":
		return app.orchestration(), nil
	case "delivery":
		task, e := manager.Get(id)
		if e != nil {
			return nil, e
		}
		task["turnId"] = "fixture-delivery-" + UUID()
		_, e = manager.Update(id, func(t M) error { t["turnId"] = task["turnId"]; return nil })
		if e != nil {
			return nil, e
		}
		_, finish, e := app.Local.Begin(ctx, task)
		if e != nil {
			return nil, e
		}
		defer finish()
		app.Local.mu.RLock()
		var turn *toolTurn
		for _, candidate := range app.Local.turns {
			if candidate.TaskID == id && candidate.TurnID == S(task["turnId"]) {
				turn = candidate
				break
			}
		}
		app.Local.mu.RUnlock()
		if turn == nil {
			return nil, fmt.Errorf("missing fixture turn")
		}
		return app.Delivery.Submit(ctx, turn, O(data["input"])), nil
	case "batch":
		for _, operation := range A(data["operations"]) {
			if _, e := fixtureOperation(app, ctx, O(operation)); e != nil {
				return nil, e
			}
		}
		return nil, nil
	case "saveTask":
		task := O(data["task"])
		manager.mu.Lock()
		defer manager.mu.Unlock()
		manager.tasks[S(task["id"])] = Clone(task)
		return nil, app.Store.SaveTask(task)
	case "saveMessage":
		return nil, app.Store.SaveMessage(id, O(data["message"]))
	case "append":
		manager.mu.Lock()
		defer manager.mu.Unlock()
		task, e := manager.getLocked(id)
		if e != nil {
			return nil, e
		}
		message, e := manager.appendLocked(task, O(data["message"]))
		return M{"task": Clone(task), "message": message}, e
	case "appendText":
		manager.mu.Lock()
		defer manager.mu.Unlock()
		task, e := manager.getLocked(id)
		if e != nil {
			return nil, e
		}
		message, e := app.Store.Message(id, I(data["index"]))
		if e != nil {
			return nil, e
		}
		e = manager.appendTextLocked(task, message, S(data["block"]), S(data["field"]), S(data["delta"]))
		return M{"task": Clone(task), "message": message}, e
	case "replace":
		manager.mu.Lock()
		defer manager.mu.Unlock()
		task, e := manager.getLocked(id)
		if e != nil {
			return nil, e
		}
		return nil, manager.replaceLocked(task, O(data["message"]))
	case "publish":
		manager.mu.Lock()
		defer manager.mu.Unlock()
		task, e := manager.getLocked(id)
		if e != nil {
			return nil, e
		}
		return nil, manager.publishLocked(task, O(data["event"]), nil)
	case "settle":
		manager.mu.Lock()
		defer manager.mu.Unlock()
		task, e := manager.getLocked(id)
		if e != nil {
			return nil, e
		}
		return nil, manager.settleLocked(task, S(data["reason"]), OOrNil(data["failure"]))
	case "child":
		manager.mu.Lock()
		defer manager.mu.Unlock()
		parent, e := manager.getLocked(id)
		if e != nil {
			return nil, e
		}
		return manager.createChildLocked(parent, S(data["nativeId"]), S(data["agent"]), S(data["mode"]))
	}
	return nil, fmt.Errorf("unknown fixture operation %s", data["op"])
}
func TestFixtureRequest(t *testing.T) {
	url := os.Getenv("TUCK_FIXTURE_REQUEST_URL")
	if url == "" {
		return
	}
	data, e := io.ReadAll(io.LimitReader(os.Stdin, 128<<20))
	if e != nil {
		os.Exit(1)
	}
	request, e := http.NewRequest("POST", url+"/__fixture", strings.NewReader(string(data)))
	if e != nil {
		os.Exit(1)
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv("TUCK_FIXTURE_TOKEN"))
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 60 * time.Second}
	response, e := client.Do(request)
	if e != nil {
		fmt.Fprint(os.Stderr, e)
		os.Exit(1)
	}
	defer response.Body.Close()
	result, e := io.ReadAll(io.LimitReader(response.Body, 128<<20))
	if e != nil {
		os.Exit(1)
	}
	fmt.Fprint(os.Stdout, string(result))
	if response.StatusCode >= 300 {
		os.Exit(1)
	}
	os.Exit(0)
}
