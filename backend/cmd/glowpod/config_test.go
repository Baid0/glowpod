package main

import (
	"errors"
	"flag"
	"io"
	"testing"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    config
		wantErr bool
		isHelp  bool
	}{
		{name: "defaults", args: nil, want: config{addr: defaultAddr}},
		{name: "dev mode", args: []string{"-dev"}, want: config{addr: defaultAddr, dev: true}},
		{name: "no browser", args: []string{"--no-browser"}, want: config{addr: defaultAddr, noBrowser: true}},
		{name: "localhost", args: []string{"-addr", "localhost:9000"}, want: config{addr: "localhost:9000"}},
		{name: "ipv6 loopback", args: []string{"-addr", "[::1]:9000"}, want: config{addr: "[::1]:9000"}},
		{name: "random port", args: []string{"-addr", "127.0.0.1:0"}, want: config{addr: "127.0.0.1:0"}},
		{name: "all interfaces rejected", args: []string{"-addr", "0.0.0.0:7171"}, wantErr: true},
		{name: "empty host rejected", args: []string{"-addr", ":7171"}, wantErr: true},
		{name: "lan address rejected", args: []string{"-addr", "192.168.1.5:7171"}, wantErr: true},
		{name: "missing port", args: []string{"-addr", "127.0.0.1"}, wantErr: true},
		{name: "unknown flag", args: []string{"-nope"}, wantErr: true},
		{name: "stray argument", args: []string{"serve"}, wantErr: true},
		{name: "help", args: []string{"-h"}, wantErr: true, isHelp: true},
		{
			name: "cluster flags",
			args: []string{"-kubeconfig", "/tmp/k", "-context", "lab", "-prometheus-url", "http://localhost:9090"},
			want: config{addr: defaultAddr, kubeconfig: "/tmp/k", context: "lab", prometheusURL: "http://localhost:9090"},
		},
		{name: "prometheus url needs a scheme", args: []string{"-prometheus-url", "localhost:9090"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFlags(tt.args, io.Discard)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got config %+v", got)
				}
				if errors.Is(err, flag.ErrHelp) != tt.isHelp {
					t.Fatalf("help error = %v, want %v (err: %v)", !tt.isHelp, tt.isHelp, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
