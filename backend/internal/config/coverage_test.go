package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A variable the service reads and nobody documents is a setting that exists
// only for whoever wrote it. Worse, one that is documented in .env.example but
// not passed through a compose file does nothing at all when it is set, which is
// the harder failure to notice: the operator changes it, restarts, and the
// service carries on as before.
//
// This walks the configuration's own struct tags and holds every file that
// describes it to that list, so the four of them cannot drift apart again.

// notExposed are the variables no deployment file mentions, and why.
var notExposed = map[string]string{
	// Paths inside the container, fixed by the volumes mounted over them.
	"TRIPVAULT_MEDIA_PATH":       "the media volume is mounted here",
	"TRIPVAULT_BACKUP_PATH":      "the backups volume is mounted here",
	"TRIPVAULT_SECRETS_KEY_FILE": "the config volume is mounted here",
	"TRIPVAULT_HTTP_ADDR":        "the image publishes this port",
	// Timeouts that have never needed changing; the defaults are in the code.
	"TRIPVAULT_HTTP_READ_TIMEOUT":     "tuning, left at its default",
	"TRIPVAULT_HTTP_WRITE_TIMEOUT":    "tuning, left at its default",
	"TRIPVAULT_HTTP_IDLE_TIMEOUT":     "tuning, left at its default",
	"TRIPVAULT_HTTP_SHUTDOWN_TIMEOUT": "tuning, left at its default",
	"TRIPVAULT_DB_MIN_CONNS":          "tuning, left at its default",
	"TRIPVAULT_DB_CONNECT_TIMEOUT":    "tuning, left at its default",
}

// The files each variable has to appear in, unless it is not exposed at all.
// The stand builds from source and needs no published version, and the two
// secrets a real deployment must set have no place in a committed example.
var (
	// productionOnly are set by the operator of a real instance alone.
	productionOnly = map[string]bool{
		"TRIPVAULT_DB_DSN":          true, // compose builds it from the parts
		"TRIPVAULT_AUTH_JWT_SECRET": true, // compose demands it; the stand has a throwaway
		"TRIPVAULT_ENV":             true, // compose sets it outright, per deployment
		"TRIPVAULT_SECRETS_KEY":     true, // the stand makes one inside the container
	}
	// standOnly are meaningless in a real deployment.
	standOnly = map[string]bool{
		"TRIPVAULT_DEMO_DATA": true, // refused outside development
	}
)

// TestEveryVariableIsDocumentedAndPassedThrough checks the configuration against
// the files that describe it.
func TestEveryVariableIsDocumentedAndPassedThrough(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	files := map[string]string{
		".env.example":              read(t, filepath.Join(root, ".env.example")),
		"docker-compose.yaml":       read(t, filepath.Join(root, "docker-compose.yaml")),
		"tests/docker-compose.yaml": read(t, filepath.Join(root, "tests", "docker-compose.yaml")),
	}

	for _, name := range variables() {
		if reason := notExposed[name]; reason != "" {
			// A variable listed as not exposed must really not be: a file that
			// starts mentioning it should take it off this list.
			for file, text := range files {
				if mentions(text, name) {
					t.Errorf("%s is listed as not exposed (%s) but %s mentions it", name, reason, file)
				}
			}
			continue
		}

		for file, text := range files {
			if file == ".env.example" && (productionOnly[name] || standOnly[name]) {
				continue
			}
			if file == "tests/docker-compose.yaml" && productionOnly[name] {
				continue
			}
			if file != "tests/docker-compose.yaml" && standOnly[name] {
				continue
			}
			if !mentions(text, name) {
				t.Errorf("%s is missing from %s", name, file)
			}
		}
	}
}

// TestTheStandCanBeConfiguredFromItsOwnFile checks every variable the stand's
// compose file reads from the environment is named in tests/.env, so the stand
// is configured in one place rather than by editing the compose file.
func TestTheStandCanBeConfiguredFromItsOwnFile(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	compose := read(t, filepath.Join(root, "tests", "docker-compose.yaml"))
	env := read(t, filepath.Join(root, "tests", ".env"))

	// ${NAME:-default} is how the compose file reads one.
	pattern := regexp.MustCompile(`\$\{(TRIPVAULT_[A-Z_]+)`)
	seen := map[string]bool{}
	for _, match := range pattern.FindAllStringSubmatch(compose, -1) {
		name := match[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !mentions(env, name) {
			t.Errorf("%s can be set for the stand but tests/.env does not mention it", name)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no variables were found in the stand's compose file")
	}
}

// variables lists every environment variable the configuration reads, in the
// order the structs declare them.
func variables() []string {
	var names []string
	walk(reflect.TypeOf(Config{}), envPrefix, &names)
	sort.Strings(names)
	return names
}

// walk collects the variables of one struct, following the nested ones under
// the prefix each is read with.
func walk(structType reflect.Type, prefix string, names *[]string) {
	for i := range structType.NumField() {
		field := structType.Field(i)
		if name, ok := field.Tag.Lookup("env"); ok {
			if name != "-" {
				*names = append(*names, prefix+name)
			}
			continue
		}
		if nested, ok := field.Tag.Lookup("envPrefix"); ok && field.Type.Kind() == reflect.Struct {
			walk(field.Type, prefix+nested, names)
		}
	}
}

// mentions reports whether a file names a variable, commented out or not.
func mentions(text, name string) bool {
	return regexp.MustCompile(`(?m)^\s*#?\s*`+regexp.QuoteMeta(name)+`\b`).MatchString(text) ||
		strings.Contains(text, "${"+name)
}

// read loads one of the repository's files, failing the test when it is gone.
func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}
