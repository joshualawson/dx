package route

import "strings"

type Tool struct {
	Image   string
	Version string
}

type Input struct {
	Cmd       string
	ImageFlag string
	Tools     map[string]Tool
	Markers   []string
	Registry  string
}

type Result struct {
	Image     string
	Toolchain string
	Reason    string
}

const DefaultRegistry = "ghcr.io/joshualawson"

var Builtin = map[string]string{
	"go":            "go",
	"gofmt":         "go",
	"gopls":         "go",
	"golangci-lint": "go",
	"dlv":           "go",
	"node":          "node",
	"npm":           "node",
	"npx":           "node",
	"pnpm":          "node",
	"yarn":          "node",
	"corepack":      "node",
	"tsc":           "node",
	"tsx":           "node",
	"eslint":        "node",
	"prettier":      "node",
	"python":        "python",
	"python3":       "python",
	"pip":           "python",
	"pip3":          "python",
	"uv":            "python",
	"uvx":           "python",
	"ruff":          "python",
	"cargo":         "rust",
	"rustc":         "rust",
	"rustup":        "rust",
	"rustfmt":       "rust",
	"rust-analyzer": "rust",
	"cargo-clippy":  "rust",
	"clippy-driver": "rust",
	"terraform":     "infra",
	"kubectl":       "infra",
	"helm":          "infra",
	"aws":           "infra",
	"gcloud":        "infra",
	"gsutil":        "infra",
	"az":            "infra",
}

func Toolchains() map[string]string {
	return map[string]string{
		"go":     "dx-go",
		"node":   "dx-node",
		"python": "dx-python",
		"rust":   "dx-rust",
		"infra":  "dx-infra",
		"base":   "dx-base",
	}
}

func Resolve(in Input) Result {
	toolchain := Builtin[in.Cmd]
	if in.ImageFlag != "" {
		return Result{Image: in.ImageFlag, Toolchain: toolchain, Reason: "--image flag"}
	}
	if in.Tools[in.Cmd].Image != "" {
		return resolveImage(in, toolchain, "")
	}
	if toolchain != "" {
		return resolveImage(in, toolchain, "built-in mapping for "+in.Cmd)
	}
	for _, marker := range []struct {
		name      string
		toolchain string
	}{
		{"go.mod", "go"},
		{"package.json", "node"},
		{"Cargo.toml", "rust"},
		{"pyproject.toml", "python"},
	} {
		for _, present := range in.Markers {
			if present == marker.name {
				return resolveImage(in, marker.toolchain, marker.name+" in project")
			}
		}
	}
	return resolveImage(in, "base", "default")
}

func resolveImage(in Input, toolchain, reason string) Result {
	commandTool := in.Tools[in.Cmd]
	var chainTool Tool
	if toolchain != "" {
		chainTool = in.Tools[toolchain]
	}
	image := commandTool.Image
	if image != "" {
		reason = "tools." + in.Cmd + ".image in config"
	} else if chainTool.Image != "" {
		image = chainTool.Image
		reason = "tools." + toolchain + ".image in config"
	} else {
		registry := in.Registry
		if registry == "" {
			registry = DefaultRegistry
		}
		image = registry + "/dx-" + toolchain
	}
	version := commandTool.Version
	if version == "" {
		version = chainTool.Version
	}
	if version == "" {
		version = "latest"
	}
	// A registry port is not an image tag.
	if !strings.Contains(image, "@") && strings.LastIndex(image, ":") <= strings.LastIndex(image, "/") {
		image += ":" + version
	}
	return Result{Image: image, Toolchain: toolchain, Reason: reason}
}
