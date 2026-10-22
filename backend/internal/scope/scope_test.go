package scope

import (
	"errors"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		all     bool
		wantErr bool
	}{
		{raw: "", all: true},
		{raw: "shop", want: "shop"},
		{raw: "shop,argocd,shop", want: "argocd,shop"},
		{raw: "shop,", wantErr: true},
		{raw: "Shop", wantErr: true},
		{raw: "a b", wantErr: true},
		{raw: strings.Repeat("a,", Max) + "a", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := Parse(tt.raw)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || got.String() != tt.want || got.All() != tt.all {
				t.Fatalf("got %v (all %v), err %v", got, got.All(), err)
			}
		})
	}
}

func TestNamespaces(t *testing.T) {
	all, _ := Parse("")
	two, _ := Parse("b,a")
	if !all.Contains("x") || !two.Contains("a") || two.Contains("c") {
		t.Fatal("Contains")
	}
	if strings.Join(all.Targets(), "|") != "" || len(all.Targets()) != 1 || strings.Join(two.Targets(), ",") != "a,b" {
		t.Fatalf("targets %q %q", all.Targets(), two.Targets())
	}
	if ns, ok := One("shop").Single(); !ok || ns != "shop" {
		t.Fatal("Single")
	}
	if _, ok := two.Single(); ok {
		t.Fatal("two isn't one")
	}
	if !One("").All() {
		t.Fatal(`One("") is all`)
	}
}
