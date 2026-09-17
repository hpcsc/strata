package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/hpcsc/strata/internal/keymap"
)

type Config struct {
	Theme       string
	Split       bool
	AutoRefresh bool
	Keys        keymap.Keymap
}

func Default() Config {
	return Config{Theme: "nord", Split: true, Keys: keymap.Default()}
}

func Path(getenv func(string) string) string {
	if dir := getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "strata", "config.toml")
	}
	if home := getenv("HOME"); home != "" {
		return filepath.Join(home, ".config", "strata", "config.toml")
	}
	return ""
}

func Load(path string) (Config, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	c, err := Parse(string(text))
	if err != nil {
		message := err.Error()
		if strings.Contains(message, "\n") {
			return Config{}, fmt.Errorf("%s:\n  %s", path, strings.ReplaceAll(message, "\n", "\n  "))
		}
		return Config{}, fmt.Errorf("%s: %s", path, message)
	}
	return c, nil
}

type file struct {
	Theme       *string `toml:"theme"`
	Split       *bool   `toml:"split"`
	AutoRefresh *bool   `toml:"auto_refresh"`
	Keys        any     `toml:"keys"`
}

func Parse(text string) (Config, error) {
	var f file
	meta, err := toml.Decode(text, &f)
	if err != nil {
		return Config{}, err
	}
	c := Default()
	if f.Theme != nil {
		c.Theme = *f.Theme
	}
	if f.Split != nil {
		c.Split = *f.Split
	}
	if f.AutoRefresh != nil {
		c.AutoRefresh = *f.AutoRefresh
	}

	var problems []error
	var unknown []string
	for _, key := range meta.Undecoded() {
		if key[0] != "keys" && !slices.Contains(unknown, key[0]) {
			unknown = append(unknown, key[0])
			problems = append(problems, fmt.Errorf("unknown option %s", key[0]))
		}
	}
	switch keys := f.Keys.(type) {
	case nil:
	case map[string]any:
		if bindProblems := bindAll(&c.Keys, keys); len(bindProblems) > 0 {
			problems = append(problems, bindProblems...)
		} else {
			problems = append(problems, c.Keys.Check())
		}
	default:
		problems = append(problems, errors.New("keys: want a table"))
	}
	if err := errors.Join(problems...); err != nil {
		return Config{}, err
	}
	return c, nil
}

func bindAll(k *keymap.Keymap, keys map[string]any) []error {
	var problems []error
	add := func(err error) {
		if err != nil {
			problems = append(problems, err)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(keys)) {
		entries, ok := keys[name].(map[string]any)
		if !ok {
			add(bind(k, keymap.Anywhere, name, keys[name]))
			continue
		}
		table, ok := keymap.TableNamed("keys." + name)
		if !ok {
			add(fmt.Errorf("unknown table keys.%s", name))
			continue
		}
		for _, action := range slices.Sorted(maps.Keys(entries)) {
			add(bind(k, table, action, entries[action]))
		}
	}
	return problems
}

func bind(k *keymap.Keymap, table keymap.Table, name string, value any) error {
	action, ok := keymap.Named(table, name)
	if !ok {
		return fmt.Errorf("unknown action %s.%s", table, name)
	}
	keys, ok := keyList(value)
	if !ok {
		return fmt.Errorf("%s: want a key name or a list of key names", action)
	}
	if err := k.Set(action, keys); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func keyList(value any) ([]string, bool) {
	switch v := value.(type) {
	case string:
		return []string{v}, true
	case []any:
		keys := make([]string, 0, len(v))
		for _, item := range v {
			key, ok := item.(string)
			if !ok {
				return nil, false
			}
			keys = append(keys, key)
		}
		return keys, true
	}
	return nil, false
}
