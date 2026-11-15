package actions

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestActionSerializationRoundTrip(t *testing.T) {
	latest := false
	create := NewCreateReleaseAction("v1.0.0", false, true, "abc")
	create.MakeLatest = &latest
	publish := NewPublishReleaseAction("v1.0.0", 123)
	publish.MakeLatest = &latest
	republish := NewRepublishReleaseAction("v1.0.0")
	republish.MakeLatest = &latest
	all := []state.Action{
		NewCreateTagAction("v1", "abc"), NewUpdateTagAction("v1", "abc", true), NewDeleteTagAction("v1"),
		NewCreateBranchAction("v1", "abc"), NewUpdateBranchAction("v1", "abc", true), NewDeleteBranchAction("v1"),
		NewConvertBranchToTagAction("v1", "abc"), NewConvertTagToBranchAction("v1", "abc"),
		create, NewDeleteReleaseAction("v1.0.0", 123), publish, NewSetLatestReleaseAction("v1.0.0", 123),
		NewEnableImmutableReleasesAction(), republish,
	}
	covered := map[string]bool{}
	for _, action := range all {
		name := reflect.TypeOf(action).Elem().Name()
		covered[name] = true
		t.Run(name, func(t *testing.T) {
			encoded, err := Serialize(state.New(), action)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(encoded)
			if err != nil {
				t.Fatal(err)
			}
			var decoded SerializedAction
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			got, err := Deserialize(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, action) {
				t.Fatalf("round trip: got %#v, want %#v", got, action)
			}
		})
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			method, ok := decl.(*ast.FuncDecl)
			if !ok || method.Recv == nil || method.Name.Name != "Execute" {
				continue
			}
			receiver, ok := method.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				t.Fatalf("unexpected Execute receiver in %s", name)
			}
			typ, ok := receiver.X.(*ast.Ident)
			if !ok || !covered[typ.Name] {
				t.Errorf("action with Execute in %s has no serialization round-trip test", name)
			}
		}
	}
}

func TestSerializeResolvesReleaseIDOrRequiresReload(t *testing.T) {
	st := state.New()
	st.Releases = []*state.ReleaseInfo{{TagName: "v1.0.0", ID: 123}}
	got, err := Serialize(st, NewPublishReleaseAction("v1.0.0", 0))
	if err != nil || got.ReleaseID != 123 || got.RequiresReload != "" {
		t.Fatalf("resolved action = %+v, %v", got, err)
	}
	got, err = Serialize(st, NewDeleteReleaseAction("v2.0.0", 0))
	if err != nil || got.RequiresReload == "" {
		t.Fatalf("unresolved action = %+v, %v", got, err)
	}
	if _, err := Deserialize(got); err == nil {
		t.Fatal("expected unresolved action to be rejected")
	}
}

func TestDeserializeRejectsInvalidActions(t *testing.T) {
	for _, action := range []SerializedAction{
		{Kind: "unknown", Name: "v1"},
		{Kind: "create-tag", Name: "v1"},
		{Kind: "delete-tag"},
		{Kind: "delete-release", Name: "v1", ReleaseID: -1},
	} {
		if _, err := Deserialize(action); err == nil {
			t.Errorf("accepted invalid action %+v", action)
		}
	}
}
