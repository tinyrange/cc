//go:build linux

package vm

import (
	"strings"
	"testing"
)

func TestResolveLinuxRuntimeExecUser(t *testing.T) {
	tests := []struct {
		name string
		user string
		want string
	}{
		{name: "root", user: "root", want: "0:0"},
		{name: "uid zero", user: "0", want: "0:0"},
		{name: "uid gid zero", user: "0:0", want: "0:0"},
		{name: "uid only", user: "1234", want: "1234:1234"},
		{name: "uid gid", user: "1234:5678", want: "1234:5678"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveLinuxRuntimeExecUser("linux test", tc.user)
			if err != nil {
				t.Fatalf("resolveLinuxRuntimeExecUser: %v", err)
			}
			if got != tc.want {
				t.Fatalf("user = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveLinuxRuntimeExecUserDefaultsToHostIdentity(t *testing.T) {
	got, err := resolveLinuxRuntimeExecUser("linux test", "")
	if err != nil {
		t.Fatalf("resolveLinuxRuntimeExecUser: %v", err)
	}
	if parts := strings.Split(got, ":"); len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Fatalf("default user = %q, want uid:gid", got)
	}
}

func TestResolveLinuxRuntimeExecUserRejectsInvalidUsers(t *testing.T) {
	tests := []struct {
		user string
		want string
	}{
		{user: ":1", want: "invalid user"},
		{user: "1:", want: "invalid user"},
		{user: "daemon", want: "linux test runtime supports numeric users only"},
		{user: "1:daemon", want: "linux test runtime supports numeric users only"},
	}
	for _, tc := range tests {
		t.Run(tc.user, func(t *testing.T) {
			_, err := resolveLinuxRuntimeExecUser("linux test", tc.user)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
