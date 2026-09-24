package utility

import (
	"slices"
	"testing"
)

func TestResolveCodexAppServerCommandAllowList(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		want    []string
		ok      bool
	}{
		{
			name:    "managed npm runtime",
			command: []string{"npx", "--yes", "--prefer-offline", "@openai/codex@0.154.0", "app-server"},
			want:    []string{"--yes", "--prefer-offline", "@openai/codex@0.154.0", "app-server"},
			ok:      true,
		},
		{name: "native command", command: []string{"codex", "app-server"}, want: []string{"app-server"}, ok: true},
		{name: "other package", command: []string{"npx", "--yes", "--prefer-offline", "example/other@1.2.3", "app-server"}},
		{name: "unexpected arguments", command: []string{"npx", "--yes", "@openai/codex@0.154.0", "app-server", "--danger"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, args, err := resolveCodexAppServerCommand(&InferenceConfigDTO{Command: tt.command})
			if tt.ok {
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(args, tt.want) {
					t.Fatalf("args = %#v, want %#v", args, tt.want)
				}
				if (tt.name == "native command") != (command == "codex") {
					t.Fatalf("command = %q", command)
				}
				return
			}
			if err == nil {
				t.Fatal("expected command to be rejected")
			}
		})
	}
}

func TestCodexUtilityMCPConfigKeepsHTTPHeaders(t *testing.T) {
	config := codexUtilityMCPConfig([]MCPServerDTO{{
		Name: "kandev", Type: "http", URL: "http://localhost:4231/mcp",
		HeaderKVs: []HTTPHeaderDTO{{Name: "Authorization", Value: "Bearer test-token"}},
	}})
	servers, ok := config["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatal("MCP server config is missing")
	}
	server, ok := servers["kandev"].(map[string]any)
	if !ok || server["url"] != "http://localhost:4231/mcp" {
		t.Fatalf("MCP server config = %#v", servers["kandev"])
	}
	headers, ok := server["http_headers"].(map[string]string)
	if !ok || headers["Authorization"] != "Bearer test-token" {
		t.Fatalf("MCP headers = %#v", server["http_headers"])
	}
}
