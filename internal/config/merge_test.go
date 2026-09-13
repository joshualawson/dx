package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestMerge(t *testing.T) {
	for _, test := range []struct {
		name    string
		layers  []map[string]any
		want    map[string]any
		origins map[string]string
		err     string
	}{
		{
			name:   "replace list",
			layers: []map[string]any{{"trusted": []any{"one", "two"}}, {"trusted": []any{"three"}}},
			want:   map[string]any{"trusted": []any{"three"}}, origins: map[string]string{"trusted": "near.yaml"},
		},
		{
			name:   "append list",
			layers: []map[string]any{{"trusted": []any{"one", "two"}}, {"trusted+": []any{"three"}}},
			want:   map[string]any{"trusted": []any{"one", "two", "three"}}, origins: map[string]string{"trusted": "near.yaml"},
		},
		{
			name:   "append without inheritance",
			layers: []map[string]any{{}, {"trusted+": []any{"one"}}},
			want:   map[string]any{"trusted": []any{"one"}}, origins: map[string]string{"trusted": "near.yaml"},
		},
		{
			name:   "empty append records origin",
			layers: []map[string]any{{"trusted": []any{"one"}}, {"trusted+": []any{}}},
			want:   map[string]any{"trusted": []any{"one"}}, origins: map[string]string{"trusted": "near.yaml"},
		},
		{
			name:   "empty replacement clears list",
			layers: []map[string]any{{"trusted": []any{"one"}}, {"trusted": []any{}}},
			want:   map[string]any{"trusted": []any{}}, origins: map[string]string{"trusted": "near.yaml"},
		},
		{
			name:   "nested append",
			layers: []map[string]any{{"outer": map[string]any{"list": []any{"one"}, "keep": true}}, {"outer": map[string]any{"list+": []any{"two"}}}},
			want:   map[string]any{"outer": map[string]any{"list": []any{"one", "two"}, "keep": true}}, origins: map[string]string{"outer.list": "near.yaml", "outer.keep": "far.yaml"},
		},
		{
			name:   "map replaced by scalar clears descendant origins",
			layers: []map[string]any{{"outer": map[string]any{"child": "one"}}, {"outer": false}},
			want:   map[string]any{"outer": false}, origins: map[string]string{"outer": "near.yaml"},
		},
		{
			name:   "scalar replaced by map clears scalar origin",
			layers: []map[string]any{{"outer": false}, {"outer": map[string]any{"child": "one"}}},
			want:   map[string]any{"outer": map[string]any{"child": "one"}}, origins: map[string]string{"outer.child": "near.yaml"},
		},
		{
			name:   "null replaces map",
			layers: []map[string]any{{"outer": map[string]any{"child": "one"}}, {"outer": nil}},
			want:   map[string]any{"outer": nil}, origins: map[string]string{"outer": "near.yaml"},
		},
		{
			name:   "empty map preserves inherited values",
			layers: []map[string]any{{"outer": map[string]any{"child": "one"}}, {"outer": map[string]any{}}},
			want:   map[string]any{"outer": map[string]any{"child": "one"}}, origins: map[string]string{"outer.child": "far.yaml"},
		},
		{
			name:   "dotted map keys preserve unrelated origins",
			layers: []map[string]any{{"env": map[string]any{"A": "one", "A.B": "two"}}, {"env": map[string]any{"A": "three"}}},
			want:   map[string]any{"env": map[string]any{"A": "three", "A.B": "two"}}, origins: map[string]string{"env.A": "near.yaml", "env.A.B": "far.yaml"},
		},
		{
			name: "append against scalar", layers: []map[string]any{{"list": "one"}, {"list+": []any{"two"}}}, err: "non-list",
		},
		{
			name: "append against map", layers: []map[string]any{{"list": map[string]any{}}, {"list+": []any{"two"}}}, err: "non-list",
		},
		{
			name: "append against null", layers: []map[string]any{{"list": nil}, {"list+": []any{"two"}}}, err: "non-list",
		},
		{
			name: "append requires list", layers: []map[string]any{{}, {"list+": "two"}}, err: "must append a list",
		},
		{
			name: "ambiguous append rejected", layers: []map[string]any{{}, {"list": []any{"one"}, "list+": []any{"two"}}}, err: "cannot appear together",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			merged := make(map[string]any)
			origins := make(map[string]string)
			var err error
			for i, doc := range test.layers {
				source := "far.yaml"
				if i == len(test.layers)-1 {
					source = "near.yaml"
				}
				err = merge(merged, doc, source, "", origins)
				if err != nil {
					break
				}
			}
			if test.err != "" {
				if err == nil || !strings.Contains(err.Error(), test.err) || !strings.Contains(err.Error(), "near.yaml") {
					t.Fatalf("error = %v, want %q naming near.yaml", err, test.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(merged, test.want) || !reflect.DeepEqual(origins, test.origins) {
				t.Errorf("merged = %#v, origins = %#v; want %#v, %#v", merged, origins, test.want, test.origins)
			}
		})
	}
}

func TestMergeDoesNotMutateSourceMaps(t *testing.T) {
	far := map[string]any{"tools": map[string]any{"go": map[string]any{"version": "1.24"}}}
	near := map[string]any{"tools": map[string]any{"go": map[string]any{"version": "1.25"}}}
	merged := make(map[string]any)
	origins := make(map[string]string)
	for _, doc := range []map[string]any{far, near} {
		if err := merge(merged, doc, "config.yaml", "", origins); err != nil {
			t.Fatal(err)
		}
	}
	if got := far["tools"].(map[string]any)["go"].(map[string]any)["version"]; got != "1.24" {
		t.Errorf("source version = %v, want 1.24", got)
	}
}
