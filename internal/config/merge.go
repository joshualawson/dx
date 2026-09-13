package config

import (
	"fmt"
	"sort"
	"strings"
)

func validate(doc map[string]any, global bool) error {
	normalized := make(map[string]any, len(doc))
	for _, rawKey := range sortedKeys(doc) {
		key := strings.TrimSuffix(rawKey, "+")
		value := doc[rawKey]
		switch key {
		case "root", "tools", "env", "docker", "credentials", "trusted", "idle_timeout":
		default:
			return fmt.Errorf("unknown key %q", rawKey)
		}
		if key == "trusted" && !global {
			return fmt.Errorf("key %q is only allowed in global config", rawKey)
		}
		if rawKey != key {
			if _, exists := doc[key]; exists {
				return fmt.Errorf("keys %q and %q cannot appear together", key, rawKey)
			}
			if key != "trusted" {
				return fmt.Errorf("key %q cannot append to a non-list", rawKey)
			}
			if _, ok := value.([]any); !ok {
				return fmt.Errorf("key %q must append a list", rawKey)
			}
		}
		normalized[key] = value
		if value == nil {
			continue
		}
		switch key {
		case "tools", "env", "credentials":
			values, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("key %q must be a map with string keys", key)
			}
			for _, name := range sortedKeys(values) {
				switch key {
				case "tools":
					if values[name] == nil {
						continue
					}
					tool, ok := values[name].(map[string]any)
					if !ok {
						return fmt.Errorf("key %q must be a map with string keys", "tools."+name)
					}
					for _, field := range sortedKeys(tool) {
						switch field {
						case "image", "version", "warm":
						default:
							return fmt.Errorf("unknown key %q", "tools."+name+"."+field)
						}
					}
				case "credentials":
					switch name {
					case "all", "ssh", "git", "netrc", "aws", "gcloud", "azure", "kube", "gh", "docker", "npm", "pypi", "cargo", "terraform":
					default:
						return fmt.Errorf("unknown key %q", "credentials."+name)
					}
				}
			}
		}
	}
	_, err := decode(normalized)
	return err
}

func merge(dst, src map[string]any, source, prefix string, origins map[string]string) error {
	for _, rawKey := range sortedKeys(src) {
		key := strings.TrimSuffix(rawKey, "+")
		leaf := key
		if prefix != "" {
			leaf = prefix + "." + key
		}
		value := src[rawKey]
		if key != rawKey {
			if _, exists := src[key]; exists {
				return fmt.Errorf("merge config %q: keys %q and %q cannot appear together", source, key, rawKey)
			}
			list, ok := value.([]any)
			if !ok {
				return fmt.Errorf("merge config %q: key %q must append a list", source, leaf)
			}
			var inherited []any
			if old, exists := dst[key]; exists {
				inherited, ok = old.([]any)
				if !ok {
					return fmt.Errorf("merge config %q: key %q cannot append to a non-list", source, leaf)
				}
			}
			clearOrigins(origins, leaf, dst[key])
			dst[key] = append(append([]any{}, inherited...), list...)
			origins[leaf] = source
			continue
		}
		if values, ok := value.(map[string]any); ok {
			inherited, ok := dst[key].(map[string]any)
			if !ok {
				inherited = make(map[string]any)
				clearOrigins(origins, leaf, dst[key])
			}
			if err := merge(inherited, values, source, leaf, origins); err != nil {
				return err
			}
			dst[key] = inherited
			continue
		}
		clearOrigins(origins, leaf, dst[key])
		dst[key] = value
		origins[leaf] = source
	}
	return nil
}

func clearOrigins(origins map[string]string, key string, value any) {
	if values, ok := value.(map[string]any); ok {
		for child, value := range values {
			clearOrigins(origins, key+"."+child, value)
		}
		return
	}
	delete(origins, key)
}

func sortedKeys(doc map[string]any) []string {
	keys := make([]string, 0, len(doc))
	for key := range doc {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
