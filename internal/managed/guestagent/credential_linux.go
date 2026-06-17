//go:build linux

package guestagent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func CredentialForUser(user string) (*syscall.Credential, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil, nil
	}
	if user == "root" || user == "0" || user == "0:0" {
		return nil, nil
	}
	uidPart, gidPart, hasGID := strings.Cut(user, ":")
	if uidPart == "" {
		return nil, fmt.Errorf("invalid user %q", user)
	}
	uid, err := parseUint32(uidPart)
	if err != nil {
		return nil, fmt.Errorf("invalid uid %q", uidPart)
	}
	gid := uid
	if hasGID {
		if gidPart == "" {
			return nil, fmt.Errorf("invalid gid in user %q", user)
		}
		gid, err = parseUint32(gidPart)
		if err != nil {
			return nil, fmt.Errorf("invalid gid %q", gidPart)
		}
	}
	return &syscall.Credential{Uid: uid, Gid: gid}, nil
}

func EnsureCredentialUser(rootDir string, cred *syscall.Credential) error {
	if cred == nil || cred.Uid == 0 {
		return nil
	}
	rootDir = strings.TrimRight(rootDir, "/")
	uid := fmt.Sprintf("%d", cred.Uid)
	gid := fmt.Sprintf("%d", cred.Gid)
	name := usernameForUID(rootDir, uid)
	if name == "" {
		name = availableUserName(rootDir, "cc")
	}
	homeDir := homeDirForUID(rootDir, uid)
	if homeDir == "" {
		homeDir = "/home/" + name
	}
	group := groupNameForGID(rootDir, gid)
	if group == "" {
		group = availableGroupName(rootDir, name)
		if err := appendGroupEntry(rootDir, group, gid); err != nil {
			return err
		}
	}
	if name != "" && passwdHasUID(rootDir, uid) {
		return ensureCredentialHome(rootDir, homeDir, cred)
	}
	if err := appendPasswdEntry(rootDir, name, uid, gid, homeDir, "/bin/sh"); err != nil {
		return err
	}
	return ensureCredentialHome(rootDir, homeDir, cred)
}

func EnsureCredentialWorkDir(rootDir, workDir string, cred *syscall.Credential) error {
	workDir = filepath.Clean(strings.TrimSpace(workDir))
	if workDir == "" || workDir == "." || workDir == "/" {
		return nil
	}
	if !strings.HasPrefix(workDir, "/home/") {
		return nil
	}
	return ensureCredentialDirectory(rootDir, workDir, cred)
}

func ChownPathForUser(target, user string) error {
	cred, err := CredentialForUser(user)
	if err != nil || cred == nil {
		return err
	}
	return os.Chown(target, int(cred.Uid), int(cred.Gid))
}

func ensureCredentialHome(rootDir, homeDir string, cred *syscall.Credential) error {
	if strings.TrimSpace(homeDir) == "" || homeDir == "/" {
		return nil
	}
	return ensureCredentialDirectory(rootDir, homeDir, cred)
}

func ensureCredentialDirectory(rootDir, dir string, cred *syscall.Credential) error {
	if strings.TrimSpace(dir) == "" || dir == "/" {
		return nil
	}
	path := rootPath(rootDir, dir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	if err := os.Chown(path, int(cred.Uid), int(cred.Gid)); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}

func usernameForUID(rootDir, uid string) string {
	for _, line := range colonFileLines(rootPath(rootDir, "/etc/passwd")) {
		fields := strings.Split(line, ":")
		if len(fields) >= 3 && fields[2] == uid {
			return fields[0]
		}
	}
	return ""
}

func homeDirForUID(rootDir, uid string) string {
	for _, line := range colonFileLines(rootPath(rootDir, "/etc/passwd")) {
		fields := strings.Split(line, ":")
		if len(fields) >= 6 && fields[2] == uid {
			return fields[5]
		}
	}
	return ""
}

func groupNameForGID(rootDir, gid string) string {
	for _, line := range colonFileLines(rootPath(rootDir, "/etc/group")) {
		fields := strings.Split(line, ":")
		if len(fields) >= 3 && fields[2] == gid {
			return fields[0]
		}
	}
	return ""
}

func passwdHasUID(rootDir, uid string) bool {
	return usernameForUID(rootDir, uid) != ""
}

func colonFileLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	out := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func availableUserName(rootDir, base string) string {
	if !nameExists(rootPath(rootDir, "/etc/passwd"), base) {
		return base
	}
	for i := 1000; ; i++ {
		name := base + itoa(i)
		if !nameExists(rootPath(rootDir, "/etc/passwd"), name) {
			return name
		}
	}
}

func availableGroupName(rootDir, base string) string {
	if !nameExists(rootPath(rootDir, "/etc/group"), base) {
		return base
	}
	for i := 1000; ; i++ {
		name := base + itoa(i)
		if !nameExists(rootPath(rootDir, "/etc/group"), name) {
			return name
		}
	}
}

func nameExists(path, name string) bool {
	for _, line := range colonFileLines(path) {
		fields := strings.Split(line, ":")
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}

func appendGroupEntry(rootDir, name, gid string) error {
	path := rootPath(rootDir, "/etc/group")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return appendLine(path, name+":x:"+gid+":")
}

func appendPasswdEntry(rootDir, name, uid, gid, home, shell string) error {
	path := rootPath(rootDir, "/etc/passwd")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return appendLine(path, strings.Join([]string{name, "x", uid, gid, "ccvm user", home, shell}, ":"))
}

func appendLine(path, line string) error {
	existing, _ := os.ReadFile(path)
	prefix := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		prefix = "\n"
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := io.WriteString(f, prefix+line+"\n"); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func parseUint32(value string) (uint32, error) {
	n := uint64(0)
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("not numeric")
		}
		n = n*10 + uint64(ch-'0')
		if n > uint64(^uint32(0)) {
			return 0, fmt.Errorf("out of range")
		}
	}
	return uint32(n), nil
}
