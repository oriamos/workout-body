package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // embedded zone database: distroless images ship none
)

// DefaultTimezone decides which calendar date a workout belongs to.
const DefaultTimezone = "Asia/Jerusalem"

type Config struct {
	Addr, DBPath, Token string
	AuthDisabled        bool
	// WebBasePath mounts the PWA under a secret, unguessable prefix. Empty
	// disables the web UI entirely, so it is never exposed by accident.
	WebBasePath string
	// Timezone is the IANA zone used to resolve "today" and to date workouts.
	Timezone string
	// Location is Timezone, loaded.
	Location *time.Location
	// MediaDir holds uploaded videos. Defaults to a "media" directory beside
	// the database file.
	MediaDir string
	// MaxVideoBytes is the upload size limit.
	MaxVideoBytes int64
}

const defaultMaxVideoMB = 200

func Load(addr string) (Config, error) { return LoadFor(addr, false) }

// LoadFor is Load for a specific run mode. In stdio mode the process is
// launched by the client over a pipe, so there is no network endpoint to
// protect and no bearer token is required.
func LoadFor(addr string, stdio bool) (Config, error) {
	if addr == "" {
		addr = ":8080"
	}
	webBase, err := normalizeBasePath(os.Getenv("WEB_BASE_PATH"))
	if err != nil {
		return Config{}, err
	}
	db := os.Getenv("TRAINING_DB_PATH")
	if db == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, err
		}
		db = filepath.Join(home, ".local", "share", "training-mcp", "training.db")
	}
	disabled := os.Getenv("MCP_AUTH_DISABLED") == "1"
	token := os.Getenv("MCP_BEARER_TOKEN")
	if token == "" && !disabled && !stdio {
		return Config{}, errors.New("MCP_BEARER_TOKEN is required unless MCP_AUTH_DISABLED=1")
	}
	tz := strings.TrimSpace(os.Getenv("WORKOUT_TIMEZONE"))
	if tz == "" {
		tz = DefaultTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("WORKOUT_TIMEZONE %q is not a valid IANA timezone (for example Asia/Jerusalem): %w", tz, err)
	}
	mediaDir := strings.TrimSpace(os.Getenv("MEDIA_DIR"))
	if mediaDir == "" {
		mediaDir = filepath.Join(filepath.Dir(db), "media")
	}
	maxMB := int64(defaultMaxVideoMB)
	if v := strings.TrimSpace(os.Getenv("MAX_VIDEO_MB")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 || n > 4096 {
			return Config{}, fmt.Errorf("MAX_VIDEO_MB %q must be a whole number between 1 and 4096", v)
		}
		maxMB = n
	}
	return Config{Addr: addr, DBPath: db, Token: token, AuthDisabled: disabled, WebBasePath: webBase, Timezone: tz, Location: loc,
		MediaDir: mediaDir, MaxVideoBytes: maxMB << 20}, nil
}

// normalizeBasePath accepts "", "app", "/app" or "/app/" and returns "" or
// "/app". A single "/" is rejected: the PWA must live under a secret prefix,
// not at the root where it would be reachable by guessing.
func normalizeBasePath(v string) (string, error) {
	v = strings.Trim(strings.TrimSpace(v), "/")
	if v == "" {
		return "", nil
	}
	if strings.ContainsAny(v, " ?#") {
		return "", errors.New("WEB_BASE_PATH must not contain spaces, '?' or '#'")
	}
	return "/" + v, nil
}
func AddressFlag(fs *flag.FlagSet) *string { return fs.String("addr", ":8080", "HTTP listen address") }
