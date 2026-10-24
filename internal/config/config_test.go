package config

import "testing"

func TestLoadDefaultsAndRequiresToken(t *testing.T) {
	t.Setenv("MCP_BEARER_TOKEN", "secret")
	t.Setenv("MCP_AUTH_DISABLED", "")
	c, err := Load("")
	if err != nil || c.Addr != ":8080" || c.Token != "secret" {
		t.Fatalf("config=%#v err=%v", c, err)
	}
	t.Setenv("MCP_BEARER_TOKEN", "")
	if _, err := Load(""); err == nil {
		t.Fatal("missing token should fail")
	}
}

func TestLoadNormalizesWebBasePath(t *testing.T) {
	for _, tt := range []struct {
		name, env, want string
		wantErr         bool
	}{
		{name: "unset disables the web UI", env: "", want: ""},
		{name: "bare slashes disable it too", env: "/", want: ""},
		{name: "adds the leading slash", env: "app", want: "/app"},
		{name: "strips the trailing slash", env: "/app/", want: "/app"},
		{name: "keeps a normalized value", env: "/a1b2c3", want: "/a1b2c3"},
		{name: "rejects a query separator", env: "/app?x", wantErr: true},
		{name: "rejects a fragment separator", env: "/app#x", wantErr: true},
		{name: "rejects spaces", env: "/my app", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MCP_BEARER_TOKEN", "secret")
			t.Setenv("WEB_BASE_PATH", tt.env)
			cfg, err := Load(":8080")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() error = nil, want an error for %q", tt.env)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.WebBasePath != tt.want {
				t.Fatalf("WebBasePath = %q, want %q", cfg.WebBasePath, tt.want)
			}
		})
	}
}

func TestMCPURLKeyValidation(t *testing.T) {
	t.Setenv("MCP_BEARER_TOKEN", "secret-token-secret-token-secret-token")
	t.Setenv("MCP_AUTH_DISABLED", "")
	for name, tc := range map[string]struct {
		key string
		ok  bool
	}{
		"unset":         {"", true},
		"valid":         {"0123456789abcdef0123456789abcdef", true},
		"too short":     {"short", false},
		"same as token": {"secret-token-secret-token-secret-token", false},
		"bad chars":     {"0123456789abcdef0123456789abcde&", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MCP_URL_KEY", tc.key)
			cfg, err := Load("")
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
			if err == nil && cfg.MCPURLKey != tc.key {
				t.Fatalf("MCPURLKey = %q", cfg.MCPURLKey)
			}
		})
	}
}
