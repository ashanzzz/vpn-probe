package model

import "testing"

func TestCustomGroupValidate(t *testing.T) {
	g := CustomGroupDef{Name: "self-us", DisplayName: "自建-美国", Filter: GroupFilter{Mode: "filter", Kinds: []string{"residential"}}}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	g.Filter.CustomMatchRegex = "["
	if err := g.Validate(); err == nil {
		t.Fatal("invalid regex should fail")
	}
	g.Filter.CustomMatchRegex = ""
	g.Filter.Mode = "manual"
	if err := g.Validate(); err == nil {
		t.Fatal("manual group needs IDs")
	}
}
