package tuck

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func connectTuckTools(t *testing.T, app *App, task M, actor string) *mcp.ClientSession {
	t.Helper()
	task = mustUpdate(t, app.Manager, S(task["id"]), M{"turnId": UUID()})
	headers, finish, e := app.Local.Begin(context.Background(), task)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(finish)
	client := &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+app.Models.Secret)
		r.Header.Set(tuckTurnHeader, headers[tuckTurnHeader])
		return http.DefaultTransport.RoundTrip(r)
	})}
	sdk := mcp.NewClient(&mcp.Implementation{Name: "tuck-test", Version: "1"}, nil)
	session, e := sdk.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: app.Models.URL + tuckToolsPath + "?agent=" + url.QueryEscape(actor), HTTPClient: client}, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func callTuckTool(t *testing.T, session *mcp.ClientSession, name string, args M, wantError bool) M {
	t.Helper()
	result, e := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if e != nil {
		t.Fatal(e)
	}
	if result.IsError != wantError {
		t.Fatalf("%s: error=%v result=%s", name, result.IsError, JSON(result))
	}
	var value M
	if e = json.Unmarshal(JSON(result.StructuredContent), &value); e != nil {
		t.Fatal(e)
	}
	return value
}
func TestModelContentToolInstructions(t *testing.T) {
	for _, tt := range []struct {
		names []string
		want  string
	}{
		{nil, ""},
		{[]string{"inspect_attachments"}, "The Tuck toolset provides the following tool: inspect_attachments."},
		{[]string{"send_session_message", "list_sessions"}, "The Tuck toolset provides the following tools: list_sessions and send_session_message."},
		{modelContent.ToolNames(), "The Tuck toolset provides the following tools: list_sessions, send_session_message, deliver_result, upload_runcove_file, download_runcove_files, generate_images, and inspect_attachments."},
	} {
		if got := modelContent.Instructions(modelContent.Tools(tt.names, nil)); got != tt.want {
			t.Fatalf("%q != %q", got, tt.want)
		}
	}
}
func TestModelContentDisabledInspectionWarning(t *testing.T) {
	for _, image := range []bool{false, true} {
		mime := "text/plain"
		if image {
			mime = "image/png"
		}
		for _, canInspect := range []bool{false, true} {
			message := modelContent.UserMessage("Read the attachment", []modelAttachment{{Path: "artifacts/uploads/attachment", MIME: mime}}, false, canInspect)
			text := S(message["content"])
			if strings.Contains(text, "image pixels") != image || strings.Contains(text, "inspect_attachments") != (image && canInspect) {
				t.Fatal(message)
			}
		}
	}
	for _, enabled := range []bool{false, true} {
		output := modelContent.ToolResult("generate_images", M{"status": "completed", "textOnlyModel": true, "canInspect": enabled})
		if strings.Contains(S(output["warning"]), "inspect_attachments") != enabled {
			t.Fatal(output)
		}
	}
	value := modelContent.Error(Problem(500, "invalid_arguments", "SECRET internal path"))
	if strings.Contains(string(JSON(value)), "SECRET") {
		t.Fatal(value)
	}
}

func TestUnifiedMCPActorPermissions(t *testing.T) {
	f := newWorkspaceFixture(t)
	local, turn := f.turn(t)
	server := httptest.NewServer(local)
	defer server.Close()
	for _, tc := range []struct {
		actor string
		names []string
	}{
		{"root", modelContent.ToolNames()},
		{"worker", []string{"upload_runcove_file", "download_runcove_files", "inspect_attachments"}},
		{"reviewer", []string{"inspect_attachments"}},
	} {
		t.Run(tc.actor, func(t *testing.T) {
			client := &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
				r = r.Clone(r.Context())
				r.Header.Set("Authorization", "Bearer "+local.Models.Secret)
				r.Header.Set(tuckTurnHeader, turn.Key)
				return http.DefaultTransport.RoundTrip(r)
			})}
			sdk := mcp.NewClient(&mcp.Implementation{Name: "actor-test", Version: "1"}, nil)
			session, err := sdk.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + tuckToolsPath + "?agent=" + tc.actor, HTTPClient: client}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			result, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, tool := range result.Tools {
				names = append(names, tool.Name)
			}
			if len(names) != len(tc.names) {
				t.Fatal(names, tc.names)
			}
			for _, name := range tc.names {
				if !Contains(names, name) {
					t.Fatal(names, tc.names)
				}
			}
			if tc.actor != "root" {
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "deliver_result", Arguments: M{"summary": "must not deliver"}})
				if err == nil && !result.IsError {
					t.Fatal("child invoked main-only tool", result)
				}
			} else {
				// A cached definition is not authorization after its switch changes.
				c := local.Models.Config()
				O(O(c["agents"])["root"])["disabledTuckTools"] = []string{"list_sessions"}
				f.p.save(c)
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_sessions", Arguments: M{}})
				if err == nil && !result.IsError {
					t.Fatal("disabled tool remained callable", result)
				}
			}
		})
	}
	if local.actorAllowed(turn.Task, "video") == nil {
		t.Fatal("unrelated actor accepted")
	}
}

func TestModelContentMCPResult(t *testing.T) {
	for _, status := range []string{"completed", "accepted", "failed", "partial", "unknown"} {
		value := M{"status": status}
		result := modelContent.MCPResult(value)
		if result.IsError != (status != "completed" && status != "accepted") || string(JSON(result.StructuredContent)) != string(JSON(value)) || len(result.Content) != 1 {
			t.Fatal(result)
		}
		text, ok := result.Content[0].(*mcp.TextContent)
		if !ok || text.Text != string(JSON(value)) {
			t.Fatal(result.Content)
		}
	}
	// The prepared model representation is disclosed; original provider bytes
	// belong to file storage and must not enter the model result by accident.
	image := GeneratedImage{Bytes: []byte("original"), MIME: "image/png", ModelBytes: []byte("model"), ModelMIME: "image/jpeg"}
	value := modelContent.ToolResult("generate_images", M{"status": "completed", "files": []any{M{"path": "artifacts/images/result.png", "promptPath": "artifacts/images/result.txt"}}})
	result := modelContent.MCPResult(value, image)
	if len(result.Content) != 2 {
		t.Fatal(result)
	}
	media, ok := result.Content[1].(*mcp.ImageContent)
	if !ok || media.MIMEType != image.ModelMIME || !bytes.Equal(media.Data, image.ModelBytes) {
		t.Fatal(result.Content)
	}
}
