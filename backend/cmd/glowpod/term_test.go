package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestParseTermFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    termConfig
		wantErr string
	}{
		{name: "defaults", args: nil, want: termConfig{}},
		{
			name: "everything",
			args: []string{"-kubeconfig", "/k", "-context", "lab", "-n", "team", "-prometheus-url", "http://p:9090", "deploy"},
			want: termConfig{kubeconfig: "/k", context: "lab", namespace: "team", prometheusURL: "http://p:9090", resource: "deploy"},
		},
		{name: "long namespace flag", args: []string{"-namespace", "x"}, want: termConfig{namespace: "x"}},
		{name: "two resources", args: []string{"pods", "deploy"}, wantErr: `unexpected argument "deploy"`},
		{name: "unknown flag", args: []string{"-nope"}, wantErr: "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTermFlags(tt.args, &bytes.Buffer{})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestRunTerm(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantOut string
		wantErr string
	}{
		{name: "help", args: []string{"term", "-h"}, wantOut: "Usage: glowpod term"},
		{name: "missing kubeconfig", args: []string{"term", "-kubeconfig", "/does/not/exist"}, wantErr: "/does/not/exist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := run(context.Background(), tt.args, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("err = %v, output %q, want %q", err, out.String(), tt.wantOut)
			}
		})
	}
}
