package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Root        bool              `yaml:"root"`
	Tools       map[string]Tool   `yaml:"tools"`
	Env         map[string]string `yaml:"env"`
	Docker      bool              `yaml:"docker"`
	Credentials map[string]bool   `yaml:"credentials"`
	Trusted     []string          `yaml:"trusted"`
	IdleTimeout Duration          `yaml:"idle_timeout"`
}

type Tool struct {
	Image   string `yaml:"image"`
	Version string `yaml:"version"`
	Warm    *bool  `yaml:"warm"`
}

type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return fmt.Errorf("line %d: duration must be a string", node.Line)
	}
	value, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration: %w", node.Line, err)
	}
	*d = Duration(value)
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

func (c Config) CredentialEnabled(name string) bool {
	if enabled, ok := c.Credentials[name]; ok {
		return enabled
	}
	if enabled, ok := c.Credentials["all"]; ok {
		return enabled
	}
	return true
}

type Source struct {
	Path   string
	Global bool
}

type Result struct {
	Config  Config
	Sources []Source
	Origins map[string]string
}

type TrustChecker interface {
	IsTrusted(path string, content []byte, doc map[string]any, trustedDirs []string) (bool, error)
}

type UntrustedError struct{ Paths []string }

func (e *UntrustedError) Error() string {
	return "untrusted config files: " + strings.Join(e.Paths, ", ") + "; run dx trust for each file"
}

type Options struct {
	Cwd, Home, GOOS string
	Getenv          func(string) string
	Trust           TrustChecker
}

type layer struct {
	source  Source
	content []byte
	doc     map[string]any
}

func Load(opts Options) (*Result, error) {
	cwd, err := absolutePath(opts.GOOS, opts.Cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve working directory %q: %w", opts.Cwd, err)
	}
	home := opts.Home
	if home != "" {
		home, err = absolutePath(opts.GOOS, home)
		if err != nil {
			return nil, fmt.Errorf("resolve home directory %q: %w", opts.Home, err)
		}
	}

	result := &Result{Origins: make(map[string]string)}
	merged := make(map[string]any)
	defaults := map[string]any{
		"root": false, "tools": map[string]any{}, "env": map[string]any{},
		"docker": false, "credentials": map[string]any{"all": true},
		"trusted": []any{}, "idle_timeout": "30m",
	}
	if err := merge(merged, defaults, "default", "", result.Origins); err != nil {
		return nil, err
	}
	global, err := readLayer(GlobalPath(opts.GOOS, home, opts.Getenv), true)
	if err != nil {
		return nil, err
	}
	if global != nil {
		if err := merge(merged, global.doc, global.source.Path, "", result.Origins); err != nil {
			return nil, err
		}
		result.Sources = append(result.Sources, global.source)
	}
	globalConfig, err := decode(merged)
	if err != nil {
		return nil, fmt.Errorf("decode global config %q: %w", GlobalPath(opts.GOOS, home, opts.Getenv), err)
	}
	trusted := expandTrusted(globalConfig.Trusted, home, opts.GOOS)

	var projects []*layer
	insideHome := home != "" && containsPath(opts.GOOS, home, cwd)
	for dir := cwd; ; dir = parentPath(opts.GOOS, dir) {
		if insideHome && samePath(opts.GOOS, dir, home) {
			break
		}
		project, err := readLayer(joinPath(opts.GOOS, dir, ".dx.yaml"), false)
		if err != nil {
			return nil, err
		}
		if project != nil {
			projects = append(projects, project)
			if root, _ := project.doc["root"].(bool); root {
				break
			}
		}
		if samePath(opts.GOOS, parentPath(opts.GOOS, dir), dir) {
			break
		}
	}

	var untrusted []string
	if opts.Trust != nil {
		for _, project := range projects {
			ok, err := opts.Trust.IsTrusted(project.source.Path, project.content, project.doc, trusted)
			if err != nil {
				return nil, fmt.Errorf("check trust for %q: %w", project.source.Path, err)
			}
			if !ok {
				untrusted = append(untrusted, project.source.Path)
			}
		}
	}
	if len(untrusted) != 0 {
		return nil, &UntrustedError{Paths: untrusted}
	}
	for i := len(projects) - 1; i >= 0; i-- {
		project := projects[i]
		if err := merge(merged, project.doc, project.source.Path, "", result.Origins); err != nil {
			return nil, err
		}
		result.Sources = append(result.Sources, project.source)
	}
	result.Config, err = decode(merged)
	if err != nil {
		return nil, fmt.Errorf("decode merged config from %v: %w", result.Sources, err)
	}
	result.Config.Trusted = trusted
	return result, nil
}

func readLayer(filename string, global bool) (*layer, error) {
	content, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", filename, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	doc := make(map[string]any)
	if err := decoder.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse config %q: %w", filename, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("parse config %q: %w", filename, err)
		}
		return nil, fmt.Errorf("parse config %q: multiple yaml documents are not supported", filename)
	}
	if doc == nil {
		doc = make(map[string]any)
	}
	if err := validate(doc, global); err != nil {
		return nil, fmt.Errorf("validate config %q: %w", filename, err)
	}
	return &layer{source: Source{Path: filename, Global: global}, content: content, doc: doc}, nil
}

func decode(doc map[string]any) (Config, error) {
	var cfg Config
	content, err := yaml.Marshal(doc)
	if err != nil {
		return cfg, err
	}
	err = yaml.Unmarshal(content, &cfg)
	return cfg, err
}

func expandTrusted(dirs []string, home, goos string) []string {
	result := make([]string, len(dirs))
	for i, dir := range dirs {
		switch {
		case dir == "~":
			result[i] = home
		case strings.HasPrefix(dir, "~/") || (goos == "windows" && strings.HasPrefix(dir, `~\`)):
			result[i] = joinPath(goos, home, dir[2:])
		default:
			result[i] = dir
		}
	}
	return result
}
